//go:build !windows

package clip

import "github.com/atotto/clipboard"

func writeSecret(s string) error {
	return clipboard.WriteAll(s)
}
