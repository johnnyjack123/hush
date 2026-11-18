// Package store persists the identity and the contact book on the local disk.
package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/termsafe"
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
	// ErrInvalidName means a contact name contains control characters or is too long.
	ErrInvalidName = errors.New("contact name is invalid")
	// ErrContactsChanged means contacts.json was modified by another hush
	// process since it was loaded; saving would silently undo that change.
	ErrContactsChanged = errors.New("contacts were changed by another program")
	// ErrIdentityExists means a key pair exists already and must not be replaced.
	ErrIdentityExists = errors.New("a key pair already exists")
	// ErrUnsafeDir means the data directory belongs to another user.
	ErrUnsafeDir = errors.New("data directory belongs to another user")
)

// MaxNameLength bounds contact names, in characters.
const MaxNameLength = 64

// ContactsError describes an invalid contacts.json, naming the entry at fault.
type ContactsError struct {
	Path string
	Name string // empty if the file as a whole is unreadable
	Err  error
}

func (e *ContactsError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("%s: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("%s: contact %q: %v", e.Path, e.Name, e.Err)
}

func (e *ContactsError) Unwrap() error { return e.Err }

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
	s := &Store{Dir: dir}
	// The directory may predate hush, e.g. restored from a backup or chosen
	// with HUSH_HOME, so its permissions are not necessarily the ones above.
	if err := secure(dir, s.path(identityFile), s.path(contactsFile)); err != nil {
		return nil, err
	}
	return s, nil
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

// CreateIdentity stores a new identity like SaveIdentity, but fails with
// ErrIdentityExists instead of replacing one, which would destroy the key
// pair the user's contacts encrypt to.
func (s *Store) CreateIdentity(id *keys.Identity, passphrase string) error {
	data, err := identityData(id, passphrase)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(identityFile), data, 0o600, false)
}

// SaveIdentity writes the identity, encrypted with passphrase unless it is empty.
func (s *Store) SaveIdentity(id *keys.Identity, passphrase string) error {
	data, err := identityData(id, passphrase)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(identityFile), data, 0o600, true)
}

func identityData(id *keys.Identity, passphrase string) ([]byte, error) {
	data, err := id.Marshal()
	if err != nil || passphrase == "" {
		return data, err
	}
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	r.SetWorkFactor(scryptWorkFactor)
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, r)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := aw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

	// loaded identifies the file content the book was read from, so that
	// saving can detect a concurrent change (zero if there was no file).
	loaded [sha256.Size]byte
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
		return nil, &ContactsError{Path: s.path(contactsFile), Err: err}
	}
	// The file may have been edited by hand; reject what the book's methods
	// would otherwise resolve ambiguously, such as two contacts named alike.
	var valid Book
	for _, c := range b.Contacts {
		err := checkName(c.Name)
		if err == nil {
			_, err = keys.ParsePublicKey(c.Key)
		}
		if err == nil && valid.Index(c.Name) >= 0 {
			err = ErrDuplicateName
		}
		if err == nil && valid.ByKey(c.PublicKey()) != nil {
			err = ErrDuplicateKey
		}
		if err != nil {
			return nil, &ContactsError{Path: s.path(contactsFile), Name: c.Name, Err: err}
		}
		valid.Contacts = append(valid.Contacts, c)
	}
	b.sort()
	b.loaded = sha256.Sum256(data)
	return &b, nil
}

// SaveContacts writes the contact book. It fails with ErrContactsChanged if
// the file changed since b was loaded.
func (s *Store) SaveContacts(b *Book) error {
	var current [sha256.Size]byte
	if data, err := os.ReadFile(s.path(contactsFile)); err == nil {
		current = sha256.Sum256(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if current != b.loaded {
		return ErrContactsChanged
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeFileAtomic(s.path(contactsFile), data, 0o600, true); err != nil {
		return err
	}
	b.loaded = sha256.Sum256(data)
	return nil
}

// checkName validates a trimmed contact name. Names are shown on the
// terminal, so control and bidi characters, which could fake parts of the
// interface, are not allowed.
func checkName(name string) error {
	switch {
	case name == "":
		return ErrEmptyName
	case !termsafe.IsClean(name), utf8.RuneCountInString(name) > MaxNameLength:
		return ErrInvalidName
	}
	return nil
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
	if err := checkName(name); err != nil {
		return -1, err
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
	if err := checkName(name); err != nil {
		return i, err
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

// writeFileAtomic replaces path in one step so a crash never leaves a
// half-written key file. Without overwrite it fails with ErrIdentityExists if
// path exists; the check sits right before the rename to keep the window small.
func writeFileAtomic(path string, data []byte, perm os.FileMode, overwrite bool) error {
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
	if !overwrite {
		if _, err := os.Lstat(path); err == nil {
			return ErrIdentityExists
		}
	}
	return os.Rename(tmp.Name(), path)
}
