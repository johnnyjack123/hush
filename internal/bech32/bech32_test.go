package bech32

import (
	"bytes"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestBIP173Vectors(t *testing.T) {
	valid := []string{
		"A12UEL5L",
		"a12uel5l",
		"abcdef1qpzry9x8gf2tvdw0s3jn54khce6mua7lmqqqxw",
		"split1checkupstagehandshakeupstreamerranterredcaperred2y9e3w",
	}
	for _, s := range valid {
		if _, _, err := Decode(s); err != nil {
			t.Errorf("Decode(%q) = %v, want valid", s, err)
		}
	}
	invalid := []string{
		"pzry9x0s0muk",  // no separator
		"1pzry9x0s0muk", // empty hrp
		"x1b4n0q5v",     // invalid data character
		"li1dgmt3",      // checksum too short
		"A1G7SGD8",      // checksum computed with upper-case hrp
		"a12UEL5L",      // mixed case
		"abcdef1qpzry9x8gf2tvdw0s3jn54khce6mua7lmqqqxx", // wrong checksum
	}
	for _, s := range invalid {
		if _, _, err := Decode(s); err == nil {
			t.Errorf("Decode(%q) succeeded, want error", s)
		}
	}
}

func TestInteropWithAge(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{id.Recipient().String(), id.String()} {
		hrp, data, err := Decode(s)
		if err != nil {
			t.Fatalf("Decode(%q): %v", s, err)
		}
		if len(data) != 32 {
			t.Fatalf("got %d bytes, want 32", len(data))
		}
		if strings.ToUpper(s) == s {
			hrp = strings.ToUpper(hrp)
		}
		again, err := Encode(hrp, data)
		if err != nil {
			t.Fatal(err)
		}
		if again != s {
			t.Fatalf("round trip mismatch:\n got %s\nwant %s", again, s)
		}
	}
}

func TestLongRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte{0xa5, 0x00, 0xff}, 30)
	s, err := Encode("hush", data)
	if err != nil {
		t.Fatal(err)
	}
	hrp, got, err := Decode(s)
	if err != nil || hrp != "hush" || !bytes.Equal(got, data) {
		t.Fatalf("round trip failed: hrp=%q err=%v", hrp, err)
	}
	// Flip one character: the checksum must catch it.
	broken := []byte(s)
	i := len(broken) / 2
	if broken[i] == 'q' {
		broken[i] = 'p'
	} else {
		broken[i] = 'q'
	}
	if _, _, err := Decode(string(broken)); err == nil {
		t.Fatal("single character error was not detected")
	}
}
