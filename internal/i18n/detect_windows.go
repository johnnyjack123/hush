//go:build windows

package i18n

import "golang.org/x/sys/windows"

// systemLanguage uses the Windows display language; LANG is usually unset on
// Windows, or set by tools like Git Bash without reflecting the user's choice.
func systemLanguage() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return ""
	}
	return langs[0]
}
