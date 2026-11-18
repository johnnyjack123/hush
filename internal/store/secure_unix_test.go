//go:build !windows

package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenTightensPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hush")
	os.Mkdir(dir, 0o755)
	key := filepath.Join(dir, identityFile)
	os.WriteFile(key, []byte("x"), 0o644)
	os.Chmod(dir, 0o777)
	t.Setenv("HUSH_HOME", dir)
	if _, err := Open(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, key: 0o600} {
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != want {
			t.Errorf("%s: mode %o, want %o", path, fi.Mode().Perm(), want)
		}
	}
}
