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
	if got := pk.Fingerprint().String(); len(got) != 19 || got[:9] != pk.Fingerprint().Short() {
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
