package message

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/johnnyjack123/hush/internal/keys"
)

func mustIdentity(t *testing.T) *keys.Identity {
	t.Helper()
	id, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSealOpen(t *testing.T) {
	alice, bob := mustIdentity(t), mustIdentity(t)
	now := time.Unix(1_800_000_000, 0)
	sealed, err := Seal("hunter2 – äöü 🔑", alice, bob.Public(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, armor.Header) || !strings.HasSuffix(sealed, armor.Footer) {
		t.Fatalf("not armored:\n%s", sealed)
	}
	got, err := Open(sealed, bob)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hunter2 – äöü 🔑" {
		t.Fatalf("text = %q", got.Text)
	}
	if got.Sender == nil || *got.Sender != alice.Public() {
		t.Fatal("sender not reported")
	}
	if !got.SentAt.Equal(now) {
		t.Fatalf("SentAt = %v, want %v", got.SentAt, now)
	}
}

func TestOpenWrongRecipient(t *testing.T) {
	alice, bob, eve := mustIdentity(t), mustIdentity(t), mustIdentity(t)
	sealed, _ := Seal("secret", alice, bob.Public(), time.Now())
	if _, err := Open(sealed, eve); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("err = %v, want ErrNotForMe", err)
	}
}

func TestForwardedMessageFailsVerification(t *testing.T) {
	alice, bob, carol := mustIdentity(t), mustIdentity(t), mustIdentity(t)
	// Bob decrypts Alice's envelope and re-encrypts it unchanged to Carol.
	payload := envelope("for bob only", alice, bob.Public(), time.Now())
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, _ := age.Encrypt(aw, carol.Public().Recipient())
	w.Write(payload)
	w.Close()
	aw.Close()
	if _, err := Open(buf.String(), carol); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

func TestForgedSenderFailsVerification(t *testing.T) {
	alice, bob, mallory := mustIdentity(t), mustIdentity(t), mustIdentity(t)
	payload := envelope("pay mallory", mallory, bob.Public(), time.Now())
	// Claim to be Alice while keeping Mallory's signature.
	copy(payload[len(magic)+1:], alice.Public().Bytes())
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, _ := age.Encrypt(aw, bob.Public().Recipient())
	w.Write(payload)
	w.Close()
	aw.Close()
	if _, err := Open(buf.String(), bob); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

func TestOpenPlainAgeMessage(t *testing.T) {
	bob := mustIdentity(t)
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, _ := age.Encrypt(aw, bob.Public().Recipient())
	w.Write([]byte("from the age CLI"))
	w.Close()
	aw.Close()
	got, err := Open(buf.String(), bob)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sender != nil || got.Text != "from the age CLI" {
		t.Fatalf("got %+v", got)
	}
}

func TestPaddingHidesLength(t *testing.T) {
	alice, bob := mustIdentity(t), mustIdentity(t)
	short, _ := Seal("a", alice, bob.Public(), time.Now())
	long, _ := Seal(strings.Repeat("x", 100), alice, bob.Public(), time.Now())
	if len(short) != len(long) {
		t.Fatalf("lengths differ: %d vs %d", len(short), len(long))
	}
}

func TestOpenToleratesMessengerMangling(t *testing.T) {
	alice, bob := mustIdentity(t), mustIdentity(t)
	sealed, _ := Seal("pw", alice, bob.Public(), time.Now())
	lines := strings.Split(sealed, "\n")

	variants := map[string]string{
		"crlf":        strings.ReplaceAll(sealed, "\n", "\r\n"),
		"surrounding": "Hier das Passwort:\n\n" + sealed + "\n\nLG",
		"indented":    "    " + strings.Join(lines, "\n    "),
		"joined":      strings.Join(lines, " "),
		"no newlines": strings.Join(lines, ""),
		"em dashes":   strings.ReplaceAll(sealed, "-----", "——"),
		"rewrapped":   lines[0] + "\n" + rewrap(strings.Join(lines[1:len(lines)-1], ""), 40) + "\n" + lines[len(lines)-1],
	}
	for name, v := range variants {
		got, err := Open(v, bob)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got.Text != "pw" {
			t.Errorf("%s: text = %q", name, got.Text)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	alice, bob := mustIdentity(t), mustIdentity(t)
	if _, err := Open("just some text", bob); !errors.Is(err, ErrNoMessage) {
		t.Errorf("plain text: err = %v", err)
	}
	sealed, _ := Seal("pw", alice, bob.Public(), time.Now())
	lines := strings.Split(sealed, "\n")
	mid := []byte(lines[3])
	if mid[10] == 'A' {
		mid[10] = 'B'
	} else {
		mid[10] = 'A'
	}
	lines[3] = string(mid)
	if _, err := Open(strings.Join(lines, "\n"), bob); err == nil {
		t.Error("tampered ciphertext was accepted")
	}
}

func rewrap(s string, n int) string {
	var parts []string
	for len(s) > n {
		parts = append(parts, s[:n])
		s = s[n:]
	}
	return strings.Join(append(parts, s), "\n")
}

func TestOpenRejectsOversizedPlainAgeMessage(t *testing.T) {
	bob := mustIdentity(t)
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, _ := age.Encrypt(aw, bob.Public().Recipient())
	w.Write(bytes.Repeat([]byte("x"), 2*MaxTextSize))
	w.Close()
	aw.Close()
	if _, err := Open(buf.String(), bob); !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v, want ErrTooLong instead of a silently truncated text", err)
	}
}

func TestSealRejectsInvalidUTF8(t *testing.T) {
	alice, bob := mustIdentity(t), mustIdentity(t)
	if _, err := Seal("caf\xe9", alice, bob.Public(), time.Now()); !errors.Is(err, ErrNotText) {
		t.Fatalf("err = %v, want ErrNotText", err)
	}
}
