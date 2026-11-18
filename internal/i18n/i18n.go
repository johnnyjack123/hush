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
	{keys.ErrInvalidPublicKey, "err.invalid_key"},
	{message.ErrNoMessage, "err.no_message"},
	{message.ErrNotForMe, "err.not_for_me"},
	{message.ErrBadSignature, "err.bad_signature"},
	{message.ErrCorrupt, "err.corrupt"},
	{message.ErrTooLong, "err.too_long"},
}

// Err returns a user-facing description of err.
func Err(err error) string {
	for _, e := range errorKeys {
		if errors.Is(err, e.err) {
			return T(e.key)
		}
	}
	return err.Error()
}
