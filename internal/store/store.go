// Package store persists the identity and the contact book on the local disk.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/johnnyjack123/hush/internal/keys"
)

const (
	identityFile = "identity.key"
	contactsFile = "contacts.json"
)

var (
	// ErrNoIdentity means no key pair has been created yet.
	ErrNoIdentity = errors.New("no identity found")
	// ErrPassphraseRequired means the identity is locked and no passphrase was given.
	ErrPassphraseRequired = errors.New("passphrase required")
	// ErrWrongPassphrase means the passphrase did not unlock the identity.
	ErrWrongPassphrase = errors.New("wrong passphrase")
	// ErrEmptyName means a contact name is blank.
	ErrEmptyName = errors.New("contact name is empty")
	// ErrDuplicateName means another contact already has this name.
	ErrDuplicateName = errors.New("a contact with this name already exists")
	// ErrDuplicateKey means another contact already has this public key.
	ErrDuplicateKey = errors.New("a contact with this key already exists")
)

// scryptWorkFactor is age's default (about one second); tests lower it.
var scryptWorkFactor = 18

// Store is a directory holding hush's files.
type Store struct {
	Dir string
}

// Open returns the store in $HUSH_HOME, or in the user's configuration
// directory (e.g. %AppData%\hush, ~/.config/hush), creating it if needed.
func Open() (*Store, error) {
	dir := os.Getenv("HUSH_HOME")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, "hush")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// HasIdentity reports whether a key pair exists.
func (s *Store) HasIdentity() bool {
	_, err := os.Stat(s.path(identityFile))
	return err == nil
}

// IdentityLocked reports whether the identity is protected by a passphrase.
func (s *Store) IdentityLocked() (bool, error) {
	data, err := os.ReadFile(s.path(identityFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, ErrNoIdentity
	}
	if err != nil {
		return false, err
	}
	return isArmored(data), nil
}

func isArmored(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(data), []byte(armor.Header))
}

// LoadIdentity reads the identity. passphrase is ignored if the identity is not locked.
func (s *Store) LoadIdentity(passphrase string) (*keys.Identity, error) {
	data, err := os.ReadFile(s.path(identityFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoIdentity
	}
	if err != nil {
		return nil, err
	}
	if isArmored(data) {
		if passphrase == "" {
			return nil, ErrPassphraseRequired
		}
		if data, err = decryptWithPassphrase(data, passphrase); err != nil {
			return nil, err
		}
	}
	return keys.ParseIdentity(data)
}

func decryptWithPassphrase(data []byte, passphrase string) ([]byte, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(armor.NewReader(bytes.NewReader(data)), id)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return nil, ErrWrongPassphrase
		}
		return nil, err
	}
	return io.ReadAll(r)
}

// SaveIdentity writes the identity, encrypted with passphrase unless it is empty.
func (s *Store) SaveIdentity(id *keys.Identity, passphrase string) error {
	data, err := id.Marshal()
	if err != nil {
		return err
	}
	if passphrase != "" {
		r, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return err
		}
		r.SetWorkFactor(scryptWorkFactor)
		var buf bytes.Buffer
		aw := armor.NewWriter(&buf)
		w, err := age.Encrypt(aw, r)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		if err := aw.Close(); err != nil {
			return err
		}
		data = buf.Bytes()
	}
	return writeFileAtomic(s.path(identityFile), data, 0o600)
}

// Contact is a person the user can send messages to.
type Contact struct {
	Name string `json:"name"`
	Key  string `json:"public_key"`
	// Verified means the user confirmed the fingerprint with the contact out of band.
	Verified bool      `json:"verified"`
	Added    time.Time `json:"added"`
}

// PublicKey returns the parsed key; contacts are validated when loaded or added.
func (c Contact) PublicKey() keys.PublicKey {
	pk, _ := keys.ParsePublicKey(c.Key)
	return pk
}

// Book is the contact list, kept sorted by name.
type Book struct {
	Contacts []Contact `json:"contacts"`
}

// LoadContacts reads the contact book; a missing file yields an empty book.
func (s *Store) LoadContacts() (*Book, error) {
	data, err := os.ReadFile(s.path(contactsFile))
	if errors.Is(err, os.ErrNotExist) {
		return &Book{}, nil
	}
	if err != nil {
		return nil, err
	}
	var b Book
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", contactsFile, err)
	}
	for _, c := range b.Contacts {
		if _, err := keys.ParsePublicKey(c.Key); err != nil {
			return nil, fmt.Errorf("%s: contact %q: %w", contactsFile, c.Name, err)
		}
	}
	b.sort()
	return &b, nil
}

// SaveContacts writes the contact book.
func (s *Store) SaveContacts(b *Book) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(contactsFile), append(data, '\n'), 0o600)
}

func (b *Book) sort() {
	sort.SliceStable(b.Contacts, func(i, j int) bool {
		return strings.ToLower(b.Contacts[i].Name) < strings.ToLower(b.Contacts[j].Name)
	})
}

// Index returns the position of the contact with the given name, or -1.
func (b *Book) Index(name string) int {
	for i, c := range b.Contacts {
		if strings.EqualFold(c.Name, strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// ByKey returns the contact with the given public key, or nil.
func (b *Book) ByKey(pk keys.PublicKey) *Contact {
	for i := range b.Contacts {
		if b.Contacts[i].PublicKey() == pk {
			return &b.Contacts[i]
		}
	}
	return nil
}

// Add inserts a contact and returns its index in the sorted list.
func (b *Book) Add(name string, pk keys.PublicKey) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1, ErrEmptyName
	}
	if b.Index(name) >= 0 {
		return -1, ErrDuplicateName
	}
	if b.ByKey(pk) != nil {
		return -1, ErrDuplicateKey
	}
	b.Contacts = append(b.Contacts, Contact{Name: name, Key: pk.String(), Added: time.Now().UTC()})
	b.sort()
	return b.Index(name), nil
}

// Rename changes the name of the contact at i and returns its new index.
func (b *Book) Rename(i int, name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return i, ErrEmptyName
	}
	if j := b.Index(name); j >= 0 && j != i {
		return i, ErrDuplicateName
	}
	b.Contacts[i].Name = name
	b.sort()
	return b.Index(name), nil
}

// Remove deletes the contact at i.
func (b *Book) Remove(i int) {
	b.Contacts = append(b.Contacts[:i], b.Contacts[i+1:]...)
}

// writeFileAtomic replaces path in one step so a crash never leaves a half-written key file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
