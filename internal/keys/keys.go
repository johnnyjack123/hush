// Package keys defines hush identities and the public keys users exchange.
//
// A hush key pair bundles two independent key pairs: an age X25519 key for
// encryption and an Ed25519 key for signatures. They are kept separate rather
// than deriving one from the other so that each key is only ever used with the
// primitive it was made for.
package keys

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/edwards25519"

	"github.com/johnnyjack123/hush/internal/bech32"
)

const (
	publicKeyHRP = "hush"
	// publicKeyVersion is the first byte of an encoded public key so that the
	// format can change (e.g. to post-quantum keys) without ambiguity.
	publicKeyVersion = 1
	// PublicKeySize is the raw size of a public key: X25519 point plus Ed25519 key.
	PublicKeySize = 64
)

// ErrInvalidPublicKey is returned for strings that are not a valid hush public key.
var ErrInvalidPublicKey = errors.New("invalid public key")

// PublicKey is what users share with each other.
type PublicKey struct {
	enc  [32]byte
	sign [32]byte
}

// ParsePublicKey parses the "hush1…" text form, ignoring surrounding whitespace.
func ParsePublicKey(s string) (PublicKey, error) {
	var pk PublicKey
	hrp, data, err := bech32.Decode(strings.TrimSpace(s))
	if err != nil {
		return pk, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}
	if hrp != publicKeyHRP {
		return pk, fmt.Errorf("%w: unexpected prefix %q", ErrInvalidPublicKey, hrp)
	}
	if len(data) != 1+PublicKeySize || data[0] != publicKeyVersion {
		return pk, fmt.Errorf("%w: unsupported version or length", ErrInvalidPublicKey)
	}
	return PublicKeyFromBytes(data[1:])
}

// FindPublicKey returns the first valid "hush1…" key in s, so that a key
// pasted together with a greeting from a messenger still parses. Without a
// valid key it returns the first "hush1…" token, or "" if there is none, so
// that callers can report the invalid key.
func FindPublicKey(s string) string {
	lower := strings.ToLower(s)
	first := ""
	for rest := lower; ; {
		i := strings.Index(rest, publicKeyHRP+"1")
		if i < 0 {
			return first
		}
		j := i + len(publicKeyHRP) + 1
		for j < len(rest) && bech32.IsDataChar(rest[j]) {
			j++
		}
		token := rest[i:j]
		if _, err := ParsePublicKey(token); err == nil {
			return token
		}
		if first == "" {
			first = token
		}
		rest = rest[j:]
	}
}

// PublicKeyFromBytes builds a public key from its 64-byte raw form.
func PublicKeyFromBytes(b []byte) (PublicKey, error) {
	var pk PublicKey
	if len(b) != PublicKeySize {
		return pk, fmt.Errorf("%w: want %d bytes, got %d", ErrInvalidPublicKey, PublicKeySize, len(b))
	}
	copy(pk.enc[:], b[:32])
	copy(pk.sign[:], b[32:])
	if err := pk.validate(); err != nil {
		return PublicKey{}, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}
	if _, err := pk.recipient(); err != nil {
		return PublicKey{}, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
	}
	return pk, nil
}

// lowOrderProbe is an arbitrary fixed scalar; X25519 with it yields zero
// exactly for the small-order points.
var lowOrderProbe = func() *ecdh.PrivateKey {
	seed := sha256.Sum256([]byte("hush/v1 low-order probe"))
	k, err := ecdh.X25519().NewPrivateKey(seed[:])
	if err != nil {
		panic(err)
	}
	return k
}()

// validate rejects what a generated key never contains. Non-canonical
// encodings would give one key several fingerprints. A small-order X25519
// point makes encryption fail, and a small-order Ed25519 key accepts a
// signature anyone can forge (R = identity, S = 0 verifies for any message).
func (pk PublicKey) validate() error {
	if !canonicalX25519(pk.enc) {
		return errors.New("non-canonical encryption key")
	}
	enc, err := ecdh.X25519().NewPublicKey(pk.enc[:])
	if err != nil {
		return err
	}
	if _, err := lowOrderProbe.ECDH(enc); err != nil {
		return errors.New("weak encryption key")
	}
	p, err := new(edwards25519.Point).SetBytes(pk.sign[:])
	if err != nil || !bytes.Equal(p.Bytes(), pk.sign[:]) {
		return errors.New("invalid signing key")
	}
	if new(edwards25519.Point).MultByCofactor(p).Equal(edwards25519.NewIdentityPoint()) == 1 {
		return errors.New("weak signing key")
	}
	return nil
}

// canonicalX25519 reports whether u, little endian, is below 2^255 - 19.
func canonicalX25519(u [32]byte) bool {
	if u[31]&0x80 != 0 {
		return false
	}
	if u[31] != 0x7f || u[0] < 0xed {
		return true
	}
	for _, b := range u[1:31] {
		if b != 0xff {
			return true
		}
	}
	return false
}

