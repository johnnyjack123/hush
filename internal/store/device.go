package store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/zalando/go-keyring"

	"github.com/johnnyjack123/hush/internal/keys"
)

// Remembering a device keeps a second copy of the identity next to
// identity.key, encrypted to a random age key that lives in the operating
// system's keyring (Windows Credential Manager, macOS Keychain, Secret
// Service on Linux). The keyring opens when the user logs in, so hush starts
// without the passphrase on this device, while identity.key and its backups
// stay protected by the passphrase alone.

const (
	deviceFile     = "identity.device"
	keyringService = "hush"
	// keyringTimeout bounds each keyring call. It is generous because the
	// system may ask the user to unlock the keyring first, but it keeps hush
	// from hanging when, for example, D-Bus never answers.
	keyringTimeout = 30 * time.Second
)

var (
	// ErrNoKeyring means the system keyring could not store the device key.
	ErrNoKeyring = errors.New("system keyring not available")
	// ErrDeviceKeyMissing means identity.device exists but its key is gone
	// from the keyring, e.g. after the keyring was reset.
	ErrDeviceKeyMissing = errors.New("device key missing from the system keyring")
	// ErrDeviceStale means identity.key was replaced after the device was
	// remembered, so the remembered copy may be a different identity.
	ErrDeviceStale = errors.New("remembered identity does not match identity.key")

	errKeyringTimeout = errors.New("system keyring did not respond")
)

type deviceData struct {
	Version int `json:"version"`
	// Account names the keyring entry holding the device key.
	Account string `json:"account"`
	// Bound is the SHA-256 of the identity.key this copy belongs to.
	Bound string `json:"bound"`
	// Identity is the identity, armored and age-encrypted to the device key.
	Identity string `json:"identity"`
}

// Remembered reports whether this device can unlock the identity without the passphrase.
func (s *Store) Remembered() bool {
	_, err := os.Stat(s.path(deviceFile))
	return err == nil
}

// Remember stores a copy of id that the system keyring unlocks. id must be
// the identity in identity.key.
func (s *Store) Remember(id *keys.Identity) error {
	bound, err := s.identityHash()
	if err != nil {
		return err
	}
	deviceKey, err := age.GenerateX25519Identity()
	if err != nil {
		return err
	}
	plain, err := id.Marshal()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, deviceKey.Recipient())
	if err != nil {
		return err
	}
	if _, err := w.Write(plain); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := aw.Close(); err != nil {
		return err
	}

	var account [8]byte
	if _, err := rand.Read(account[:]); err != nil {
		return err
	}
	d := deviceData{Version: 1, Account: "device-" + hex.EncodeToString(account[:]), Bound: bound, Identity: buf.String()}
	if err := keyringCall(func() error { return keyring.Set(keyringService, d.Account, deviceKey.String()) }); err != nil {
		return fmt.Errorf("%w: %v", ErrNoKeyring, err)
	}
	previous, _ := s.readDevice()
	if err := s.writeDevice(d); err != nil {
		_ = keyringCall(func() error { return keyring.Delete(keyringService, d.Account) })
		return err
	}
	if previous != nil {
		_ = keyringCall(func() error { return keyring.Delete(keyringService, previous.Account) })
	}
	return nil
}

// Forget removes the remembered copy, so the passphrase is needed again.
func (s *Store) Forget() error {
	d, err := s.readDevice()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// Without the file the keyring entry is useless, so a broken file or an
	// unreachable keyring must not keep the copy around.
	if err := os.Remove(s.path(deviceFile)); err != nil {
		return err
	}
	if d != nil {
		_ = keyringCall(func() error { return keyring.Delete(keyringService, d.Account) })
	}
	return nil
}

// LoadRemembered unlocks the identity with the device key from the keyring.
func (s *Store) LoadRemembered() (*keys.Identity, error) {
	d, err := s.readDevice()
	if err != nil {
		return nil, err
	}
	bound, err := s.identityHash()
	if err != nil {
		return nil, err
	}
	if d.Bound != bound {
		return nil, ErrDeviceStale
	}
	var secret string
	err = keyringCall(func() (err error) {
		secret, err = keyring.Get(keyringService, d.Account)
		return err
	})
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrDeviceKeyMissing
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoKeyring, err)
	}
	deviceKey, err := age.ParseX25519Identity(secret)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDeviceKeyMissing, err)
	}
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader([]byte(d.Identity))), deviceKey)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", deviceFile, err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", deviceFile, err)
	}
	return keys.ParseIdentity(plain)
}

// rebindDevice follows identity.key after it was rewritten with a new
// passphrase; the identity itself, and so the remembered copy, is unchanged.
func (s *Store) rebindDevice() error {
	d, err := s.readDevice()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.Bound, err = s.identityHash(); err != nil {
		return err
	}
	return s.writeDevice(*d)
}

func (s *Store) readDevice() (*deviceData, error) {
	data, err := os.ReadFile(s.path(deviceFile))
	if err != nil {
		return nil, err
	}
	var d deviceData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", deviceFile, err)
	}
	if d.Version != 1 || d.Account == "" {
		return nil, fmt.Errorf("%s: unsupported format", deviceFile)
	}
	return &d, nil
}

func (s *Store) writeDevice(d deviceData) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(deviceFile), append(data, '\n'), 0o600, true)
}

func (s *Store) identityHash() (string, error) {
	data, err := os.ReadFile(s.path(identityFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoIdentity
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func keyringCall(f func() error) error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(keyringTimeout):
		return errKeyringTimeout
	}
}
