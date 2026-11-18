//go:build !windows

package i18n

import "os"

func systemLanguage() string {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(env); v != "" && v != "C" && v != "POSIX" {
			return v
		}
	}
	return ""
}
