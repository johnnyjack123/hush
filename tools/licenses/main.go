// Command licenses writes docs/THIRD_PARTY_LICENSES.txt: the license texts of
// the Go distribution and of every module linked into a release binary.
//
//	go run ./tools/licenses          regenerate the file
//	go run ./tools/licenses -check   fail if the file is out of date
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const outFile = "docs/THIRD_PARTY_LICENSES.txt"

// targets must match the goos/goarch matrix in .goreleaser.yaml, because the
// set of linked modules differs per platform.
var targets = [][2]string{
	{"windows", "amd64"}, {"windows", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
}

// fallback holds license texts for modules that ship no license file.
var fallback = map[string]string{
	"github.com/mattn/go-localereader": "go-localereader ships no license file. Its README states " +
		"\"License: MIT\" and names\nYasuhiro Matsumoto as the author; the standard MIT License " +
		"text follows.\n\n" + mitLicense("Yasuhiro Matsumoto"),
}

const goDistribution = "Go standard library and runtime"

type component struct {
	name  string   // "module/path version", or goDistribution
	texts []string // normalized license files
}

func main() {
	check := flag.Bool("check", false, "exit with an error if "+outFile+" is out of date instead of writing it")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, "licenses:", err)
		os.Exit(1)
	}
}

func run(check bool) error {
	root, err := goOutput(nil, "list", "-m", "-f", "{{.Dir}}")
	if err != nil {
		return err
	}
	comps, err := collect(root)
	if err != nil {
		return err
	}
	out := render(comps)
	path := filepath.Join(root, filepath.FromSlash(outFile))
	if !check {
		return os.WriteFile(path, out, 0o644)
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.ReplaceAll(cur, []byte("\r\n"), []byte("\n")), out) {
		return errors.New(outFile + " is out of date; regenerate it with: go run ./tools/licenses")
	}
	return nil
}

func collect(root string) ([]component, error) {
	goroot, err := goOutput(nil, "env", "GOROOT")
	if err != nil {
		return nil, err
	}
	goFiles, err := licenseFiles(goroot)
	if err != nil {
		return nil, err
	}

	// A package may carry its own license below the module root, so every
	// directory from each linked package up to its module root is searched.
	modFiles := map[string]map[string]bool{}
	modPath := map[string]string{}
	for _, t := range targets {
		env := []string{"CGO_ENABLED=0", "GOOS=" + t[0], "GOARCH=" + t[1]}
		list, err := goOutput(env, "list", "-C", root, "-deps", "-f",
			`{{if and .Module (not .Module.Main)}}{{.Module.Path}} {{.Module.Version}}{{"\t"}}{{.Module.Path}}{{"\t"}}{{.Module.Dir}}{{"\t"}}{{.Dir}}{{end}}`, ".")
		if err != nil {
			return nil, err
		}
		for line := range strings.Lines(list) {
			f := strings.Split(strings.TrimSpace(line), "\t")
			if len(f) != 4 {
				continue
			}
			name, mdir := f[0], f[2]
			modPath[name] = f[1]
			if modFiles[name] == nil {
				modFiles[name] = map[string]bool{}
			}
			for dir := f[3]; ; dir = filepath.Dir(dir) {
				files, err := licenseFiles(dir)
				if err != nil {
					return nil, err
				}
				for _, file := range files {
					modFiles[name][file] = true
				}
				if dir == mdir || filepath.Dir(dir) == dir {
					break
				}
			}
		}
	}

	goTexts, err := readAll(goFiles)
	if err != nil {
		return nil, err
	}
	comps := []component{{name: goDistribution, texts: goTexts}}
	names := make([]string, 0, len(modFiles))
	for name := range modFiles {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		files := make([]string, 0, len(modFiles[name]))
		for file := range modFiles[name] {
			files = append(files, file)
		}
		slices.Sort(files)
		texts, err := readAll(files)
		if err != nil {
			return nil, err
		}
		if len(texts) == 0 {
			text, ok := fallback[modPath[name]]
			if !ok {
				return nil, fmt.Errorf("no license file found for %s", name)
			}
			texts = []string{text}
		}
		comps = append(comps, component{name: name, texts: texts})
	}
	return comps, nil
}

// licenseFiles returns the license-like files directly inside dir.
func licenseFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		if e.IsDir() {
			continue
		}
		for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS"} {
			if strings.HasPrefix(name, prefix) {
				files = append(files, filepath.Join(dir, e.Name()))
				break
			}
		}
	}
	return files, nil
}

func readAll(files []string) ([]string, error) {
	var texts []string
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		texts = append(texts, normalize(string(b)))
	}
	return texts, nil
}

// normalize makes the output independent of the line endings and trailing
// whitespace a module happens to ship, so -check is stable across platforms.
func normalize(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// render groups components with identical license texts, so the many
// golang.org/x modules share the Go license instead of repeating it.
func render(comps []component) []byte {
	var order []string
	groups := map[string][]string{}
	for _, c := range comps {
		key := strings.Join(c.texts, "\n\n")
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], c.name)
	}

	var b strings.Builder
	b.WriteString(`THIRD-PARTY LICENSES

hush is distributed under the MIT License (see LICENSE). Its binaries also
contain the Go standard library and runtime and the Go modules listed below.
Their license texts follow, grouped where they are identical.

This file is generated by "go run ./tools/licenses"; do not edit it by hand.

`)
	for _, c := range comps {
		fmt.Fprintf(&b, "  %s\n", c.name)
	}
	rule := strings.Repeat("=", 80)
	for _, key := range order {
		fmt.Fprintf(&b, "\n\n%s\n%s\n%s\n\n%s\n", rule, strings.Join(groups[key], "\n"), strings.Repeat("-", 80), key)
	}
	return []byte(b.String())
}

func goOutput(env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

func mitLicense(holder string) string {
	return "MIT License\n\nCopyright (c) " + holder + `

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.`
}
