// Package i18n holds all user-facing text and picks the language from the environment.
//
// The language is taken from $HUSH_LANG if set, otherwise from the system
// (the Windows UI language, or LC_ALL / LC_MESSAGES / LANG elsewhere), and
// falls back to English.
package i18n

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/message"
	"github.com/johnnyjack123/hush/internal/store"
	"github.com/johnnyjack123/hush/internal/termsafe"
	"github.com/johnnyjack123/hush/internal/update"
)

var catalogs = map[string]map[string]string{
	"de": de,
	"en": en,
}

var lang = detect()

func detect() string {
	if v := os.Getenv("HUSH_LANG"); v != "" {
		return match(v)
	}
	return match(systemLanguage())
}

// match maps a locale such as "de_DE.UTF-8" or "de-AT" to a catalog.
func match(locale string) string {
	l := strings.ToLower(locale)
	if len(l) >= 2 {
		if _, ok := catalogs[l[:2]]; ok {
			return l[:2]
		}
	}
	return "en"
}

// Lang returns the active language code.
func Lang() string { return lang }

// T returns the text for key in the active language, formatted with args if any.
func T(key string, args ...any) string {
	s, ok := catalogs[lang][key]
	if !ok {
		if s, ok = en[key]; !ok {
			return key
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Date formats t as a local calendar date.
func Date(t time.Time) string { return t.Local().Format(T("fmt.date")) }

// Time formats t as a local date and time of day.
func Time(t time.Time) string { return t.Local().Format(T("fmt.datetime")) }

var errorKeys = []struct {
	err error
	key string
}{
	{store.ErrNoIdentity, "err.no_identity"},
	{store.ErrPassphraseRequired, "err.passphrase_required"},
	{store.ErrWrongPassphrase, "err.wrong_passphrase"},
	{store.ErrEmptyName, "err.empty_name"},
	{store.ErrDuplicateName, "err.duplicate_name"},
	{store.ErrDuplicateKey, "err.duplicate_key"},
	{store.ErrInvalidName, "err.invalid_name"},
	{store.ErrContactsChanged, "err.contacts_changed"},
	{store.ErrIdentityExists, "err.identity_exists"},
	{store.ErrUnsafeDir, "err.unsafe_dir"},
	{store.ErrNoKeyring, "err.no_keyring"},
	{store.ErrDeviceKeyMissing, "err.device_key_missing"},
	{store.ErrDeviceStale, "err.device_stale"},
	{message.ErrNotText, "err.not_text"},
	{message.ErrRecipients, "err.recipients"},
	{update.ErrDevBuild, "err.update_dev"},
	{update.ErrUnsigned, "err.update_unsigned"},
	{update.ErrNoDownload, "err.update_no_download"},
	{update.ErrChecksum, "err.update_checksum"},
	{update.ErrNotWritable, "err.update_not_writable"},
	{update.ErrSelfTest, "err.update_self_test"},
	{keys.ErrInvalidPublicKey, "err.invalid_key"},
	{message.ErrNoMessage, "err.no_message"},
	{message.ErrNotForMe, "err.not_for_me"},
	{message.ErrBadSignature, "err.bad_signature"},
	{message.ErrCorrupt, "err.corrupt"},
	{message.ErrTooLong, "err.too_long"},
}

// Err returns a user-facing description of err.
func Err(err error) string {
	// Checked first: it wraps errors such as an invalid key, and the user
	// needs to know which file and entry to fix.
	var ce *store.ContactsError
	if errors.As(err, &ce) {
		detail := Err(ce.Err)
		if ce.Name != "" {
			detail = fmt.Sprintf("%q: %s", ce.Name, detail)
		}
		return T("err.contacts_file", ce.Path, detail)
	}
	for _, e := range errorKeys {
		if errors.Is(err, e.err) {
			return T(e.key)
		}
	}
	return err.Error()
}

// OtherRecipients names the recipients of a message other than me: contacts
// by name, anyone else by the short form of their fingerprint.
func OtherRecipients(b *store.Book, all []keys.PublicKey, me keys.PublicKey) string {
	var names []string
	for _, pk := range all {
		if pk == me {
			continue
		}
		if c := b.ByKey(pk); c != nil {
			names = append(names, termsafe.Line(c.Name))
		} else {
			names = append(names, T("opened.unknown_recipient", pk.Fingerprint().Short()))
		}
	}
	return strings.Join(names, ", ")
}
