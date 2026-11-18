// Package update replaces the running hush binary with the latest release.
//
// A release is trusted only if its checksums.txt carries a valid Ed25519
// signature by releaseKey, made by the release workflow with a key that only
// GitHub's secret store holds. The archive for this platform must match its
// signed checksum, and the signed file name must carry the version the
// release claims, so an old signed release cannot be passed off as new.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SignatureContext prefixes the signed checksums so that the release key's
// signatures cannot be confused with any other use of Ed25519.
const SignatureContext = "hush release checksums v1\x00"

const (
	checksumsName = "checksums.txt"
	signatureName = "checksums.txt.sig"
	maxSmallFile  = 1 << 20
	maxArchive    = 64 << 20
	maxBinary     = 128 << 20
)

var (
	// ErrDevBuild means this binary was not built by the release workflow.
	ErrDevBuild = errors.New("development build cannot update itself")
	// ErrUnsigned means the release has no valid signature by the release key.
	ErrUnsigned = errors.New("release is not signed with the hush release key")
	// ErrNoDownload means the release has no archive for this platform.
	ErrNoDownload = errors.New("release has no download for this platform")
	// ErrChecksum means the download does not match its signed checksum.
	ErrChecksum = errors.New("download does not match its signed checksum")
	// ErrNotWritable means the binary's folder cannot be written, typically
	// because a package manager installed it.
	ErrNotWritable = errors.New("cannot replace the installed binary")
	// ErrSelfTest means the downloaded binary does not run on this system.
	ErrSelfTest = errors.New("downloaded binary does not run on this system")
)

// These are variables so that tests can point them elsewhere.
var (
	latestURL  = "https://api.github.com/repos/johnnyjack123/hush/releases/latest"
	releaseKey = mustKey(releaseKeyHex)
	executable = os.Executable
	// selfTest runs a freshly downloaded binary and returns what it prints
	// for "version", which proves it runs on this machine before it
	// replaces the working one.
	selfTest = func(ctx context.Context, path string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, path, "version").Output()
		return strings.TrimSpace(string(out)), err
	}
	client = &http.Client{Timeout: 5 * time.Minute}
)

func mustKey(h string) ed25519.PublicKey {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("update: invalid release key")
	}
	return b
}

// Release is a published release and its downloads by file name.
type Release struct {
	Version string // without the leading "v"
	assets  map[string]string
}

// Latest asks GitHub for the newest published release.
func Latest(ctx context.Context) (*Release, error) {
	body, err := get(ctx, latestURL, maxSmallFile)
	if err != nil {
		return nil, err
	}
	var r struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("release information: %w", err)
	}
	version := strings.TrimPrefix(r.Tag, "v")
	if _, err := parseVersion(version); err != nil {
		return nil, err
	}
	rel := &Release{Version: version, assets: map[string]string{}}
	for _, a := range r.Assets {
		rel.assets[a.Name] = a.URL
	}
	return rel, nil
}

// Supported reports whether a binary of version current can update itself;
// development builds have no release version to compare against.
func Supported(current string) bool {
	_, err := parseVersion(current)
	return err == nil
}

// Newer reports whether version is newer than current.
func Newer(version, current string) (bool, error) {
	cur, err := parseVersion(current)
	if err != nil {
		return false, ErrDevBuild
	}
	v, err := parseVersion(version)
	if err != nil {
		return false, err
	}
	for i := range v {
		if v[i] != cur[i] {
			return v[i] > cur[i], nil
		}
	}
	return false, nil
}

func parseVersion(s string) ([3]int, error) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("unexpected version %q", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strconv.Itoa(n) != p {
			return v, fmt.Errorf("unexpected version %q", s)
		}
		v[i] = n
	}
	return v, nil
}

