package store

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/johnnyjack123/hush/internal/keys"
)

func rememberedStore(t *testing.T) (*Store, *keys.Identity) {
	t.Helper()
	keyring.MockInit()
	s := &Store{Dir: t.TempDir()}
	id, _ := keys.Generate()
	if err := s.CreateIdentity(id, "pw"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remember(id); err != nil {
		t.Fatal(err)
	}
	return s, id
}

func TestRememberedDeviceUnlocks(t *testing.T) {
	s, id := rememberedStore(t)
	if !s.Remembered() {
		t.Fatal("not remembered")
	}
	back, err := s.LoadRemembered()
	if err != nil || back.Public() != id.Public() {
		t.Fatalf("LoadRemembered: %v", err)
	}
	// The copy on disk must be useless without the keyring.
	data, _ := os.ReadFile(s.path(deviceFile))
	if strings.Contains(string(data), "AGE-SECRET-KEY") {
		t.Fatal("device file contains a plain private key")
	}
}

func TestRememberedDeviceFollowsPassphrase(t *testing.T) {
	s, id := rememberedStore(t)
	if err := s.SaveIdentity(id, "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRemembered(); err != nil {
		t.Fatalf("after passphrase change: %v", err)
	}
	if err := s.SaveIdentity(id, ""); err != nil {
		t.Fatal(err)
	}
	if s.Remembered() {
		t.Fatal("copy kept after the passphrase was removed")
	}
}

func TestRememberedDeviceRejectsReplacedIdentity(t *testing.T) {
	s, _ := rememberedStore(t)
	other, _ := keys.Generate()
	data, _ := identityData(other, "pw")
	os.WriteFile(s.path(identityFile), data, 0o600)
	if _, err := s.LoadRemembered(); !errors.Is(err, ErrDeviceStale) {
		t.Fatalf("err = %v, want ErrDeviceStale", err)
	}
}

func TestForgetRemovesKeyringEntry(t *testing.T) {
	s, _ := rememberedStore(t)
	d, _ := s.readDevice()
	if err := s.Forget(); err != nil {
		t.Fatal(err)
	}
	if s.Remembered() {
		t.Fatal("device file still there")
	}
	if _, err := keyring.Get(keyringService, d.Account); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("keyring entry left behind: %v", err)
	}
	if err := s.Forget(); err != nil {
		t.Fatalf("forgetting twice: %v", err)
	}
}

func TestMissingKeyringEntry(t *testing.T) {
	s, _ := rememberedStore(t)
	d, _ := s.readDevice()
	keyring.Delete(keyringService, d.Account)
	if _, err := s.LoadRemembered(); !errors.Is(err, ErrDeviceKeyMissing) {
		t.Fatalf("err = %v, want ErrDeviceKeyMissing", err)
	}
}

func TestRememberWithoutKeyring(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	s := &Store{Dir: t.TempDir()}
	id, _ := keys.Generate()
	s.CreateIdentity(id, "pw")
	if err := s.Remember(id); !errors.Is(err, ErrNoKeyring) {
		t.Fatalf("err = %v, want ErrNoKeyring", err)
	}
	if s.Remembered() {
		t.Fatal("device file written although the keyring failed")
	}
}

func TestCreateIdentityForgetsStaleDevice(t *testing.T) {
	s, _ := rememberedStore(t)
	os.Remove(s.path(identityFile))
	id, _ := keys.Generate()
	if err := s.CreateIdentity(id, "pw"); err != nil {
		t.Fatal(err)
	}
	if s.Remembered() {
		t.Fatal("copy of the deleted identity survived")
	}
}
