package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCatalogsHaveSameKeys(t *testing.T) {
	for name, cat := range catalogs {
		for k := range en {
			if _, ok := cat[k]; !ok {
				t.Errorf("%s: missing key %q", name, k)
			}
		}
		for k := range cat {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: key %q not in en", name, k)
			}
		}
	}
}

// TestUsedKeysExist catches typos in T("…") calls, which would otherwise
// show the raw key to users.
func TestUsedKeysExist(t *testing.T) {
	re := regexp.MustCompile(`\bT\("([a-z0-9_.]+)"`)
	files, _ := filepath.Glob("../*/*.go")
	main, _ := filepath.Glob("../../*.go")
	files = append(files, main...)
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			seen++
			if _, ok := en[m[1]]; !ok {
				t.Errorf("%s: unknown key %q", f, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("no T() calls found; is the glob wrong?")
	}
}

func TestMatch(t *testing.T) {
	for in, want := range map[string]string{
		"de_DE.UTF-8": "de", "de-AT": "de", "en_US": "en", "fr_FR": "en", "": "en", "C": "en",
	} {
		if got := match(in); got != want {
			t.Errorf("match(%q) = %q, want %q", in, got, want)
		}
	}
}
