//go:build keyring_integration

package store

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/johnnyjack123/hush/internal/keys"
)

// TestSystemKeyring runs against the real system keyring, which unit tests
// replace with a mock. It creates one entry and removes it again:
//
//	go test -tags keyring_integration -run TestSystemKeyring ./internal/store
func TestSystemKeyring(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	id, _ := keys.Generate()
	if err := s.CreateIdentity(id, "pw"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remember(id); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	d, err := s.readDevice()
	if err != nil {
		t.Fatal(err)
	}
	back, err := s.LoadRemembered()
	if err != nil || back.Public() != id.Public() {
		t.Fatalf("LoadRemembered: %v", err)
	}
	if err := s.Forget(); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := keyring.Get(keyringService, d.Account); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("keyring entry left behind: %v", err)
	}
}
