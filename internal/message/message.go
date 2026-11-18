// Package message seals text for contacts and opens sealed text again.
//
// A sealed message is an ASCII-armored age file. Its plaintext is a signed
// envelope. Version 1 is used for a single recipient:
//
//	magic "hush.msg\x00" | 1 | sender public key (64)
//	| unix time, big endian (8) | Ed25519 signature (64)
//	| text length, big endian (4) | text | zero padding
//
// Version 2 is used for several recipients and lists them after the
// signature, so every recipient sees who else received the message:
//
//	magic | 2 | sender (64) | time (8) | signature (64)
//	| recipient count, big endian (2) | recipient public keys (64 each)
//	| text length (4) | text | zero padding
//
// The signature covers the recipients as well, so a recipient cannot
// re-encrypt a signed message to a third party and pass it off as having
// been sent to them. Single-recipient messages stay at version 1 so that
// older versions of hush can still read them. Padding to a multiple of
// padBlock hides the exact length of short secrets such as passwords.
package message

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
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
	// ErrBadSignature means the signature does not match, so the claimed
	// sender is not authentic or did not address the message to this key.
	ErrBadSignature = errors.New("signature verification failed")
	// ErrCorrupt means the message was damaged or is not a valid message.
	ErrCorrupt = errors.New("message is damaged or invalid")
	// ErrTooLong means the text exceeds MaxTextSize.
	ErrTooLong = errors.New("text is too long")
	// ErrNotText means the input to seal is not valid UTF-8, which Open would reject.
	ErrNotText = errors.New("text is not valid UTF-8")
	// ErrRecipients means the recipient list is empty, too long or has duplicates.
	ErrRecipients = errors.New("invalid list of recipients")
)

const (
	// MaxTextSize bounds the text of a message; hush is for short secrets, not files.
	MaxTextSize = 1 << 20
	// MaxRecipients bounds the recipients of one message.
	MaxRecipients = 64
)

var magic = []byte("hush.msg\x00")

const (
	padBlock = 128
	sigSize  = 64
	// fixedHeader is everything before the text of a version 1 envelope.
	fixedHeader = 9 + 1 + keys.PublicKeySize + 8 + sigSize + 4
	maxHeader   = fixedHeader + 2 + MaxRecipients*keys.PublicKeySize
)

const (
	beginMarker = "BEGIN AGE ENCRYPTED FILE"
	endMarker   = "END AGE ENCRYPTED FILE"
	// Messengers and phone keyboards may turn "-----" into en or em dashes.
	dashes = "-–—"
)

// Seal signs text with from's key and encrypts it to every key in to. The
// result is armored text.
func Seal(text string, from *keys.Identity, to []keys.PublicKey, now time.Time) (string, error) {
	switch {
	case len(text) > MaxTextSize:
		return "", ErrTooLong
	case !utf8.ValidString(text):
		return "", ErrNotText
	case len(to) == 0 || len(to) > MaxRecipients:
		return "", ErrRecipients
	}
	recipients := make([]age.Recipient, 0, len(to))
	for i, pk := range to {
		if slices.Contains(to[:i], pk) {
			return "", ErrRecipients
		}
		recipients = append(recipients, pk.Recipient())
	}
	payload := envelope(text, from, to, now)

	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, recipients...)
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

func envelope(text string, from *keys.Identity, to []keys.PublicKey, now time.Time) []byte {
	sender := from.Public().Bytes()
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(now.Unix()))

	var b bytes.Buffer
	b.Write(magic)
	if len(to) == 1 {
		b.WriteByte(1)
		b.Write(sender)
		b.Write(ts[:])
		b.Write(from.Sign(signedDataV1(to[0], sender, ts[:], []byte(text))))
	} else {
		list := recipientList(to)
		b.WriteByte(2)
		b.Write(sender)
		b.Write(ts[:])
		b.Write(from.Sign(signedDataV2(sender, ts[:], list, []byte(text))))
		b.Write(list)
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(text)))
	b.Write(n[:])
	b.WriteString(text)
	if r := b.Len() % padBlock; r != 0 {
		b.Write(make([]byte, padBlock-r))
	}
	return b.Bytes()
}

// recipientList encodes the count and keys of a version 2 envelope.
func recipientList(to []keys.PublicKey) []byte {
	b := binary.BigEndian.AppendUint16(nil, uint16(len(to)))
	for _, pk := range to {
		b = append(b, pk.Bytes()...)
	}
	return b
}

