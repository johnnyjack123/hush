package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnyjack123/hush/internal/keys"
)

func TestCreateIdentityNeverReplaces(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	first, _ := keys.Generate()
	if err := s.CreateIdentity(first, ""); err != nil {
		t.Fatal(err)
	}
	second, _ := keys.Generate()
	if err := s.CreateIdentity(second, ""); !errors.Is(err, ErrIdentityExists) {
		t.Fatalf("err = %v, want ErrIdentityExists", err)
	}
	back, _ := s.LoadIdentity("")
	if back.Public() != first.Public() {
		t.Fatal("existing identity was replaced")
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}
}

func TestContactNamesRejectControlCharacters(t *testing.T) {
	var b Book
	pk := func() keys.PublicKey { id, _ := keys.Generate(); return id.Public() }
	for _, name := range []string{"Bob\x1b]0;x\a", "Bob\nEve", "Bob\u202Eevil", strings.Repeat("x", MaxNameLength+1)} {
		if _, err := b.Add(name, pk()); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Add(%q) err = %v, want ErrInvalidName", name, err)
		}
	}
	i, err := b.Add("  Zoë Müller-Łukasz  ", pk())
	if err != nil || b.Contacts[i].Name != "Zoë Müller-Łukasz" {
		t.Fatalf("regular name rejected: %v", err)
	}
	if _, err := b.Rename(i, "Zoë\x07"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("Rename err = %v, want ErrInvalidName", err)
	}
}

func TestLoadContactsRejectsAmbiguousFile(t *testing.T) {
	a, _ := keys.Generate()
	b, _ := keys.Generate()
	cases := map[string]string{
		"same name":    fmt.Sprintf(`{"contacts":[{"name":"alice","public_key":%q},{"name":"Alice","public_key":%q}]}`, a.Public(), b.Public()),
		"same key":     fmt.Sprintf(`{"contacts":[{"name":"Alice","public_key":%q},{"name":"Bob","public_key":%q}]}`, a.Public(), a.Public()),
		"control char": fmt.Sprintf(`{"contacts":[{"name":"Al\u001bice","public_key":%q}]}`, a.Public()),
	}
	for name, data := range cases {
		s := &Store{Dir: t.TempDir()}
		os.WriteFile(filepath.Join(s.Dir, contactsFile), []byte(data), 0o600)
		_, err := s.LoadContacts()
		var ce *ContactsError
		if !errors.As(err, &ce) || ce.Name == "" {
			t.Errorf("%s: err = %v, want a ContactsError naming the entry", name, err)
		}
	}
}

func TestSaveContactsDetectsConcurrentChange(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	a, _ := keys.Generate()
	c, _ := keys.Generate()

	stale, _ := s.LoadContacts() // e.g. the open interface
	other, _ := s.LoadContacts() // e.g. "hush add Carol" meanwhile
	other.Add("Carol", c.Public())
	if err := s.SaveContacts(other); err != nil {
		t.Fatal(err)
	}
	stale.Add("Alice", a.Public())
	if err := s.SaveContacts(stale); !errors.Is(err, ErrContactsChanged) {
		t.Fatalf("err = %v, want ErrContactsChanged", err)
	}
	fresh, _ := s.LoadContacts()
	if fresh.Index("Carol") < 0 {
		t.Fatal("concurrent change was lost")
	}
	// Saving twice from the same book is not a conflict.
	fresh.Add("Alice", a.Public())
	if err := s.SaveContacts(fresh); err != nil {
		t.Fatal(err)
	}
	fresh.Contacts[0].Verified = true
	if err := s.SaveContacts(fresh); err != nil {
		t.Fatal(err)
	}
}