// Bytes returns the 64-byte raw form.
func (pk PublicKey) Bytes() []byte {
	out := make([]byte, 0, PublicKeySize)
	out = append(out, pk.enc[:]...)
	return append(out, pk.sign[:]...)
}

// String returns the "hush1…" text form.
func (pk PublicKey) String() string {
	s, err := bech32.Encode(publicKeyHRP, append([]byte{publicKeyVersion}, pk.Bytes()...))
	if err != nil {
		panic("keys: encoding a fixed-size public key cannot fail: " + err.Error())
	}
	return s
}

// IsZero reports whether pk is the zero value.
func (pk PublicKey) IsZero() bool {
	return pk == PublicKey{}
}

func (pk PublicKey) recipient() (*age.X25519Recipient, error) {
	s, err := bech32.Encode("age", pk.enc[:])
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Recipient(s)
}

// Recipient returns the age recipient used to encrypt to this key.
func (pk PublicKey) Recipient() *age.X25519Recipient {
	r, err := pk.recipient()
	if err != nil {
		panic("keys: public key was validated on construction: " + err.Error())
	}
	return r
}

// AgeRecipient returns the "age1…" form, usable with the regular age CLI.
func (pk PublicKey) AgeRecipient() string {
	return pk.Recipient().String()
}

// Verify reports whether sig is a valid Ed25519 signature of msg by this key.
func (pk PublicKey) Verify(msg, sig []byte) bool {
	return ed25519.Verify(ed25519.PublicKey(pk.sign[:]), msg, sig)
}

// Fingerprint is a short hash of a public key, meant to be compared out of band.
// It is 96 bits because an attacker may search for a key matching any one of
// the N keys they have seen, which costs only 2^bits / N hashes: at 64 bits
// and a few thousand observed keys that is hours on a GPU.
type Fingerprint [12]byte

// Fingerprint returns the fingerprint of pk.
func (pk PublicKey) Fingerprint() Fingerprint {
	h := sha256.Sum256(append([]byte("hush/v1 fingerprint\x00"), pk.Bytes()...))
	var f Fingerprint
	copy(f[:], h[:])
	return f
}

// String returns the full form, e.g. "7F3A-91C2-04BE-1D77-A0C3-5E19".
func (f Fingerprint) String() string {
	groups := make([]string, 0, len(f)/2)
	for i := 0; i < len(f); i += 2 {
		groups = append(groups, fmt.Sprintf("%02X%02X", f[i], f[i+1]))
	}
	return strings.Join(groups, "-")
}

// Short returns the first two groups, for recognising a key at a glance.
// It is too short to verify a key with.
func (f Fingerprint) Short() string {
	return f.String()[:9]
}

// Identity is the user's private key pair.
type Identity struct {
	enc     *age.X25519Identity
	sign    ed25519.PrivateKey
	public  PublicKey
	Created time.Time
}

// Generate creates a new random identity.
func Generate() (*Identity, error) {
	enc, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	_, sign, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return newIdentity(enc, sign, time.Now().UTC())
}

func newIdentity(enc *age.X25519Identity, sign ed25519.PrivateKey, created time.Time) (*Identity, error) {
	_, encPub, err := bech32.Decode(enc.Recipient().String())
	if err != nil {
		return nil, err
	}
	raw := append(encPub, sign.Public().(ed25519.PublicKey)...)
	pub, err := PublicKeyFromBytes(raw)
	if err != nil {
		return nil, err
	}
	return &Identity{enc: enc, sign: sign, public: pub, Created: created}, nil
}

// Public returns the public key to share with contacts.
func (id *Identity) Public() PublicKey { return id.public }

// AgeIdentity returns the age identity used for decryption.
func (id *Identity) AgeIdentity() *age.X25519Identity { return id.enc }

// Sign returns an Ed25519 signature of msg.
func (id *Identity) Sign(msg []byte) []byte { return ed25519.Sign(id.sign, msg) }

type identityFile struct {
	Version     int       `json:"version"`
	Created     time.Time `json:"created"`
	AgeIdentity string    `json:"age_identity"`
	SigningSeed []byte    `json:"signing_seed"`
	PublicKey   string    `json:"public_key"`
}

// Marshal serialises the identity, including its private keys.
func (id *Identity) Marshal() ([]byte, error) {
	return json.MarshalIndent(identityFile{
		Version:     1,
		Created:     id.Created,
		AgeIdentity: id.enc.String(),
		SigningSeed: id.sign.Seed(),
		PublicKey:   id.public.String(),
	}, "", "  ")
}

// ParseIdentity is the inverse of [Identity.Marshal].
func ParseIdentity(data []byte) (*Identity, error) {
	var f identityFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("parse identity: unsupported version %d", f.Version)
	}
	enc, err := age.ParseX25519Identity(f.AgeIdentity)
	if err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	if len(f.SigningSeed) != ed25519.SeedSize {
		return nil, errors.New("parse identity: invalid signing key")
	}
	return newIdentity(enc, ed25519.NewKeyFromSeed(f.SigningSeed), f.Created)
}
