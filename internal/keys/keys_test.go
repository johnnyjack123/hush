package keys

import (
	"errors"
	"strings"
	"testing"
)

func TestPublicKeyRoundTrip(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	s := id.Public().String()
	if !strings.HasPrefix(s, "hush1") {
		t.Fatalf("unexpected prefix: %s", s)
	}
	pk, err := ParsePublicKey("  " + strings.ToUpper(s) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if pk != id.Public() {
		t.Fatal("parsed key differs from original")
	}
	if pk.Fingerprint() != id.Public().Fingerprint() {
		t.Fatal("fingerprint not stable")
	}
	if got := pk.Fingerprint().String(); len(got) != 29 || got[:9] != pk.Fingerprint().Short() {
		t.Fatalf("unexpected fingerprint format %q", got)
	}
	if !strings.HasPrefix(pk.AgeRecipient(), "age1") {
		t.Fatalf("unexpected age recipient %q", pk.AgeRecipient())
	}
}

func TestParsePublicKeyRejects(t *testing.T) {
	id, _ := Generate()
	s := id.Public().String()
	last := "q"
	if strings.HasSuffix(s, "q") {
		last = "p"
	}
	bad := []string{
		"",
		"hello",
		id.Public().AgeRecipient(), // age key, not a hush key
		s[:len(s)-1] + last,        // checksum
		strings.Replace(s, "hush1", "hish1", 1),
	}
	for _, b := range bad {
		if _, err := ParsePublicKey(b); !errors.Is(err, ErrInvalidPublicKey) {
			t.Errorf("ParsePublicKey(%q) = %v, want ErrInvalidPublicKey", b, err)
		}
	}
}

func TestFindPublicKey(t *testing.T) {
	id, _ := Generate()
	s := id.Public().String()
	for _, in := range []string{s, "Hi! Mein Schlüssel: " + s + " – LG", "\n" + strings.ToUpper(s) + ".\n"} {
		if got := FindPublicKey(in); got != s {
			t.Errorf("FindPublicKey(%q) = %q", in, got)
		}
	}
	if got := FindPublicKey("I installed hush1.2 today, my key is " + s); got != s {
		t.Errorf("FindPublicKey skipped to an invalid token: %q", got)
	}
	if got := FindPublicKey("no key here"); got != "" {
		t.Errorf("FindPublicKey found %q in plain text", got)
	}
}

func TestIdentityMarshalRoundTrip(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	data, err := id.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseIdentity(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Public() != id.Public() || !back.Created.Equal(id.Created) {
		t.Fatal("identity changed in round trip")
	}
	msg := []byte("hello")
	if !id.Public().Verify(msg, back.Sign(msg)) {
		t.Fatal("restored signing key does not match")
	}
}

func TestSignatureRejectsOtherKey(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()
	msg := []byte("hello")
	if b.Public().Verify(msg, a.Sign(msg)) {
		t.Fatal("signature verified under the wrong key")
	}
}

func TestPublicKeyFromBytesRejectsWeakKeys(t *testing.T) {
	id, _ := Generate()
	good := id.Public().Bytes()
	withEnc := func(enc []byte) []byte { return append(append([]byte{}, enc...), good[32:]...) }
	withSign := func(sign []byte) []byte { return append(append([]byte{}, good[:32]...), sign...) }
	fill := func(first, mid, last byte) []byte {
		b := make([]byte, 32)
		for i := range b {
			b[i] = mid
		}
		b[0], b[31] = first, last
		return b
	}
	topBit := append([]byte{}, good[:32]...)
	topBit[31] |= 0x80

	bad := map[string][]byte{
		"zero X25519":                            withEnc(make([]byte, 32)),
		"X25519 u=1":                             withEnc(fill(1, 0, 0)),
		"X25519 top bit set":                     withEnc(topBit),
		"X25519 u=p (non-canon.)":                withEnc(fill(0xed, 0xff, 0x7f)),
		"Ed25519 identity":                       withSign(fill(1, 0, 0)),
		"Ed25519 order 2":                        withSign(fill(0xec, 0xff, 0x7f)),
		"Ed25519 y=p+1 (non-canonical identity)": withSign(fill(0xee, 0xff, 0x7f)),
		"Ed25519 not on curve":                   withSign(fill(2, 0, 0)),
	}
	for name, b := range bad {
		if _, err := PublicKeyFromBytes(b); !errors.Is(err, ErrInvalidPublicKey) {
			t.Errorf("%s: err = %v, want ErrInvalidPublicKey", name, err)
		}
	}
	if _, err := PublicKeyFromBytes(good); err != nil {
		t.Fatalf("generated key rejected: %v", err)
	}
}
