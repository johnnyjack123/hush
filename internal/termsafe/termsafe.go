// Package termsafe neutralises untrusted text before it reaches a terminal.
//
// Decrypted messages come from whoever sent them. Written to a terminal
// unfiltered, escape sequences in them could move the cursor to erase or fake
// hush's own sender line, write to the clipboard (OSC 52) or leave the
// alternate screen.
package termsafe

import (
	"strings"
	"unicode/utf8"
)

// Text replaces every character a terminal would interpret with a visible
// placeholder, keeping newlines and tabs: C0 controls become their Unicode
// control pictures (ESC shows as ␛, CR as ␍), DEL becomes ␡, and C1 controls
// and bidirectional overrides become U+FFFD, as do invalid UTF-8 bytes.
func Text(s string) string {
	if clean(s, true) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(replace(r, true))
	}
	return b.String()
}

// Line is like Text but also replaces newlines and tabs, for values shown on
// a single line such as names.
func Line(s string) string {
	if clean(s, false) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(replace(r, false))
	}
	return b.String()
}

// IsClean reports whether s contains no characters that Line would replace.
func IsClean(s string) bool { return clean(s, false) }

func clean(s string, multiline bool) bool {
	// Invalid UTF-8 must be rebuilt too: a lone byte 0x9B is CSI to terminals
	// that honour 8-bit C1 controls.
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if replace(r, multiline) != r {
			return false
		}
	}
	return true
}

func replace(r rune, multiline bool) rune {
	switch {
	case multiline && (r == '\n' || r == '\t'):
		return r
	case r < 0x20:
		return 0x2400 + r
	case r == 0x7f:
		return '␡'
	case r >= 0x80 && r <= 0x9f, isBidiControl(r):
		return '\uFFFD'
	}
	return r
}

// isBidiControl reports the formatting characters that reorder how a line is
// displayed (Trojan Source, CVE-2021-42574).
func isBidiControl(r rune) bool {
	return r == '\u061C' || r == '\u200E' || r == '\u200F' ||
		r >= '\u202A' && r <= '\u202E' || r >= '\u2066' && r <= '\u2069'
}
