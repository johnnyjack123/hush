// Package clip reads and writes the system clipboard.
//
// On Linux this needs wl-clipboard (Wayland) or xclip/xsel (X11) installed.
package clip

import (
	"strings"

	"github.com/atotto/clipboard"
)

// Read returns the clipboard text with line endings normalised to "\n".
func Read() (string, error) {
	s, err := clipboard.ReadAll()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(s, "\r\n", "\n"), nil
}

// Write puts non-sensitive text such as a ciphertext or public key on the clipboard.
func Write(s string) error {
	return clipboard.WriteAll(s)
}

// WriteSecret puts sensitive text on the clipboard. Where the platform allows,
// the text is kept out of clipboard history and cloud clipboard sync.
func WriteSecret(s string) error {
	return writeSecret(s)
}

// ClearIf empties the clipboard if it still holds s, so that text the user
// has copied since is left alone. cleared reports whether it emptied it.
func ClearIf(s string) (cleared bool, err error) {
	cur, err := Read()
	if err != nil {
		return false, err
	}
	if cur != strings.ReplaceAll(s, "\r\n", "\n") {
		return false, nil
	}
	if err := clipboard.WriteAll(""); err != nil {
		return false, err
	}
	return true, nil
}
