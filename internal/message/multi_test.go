package message

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/johnnyjack123/hush/internal/keys"
)

// reencrypt wraps an envelope in a fresh age file, as a recipient who has
// decrypted it could.
func reencrypt(t *testing.T, payload []byte, to ...*keys.Identity) string {
	t.Helper()
	var rs []age.Recipient
	for _, id := range to {
		rs = append(rs, id.Public().Recipient())
	}
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, rs...)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(payload)
	w.Close()
	aw.Close()
	return buf.String()
}

func pks(ids ...*keys.Identity) []keys.PublicKey {
	var out []keys.PublicKey
	for _, id := range ids {
		out = append(out, id.Public())
	}
	return out
}

// listOffset is where the recipient list of a version 2 envelope starts.
const listOffset = 9 + 1 + keys.PublicKeySize + 8 + sigSize

func TestSealOpenSeveralRecipients(t *testing.T) {
	alice, bob, carol, dave := mustIdentity(t), mustIdentity(t), mustIdentity(t), mustIdentity(t)
	sealed, err := Seal("team password", alice, pks(bob, carol), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*keys.Identity{bob, carol} {
		o, err := Open(sealed, r)
		if err != nil {
			t.Fatal(err)
		}
		if o.Text != "team password" || *o.Sender != alice.Public() || !slices.Equal(o.Recipients, pks(bob, carol)) {
			t.Fatalf("opened %+v", o)
		}
	}
	if _, err := Open(sealed, dave); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("outsider: err = %v, want ErrNotForMe", err)
	}
}

func TestSingleRecipientStaysVersion1(t *testing.T) {
	alice, bob, carol := mustIdentity(t), mustIdentity(t), mustIdentity(t)
	if v := envelope("x", alice, pks(bob), time.Now())[len(magic)]; v != 1 {
		t.Fatalf("single recipient: version %d, want 1 for older readers", v)
	}
	if v := envelope("x", alice, pks(bob, carol), time.Now())[len(magic)]; v != 2 {
		t.Fatalf("two recipients: version %d, want 2", v)
	}
	o, err := Open(reencrypt(t, envelope("x", alice, pks(bob), time.Now()), bob), bob)
	if err != nil || !slices.Equal(o.Recipients, pks(bob)) {
		t.Fatalf("version 1: %v %+v", err, o)
	}
}

func TestRecipientCannotForwardToOthers(t *testing.T) {
	alice, bob, carol, dave := mustIdentity(t), mustIdentity(t), mustIdentity(t), mustIdentity(t)
	payload := envelope("for bob and carol", alice, pks(bob, carol), time.Now())

	// Bob re-encrypts the envelope unchanged to Dave.
	if _, err := Open(reencrypt(t, payload, dave), dave); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("forwarded: err = %v, want ErrBadSignature", err)
	}
	// Bob swaps Carol for Dave in the list.
	tampered := slices.Clone(payload)
	copy(tampered[listOffset+2+keys.PublicKeySize:], dave.Public().Bytes())
	if _, err := Open(reencrypt(t, tampered, dave), dave); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("rewritten list: err = %v, want ErrBadSignature", err)
	}
	// Bob drops Carol to make the message look addressed to him alone.
	var b bytes.Buffer
	b.Write(payload[:listOffset])
	b.Write(recipientList(pks(bob)))
	b.Write(payload[listOffset+2+2*keys.PublicKeySize:])
	if _, err := Open(reencrypt(t, b.Bytes(), bob), bob); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("shortened list: err = %v, want ErrBadSignature", err)
	}
}

func TestVersion1SignatureDoesNotVerifyAsVersion2(t *testing.T) {
	alice, bob := mustIdentity(t), mustIdentity(t)
	v1 := envelope("pw", alice, pks(bob), time.Now())
	var b bytes.Buffer
	b.Write(magic)
	b.WriteByte(2)
	b.Write(v1[len(magic)+1 : listOffset])
	b.Write(recipientList(pks(bob)))
	b.Write(v1[listOffset:])
	if _, err := Open(reencrypt(t, b.Bytes(), bob), bob); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
}

func TestSealRejectsBadRecipientLists(t *testing.T) {
	alice, bob := mustIdentity(t), mustIdentity(t)
	tooMany := make([]keys.PublicKey, 0, MaxRecipients+1)
	for range MaxRecipients + 1 {
		tooMany = append(tooMany, mustIdentity(t).Public())
	}
	for name, to := range map[string][]keys.PublicKey{
		"none":      nil,
		"duplicate": pks(bob, bob),
		"too many":  tooMany,
	} {
		if _, err := Seal("x", alice, to, time.Now()); !errors.Is(err, ErrRecipients) {
			t.Errorf("%s: err = %v, want ErrRecipients", name, err)
		}
	}
}

func TestOpenEnvelopeRejectsDamage(t *testing.T) {
	alice, bob, carol := mustIdentity(t), mustIdentity(t), mustIdentity(t)
	payload := envelope("x", alice, pks(bob, carol), time.Now())
	for i := len(magic); i < len(payload); i++ {
		if _, err := openEnvelope(payload[:i], bob.Public()); err == nil && i < listOffset+2+2*keys.PublicKeySize+4+1 {
			t.Fatalf("truncated to %d bytes accepted", i)
		}
	}
	for _, count := range []uint16{0, 3, MaxRecipients + 1, 0xffff} {
		bad := slices.Clone(payload)
		bad[listOffset], bad[listOffset+1] = byte(count>>8), byte(count)
		if _, err := openEnvelope(bad, bob.Public()); err == nil {
			t.Errorf("recipient count %d accepted", count)
		}
	}
}
