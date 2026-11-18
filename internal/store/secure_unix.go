//go:build !windows

package store

import (
	"os"
	"syscall"
)

// secure refuses a data directory owned by someone else, who could swap
// contact keys at will, and tightens permissions that let other users read
// the private key, as happens when a backup is restored under a lax umask.
func secure(dir string, files ...string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return ErrUnsafeDir
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	for _, f := range files {
		fi, err := os.Lstat(f)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 == 0 {
			continue
		}
		if err := os.Chmod(f, 0o600); err != nil {
			return err
		}
	}
	return nil
}
