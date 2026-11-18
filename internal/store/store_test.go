package store

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/johnnyjack123/hush/internal/keys"
)

func init() { scryptWorkFactor = 10 }

func TestIdentityWithoutPassphrase(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if s.HasIdentity() {
		t.Fatal("empty store has identity")
	}
	if _, err := s.LoadIdentity(""); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("err = %v, want ErrNoIdentity", err)
	}
	id, _ := keys.Generate()
	if err := s.SaveIdentity(id, ""); err != nil {
		t.Fatal(err)
	}
	if locked, _ := s.IdentityLocked(); locked {
		t.Fatal("identity without passphrase reported as locked")
	}
	back, err := s.LoadIdentity("")
	if err != nil || back.Public() != id.Public() {
		t.Fatalf("load failed: %v", err)
	}
}

func TestIdentityWithPassphrase(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	id, _ := keys.Generate()
	if err := s.SaveIdentity(id, "correct horse"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.path(identityFile))
	if strings.Contains(string(raw), "AGE-SECRET-KEY") {
		t.Fatal("private key stored in clear text")
	}
	if locked, _ := s.IdentityLocked(); !locked {
		t.Fatal("identity not reported as locked")
	}
	if _, err := s.LoadIdentity(""); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("err = %v, want ErrPassphraseRequired", err)
	}
	if _, err := s.LoadIdentity("wrong"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
	back, err := s.LoadIdentity("correct horse")
	if err != nil || back.Public() != id.Public() {
		t.Fatalf("load failed: %v", err)
	}
	// Removing the passphrase again.
	if err := s.SaveIdentity(back, ""); err != nil {
		t.Fatal(err)
	}
	if locked, _ := s.IdentityLocked(); locked {
		t.Fatal("still locked after removing passphrase")
	}
}

func TestContacts(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	b, err := s.LoadContacts()
	if err != nil || len(b.Contacts) != 0 {
		t.Fatalf("fresh book: %v, %d contacts", err, len(b.Contacts))
	}
	alice, _ := keys.Generate()
	bob, _ := keys.Generate()
	if _, err := b.Add("Bob", bob.Public()); err != nil {
		t.Fatal(err)
	}
	i, err := b.Add("  alice ", alice.Public())
	if err != nil || i != 0 {
		t.Fatalf("Add alice: i=%d err=%v", i, err)
	}
	if _, err := b.Add("ALICE", bob.Public()); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("err = %v, want ErrDuplicateName", err)
	}
	if _, err := b.Add("Carol", alice.Public()); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("err = %v, want ErrDuplicateKey", err)
	}
	if _, err := b.Add(" ", alice.Public()); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("err = %v, want ErrEmptyName", err)
	}
	if c := b.ByKey(bob.Public()); c == nil || c.Name != "Bob" {
		t.Fatal("ByKey did not find Bob")
	}
	if i, err := b.Rename(0, "Zoe"); err != nil || i != 1 {
		t.Fatalf("Rename: i=%d err=%v", i, err)
	}
	b.Contacts[0].Verified = true
	if err := s.SaveContacts(b); err != nil {
		t.Fatal(err)
	}
	back, err := s.LoadContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Contacts) != 2 || back.Contacts[0].Name != "Bob" || !back.Contacts[0].Verified || back.Contacts[1].Name != "Zoe" {
		t.Fatalf("unexpected contacts after reload: %+v", back.Contacts)
	}
	back.Remove(0)
	if len(back.Contacts) != 1 || back.Contacts[0].Name != "Zoe" {
		t.Fatalf("unexpected contacts after remove: %+v", back.Contacts)
	}
}