func signedDataV1(recipient keys.PublicKey, sender, ts, text []byte) []byte {
	var b bytes.Buffer
	b.WriteString("hush/v1 signature\x00")
	b.Write(recipient.Bytes())
	b.Write(sender)
	b.Write(ts)
	b.Write(text)
	return b.Bytes()
}

// signedDataV2 uses its own prefix so that a signature from one version can
// never verify as the other.
func signedDataV2(sender, ts, recipientList, text []byte) []byte {
	var b bytes.Buffer
	b.WriteString("hush/v2 signature\x00")
	b.Write(sender)
	b.Write(ts)
	b.Write(recipientList)
	b.Write(text)
	return b.Bytes()
}

// Opened is a successfully decrypted message.
type Opened struct {
	Text string
	// Sender is the verified signer, or nil for an unsigned message such as
	// one produced by the plain age CLI.
	Sender *keys.PublicKey
	// Recipients lists everyone the sender addressed, including the reader;
	// nil if unsigned.
	Recipients []keys.PublicKey
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
	const maxPlain = maxHeader + MaxTextSize + padBlock
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

// envelopeReader consumes an envelope front to back; once short, it stays
// short, so the caller checks for truncation once at the end.
type envelopeReader struct {
	p     []byte
	short bool
}

func (r *envelopeReader) next(n int) []byte {
	if r.short || n < 0 || n > len(r.p) {
		r.short = true
		return nil
	}
	b := r.p[:n]
	r.p = r.p[n:]
	return b
}

func openEnvelope(p []byte, me keys.PublicKey) (*Opened, error) {
	r := &envelopeReader{p: p[len(magic):]}
	ver := r.next(1)
	if r.short || ver[0] != 1 && ver[0] != 2 {
		return nil, fmt.Errorf("%w: unsupported version", ErrCorrupt)
	}
	senderRaw := r.next(keys.PublicKeySize)
	ts := r.next(8)
	sig := r.next(sigSize)

	var list []byte
	var recipients []keys.PublicKey
	if ver[0] == 2 {
		count := r.next(2)
		if r.short {
			return nil, fmt.Errorf("%w: truncated", ErrCorrupt)
		}
		n := int(binary.BigEndian.Uint16(count))
		if n == 0 || n > MaxRecipients {
			return nil, fmt.Errorf("%w: bad recipient count", ErrCorrupt)
		}
		keysRaw := r.next(n * keys.PublicKeySize)
		if r.short {
			return nil, fmt.Errorf("%w: truncated", ErrCorrupt)
		}
		list = append(slices.Clone(count), keysRaw...)
		for i := 0; i < n; i++ {
			pk, err := keys.PublicKeyFromBytes(keysRaw[i*keys.PublicKeySize : (i+1)*keys.PublicKeySize])
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
			}
			if slices.Contains(recipients, pk) {
				return nil, fmt.Errorf("%w: duplicate recipient", ErrCorrupt)
			}
			recipients = append(recipients, pk)
		}
	}
	length := r.next(4)
	if r.short {
		return nil, fmt.Errorf("%w: truncated", ErrCorrupt)
	}
	n := binary.BigEndian.Uint32(length)
	if uint64(n) > uint64(len(r.p)) {
		return nil, fmt.Errorf("%w: bad length", ErrCorrupt)
	}
	text := r.next(int(n))

	sender, err := keys.PublicKeyFromBytes(senderRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	var signed []byte
	if ver[0] == 1 {
		signed = signedDataV1(me, senderRaw, ts, text)
		recipients = []keys.PublicKey{me}
	} else {
		// Being able to decrypt is not enough: a recipient may have
		// re-encrypted the envelope to someone the sender never addressed.
		if !slices.Contains(recipients, me) {
			return nil, ErrBadSignature
		}
		signed = signedDataV2(senderRaw, ts, list, text)
	}
	if !sender.Verify(signed, sig) {
		return nil, ErrBadSignature
	}
	if !utf8.Valid(text) {
		return nil, fmt.Errorf("%w: not text", ErrCorrupt)
	}
	return &Opened{
		Text:       string(text),
		Sender:     &sender,
		Recipients: recipients,
		SentAt:     time.Unix(int64(binary.BigEndian.Uint64(ts)), 0),
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