// ArchiveName is the release file for this platform.
func ArchiveName(version string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("hush_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("hush_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

// Install downloads r for this platform, verifies it and replaces the
// running binary. The new version takes effect on the next start.
func Install(ctx context.Context, r *Release) error {
	sums, err := r.download(ctx, checksumsName, maxSmallFile)
	if err != nil {
		return err
	}
	sig, err := r.download(ctx, signatureName, maxSmallFile)
	if err != nil {
		return err
	}
	if !VerifyChecksums(sums, sig) {
		return ErrUnsigned
	}
	name := ArchiveName(r.Version)
	want, ok := checksumFor(sums, name)
	if !ok {
		return ErrNoDownload
	}
	archive, err := r.download(ctx, name, maxArchive)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(archive); !bytes.Equal(got[:], want) {
		return ErrChecksum
	}
	bin, err := extract(archive)
	if err != nil {
		return err
	}
	return replace(ctx, bin, r.Version)
}

func (r *Release) download(ctx context.Context, name string, limit int64) ([]byte, error) {
	url, ok := r.assets[name]
	if !ok {
		if name == signatureName {
			return nil, ErrUnsigned
		}
		return nil, ErrNoDownload
	}
	return get(ctx, url, limit)
}

// VerifyChecksums reports whether sig, as stored in checksums.txt.sig, is the
// release key's signature of sums.
func VerifyChecksums(sums, sig []byte) bool {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(releaseKey, append([]byte(SignatureContext), sums...), raw)
}

// checksumFor finds name in a checksums.txt as written by GoReleaser:
// "<sha256 hex>  <file name>" per line.
func checksumFor(sums []byte, name string) ([]byte, bool) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != name {
			continue
		}
		sum, err := hex.DecodeString(fields[0])
		if err != nil || len(sum) != sha256.Size {
			return nil, false
		}
		return sum, true
	}
	return nil, false
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "hush.exe"
	}
	return "hush"
}

// extract returns the hush binary from a release archive.
func extract(archive []byte) ([]byte, error) {
	if runtime.GOOS == "windows" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name != binaryName() || f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return readLimited(rc, maxBinary)
		}
		return nil, ErrNoDownload
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, ErrNoDownload
		}
		if err != nil {
			return nil, err
		}
		if h.Name == binaryName() && h.Typeflag == tar.TypeReg {
			return readLimited(tr, maxBinary)
		}
	}
}

// replace writes bin next to the running binary, checks that it runs and
// reports version, and swaps it in.
func replace(ctx context.Context, bin []byte, version string) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, tempPattern())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotWritable, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	// Without this, a power loss right after the rename can leave an empty
	// binary on file systems that do not order the two.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if out, err := selfTest(ctx, tmp.Name()); err != nil || out != "hush "+version {
		return fmt.Errorf("%w (%q, %v)", ErrSelfTest, out, err)
	}
	return swap(tmp.Name(), exe)
}

// swap moves the new binary into place. Windows cannot overwrite a running
// executable but can rename it, so the old one is moved aside and removed
// on the next start by Cleanup.
func swap(newPath, exe string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(newPath, exe)
	}
	// A binary replaced earlier may still be running, e.g. in another
	// window, and then cannot be removed; a fresh name avoids it.
	old := exe + replacedSuffix
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		old = fmt.Sprintf("%s%s-%d", exe, replacedSuffix, time.Now().UnixNano())
	}
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("%w: %v", ErrNotWritable, err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}

// replacedSuffix marks binaries moved aside by an update. It is specific
// to hush so that Cleanup never deletes, say, a backup the user named
// hush.exe.old.
const replacedSuffix = ".replaced-by-hush"

// tempPattern names the downloaded binary. Windows treats programs whose
// name contains words like "update", "setup", "install" or "patch" as
// installers that need administrator rights, which made the self-test fail.
func tempPattern() string {
	if runtime.GOOS == "windows" {
		return ".hush-next-*.exe"
	}
	return ".hush-next-*"
}

// Cleanup removes binaries a previous update on Windows moved aside.
func Cleanup() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := executable()
	if err != nil {
		return
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return
	}
	matches, _ := filepath.Glob(exe + replacedSuffix + "*")
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hush-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Only the code: the status text and the URL come from the server and
		// end up on the terminal.
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	return readLimited(resp.Body, limit)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("download is larger than expected")
	}
	return b, nil
}
