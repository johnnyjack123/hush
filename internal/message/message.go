// Package message seals text for a contact and opens sealed text again.
//
// A sealed message is an ASCII-armored age file. Its plaintext is a signed
// envelope:
//
//	magic "hush.msg\x00" | version (1) | sender public key (64)
//	| unix time, big endian (8) | Ed25519 signature (64)
//	| text length, big endian (4) | text | zero padding
//
// The signature covers the recipient's public key as well, so a recipient
// cannot re-encrypt a signed message to a third party and pass it off as
// having been sent to them. Padding to a multiple of padBlock hides the exact
// length of short secrets such as passwords.
package message

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/johnnyjack123/hush/internal/keys"
)

var (
	// ErrNoMessage means the input contains no armored age message.
	ErrNoMessage = errors.New("no encrypted message found")
	// ErrNotForMe means the message was encrypted to someone else's key.
	ErrNotForMe = errors.New("message is not encrypted to this key")
	// ErrBadSignature means the signature does not match, so the claimed sender is not authentic.
	ErrBadSignature = errors.New("signature verification failed")
	// ErrCorrupt means the message was damaged or is not a valid message.
	ErrCorrupt = errors.New("message is damaged or invalid")
	// ErrTooLong means the text exceeds MaxTextSize.
	ErrTooLong = errors.New("text is too long")
	// ErrNotText means the input to seal is not valid UTF-8, which Open would reject.
	ErrNotText = errors.New("text is not valid UTF-8")
)

// MaxTextSize bounds the text of a message; hush is for short secrets, not files.
const MaxTextSize = 1 << 20

var magic = []byte("hush.msg\x00")

const (
	version    = 1
	padBlock   = 128
	headerSize = 9 + 1 + keys.PublicKeySize + 8 + 64 + 4
)

const (
	beginMarker = "BEGIN AGE ENCRYPTED FILE"
	endMarker   = "END AGE ENCRYPTED FILE"
	// Messengers and phone keyboards may turn "-----" into en or em dashes.
	dashes = "-–—"
)

// Seal signs text with from's key and encrypts it to to. The result is armored text.
func Seal(text string, from *keys.Identity, to keys.PublicKey, now time.Time) (string, error) {
	if len(text) > MaxTextSize {
		return "", ErrTooLong
	}
	if !utf8.ValidString(text) {
		return "", ErrNotText
	}
	payload := envelope(text, from, to, now)

	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, to.Recipient())
	if err != nil {
		return "", err
	}
	if _, err := w.Write(payload); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	if err := aw.Close(); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func envelope(text string, from *keys.Identity, to keys.PublicKey, now time.Time) []byte {
	sender := from.Public().Bytes()
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(now.Unix()))
	sig := from.Sign(signedData(to, sender, ts[:], []byte(text)))

	var b bytes.Buffer
	b.Write(magic)
	b.WriteByte(version)
	b.Write(sender)
	b.Write(ts[:])
	b.Write(sig)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(text)))
	b.Write(n[:])
	b.WriteString(text)
	if r := b.Len() % padBlock; r != 0 {
		b.Write(make([]byte, padBlock-r))
	}
	return b.Bytes()
}

func signedData(recipient keys.PublicKey, sender, ts, text []byte) []byte {
	var b bytes.Buffer
	b.WriteString("hush/v1 signature\x00")
	b.Write(recipient.Bytes())
	b.Write(sender)
	b.Write(ts)
	b.Write(text)
	return b.Bytes()
}

// Opened is a successfully decrypted message.
type Opened struct {
	Text string
	// Sender is the verified signer, or nil for an unsigned message such as
	// one produced by the plain age CLI.
	Sender *keys.PublicKey
	// SentAt is the sender's claimed time of sealing; zero if unsigned.
	SentAt time.Time
}

// Open finds an armored message in input, decrypts it with me and verifies its signature.
func Open(input string, me *keys.Identity) (*Opened, error) {
	armored, ok := Find(input)
	if !ok {
		return nil, ErrNoMessage
	}
	r, err := age.Decrypt(armor.NewReader(strings.NewReader(armored)), me.AgeIdentity())
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return nil, ErrNotForMe
		}
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	const maxPlain = headerSize + MaxTextSize + padBlock
	plain, err := io.ReadAll(io.LimitReader(r, maxPlain+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(plain) > maxPlain {
		// Plain age messages have no length field, so reading only a prefix
		// would silently show a truncated text.
		return nil, ErrTooLong
	}
	if !bytes.HasPrefix(plain, magic) {
		if !utf8.Valid(plain) {
			return nil, fmt.Errorf("%w: not text", ErrCorrupt)
		}
		return &Opened{Text: string(plain)}, nil
	}
	return openEnvelope(plain, me.Public())
}

func openEnvelope(p []byte, me keys.PublicKey) (*Opened, error) {
	if len(p) < headerSize {
		return nil, fmt.Errorf("%w: truncated", ErrCorrupt)
	}
	p = p[len(magic):]
	if p[0] != version {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrCorrupt, p[0])
	}
	p = p[1:]
	senderRaw, p := p[:keys.PublicKeySize], p[keys.PublicKeySize:]
	ts, p := p[:8], p[8:]
	sig, p := p[:64], p[64:]
	n, p := binary.BigEndian.Uint32(p[:4]), p[4:]
	if uint64(n) > uint64(len(p)) {
		return nil, fmt.Errorf("%w: bad length", ErrCorrupt)
	}
	text := p[:n]

	sender, err := keys.PublicKeyFromBytes(senderRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if !sender.Verify(signedData(me, senderRaw, ts, text), sig) {
		return nil, ErrBadSignature
	}
	if !utf8.Valid(text) {
		return nil, fmt.Errorf("%w: not text", ErrCorrupt)
	}
	return &Opened{
		Text:   string(text),
		Sender: &sender,
		SentAt: time.Unix(int64(binary.BigEndian.Uint64(ts)), 0),
	}, nil
}

// Find extracts the first armored age message from text and returns it in
// canonical form. It tolerates what messengers do to pasted text: surrounding
// words, CRLF line endings, indentation, re-wrapped or joined lines, and
// dashes replaced by typographic ones.
func Find(text string) (string, bool) {
	i := strings.Index(text, beginMarker)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(beginMarker):]
	j := strings.Index(rest, endMarker)
	if j < 0 {
		return "", false
	}
	body := strings.Trim(rest[:j], dashes+" \t\r\n")
	body = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, body)
	if body == "" || !isBase64(body) {
		return "", false
	}
	var b strings.Builder
	b.WriteString(armor.Header + "\n")
	for len(body) > 64 {
		b.WriteString(body[:64] + "\n")
		body = body[64:]
	}
	b.WriteString(body + "\n" + armor.Footer + "\n")
	return b.String(), true
}

// Contains reports whether text contains an armored message.
func Contains(text string) bool {
	_, ok := Find(text)
	return ok
}

func isBase64(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}
