package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		v, cur string
		want   bool
	}{
		{"0.4.0", "0.3.1", true},
		{"0.3.10", "0.3.9", true},
		{"1.0.0", "0.9.9", true},
		{"0.3.1", "0.3.1", false},
		{"0.3.0", "0.3.1", false},
	}
	for _, c := range cases {
		if got, err := Newer(c.v, c.cur); err != nil || got != c.want {
			t.Errorf("Newer(%q, %q) = %v, %v", c.v, c.cur, got, err)
		}
	}
	if _, err := Newer("0.4.0", "dev"); !errors.Is(err, ErrDevBuild) {
		t.Errorf("dev build: err = %v", err)
	}
	for _, bad := range []string{"1.2", "1.2.3-rc1", "01.2.3", "a.b.c", ""} {
		if _, err := Newer(bad, "0.1.0"); err == nil {
			t.Errorf("Newer accepted version %q", bad)
		}
	}
}

// fakeRelease serves a release the way GitHub does and installs into a
// temporary "executable".
type fakeRelease struct {
	t        *testing.T
	priv     ed25519.PrivateKey
	files    map[string][]byte
	exe      string
	selfTest string
}

func newFakeRelease(t *testing.T, version string) *fakeRelease {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	oldKey, oldURL, oldExe, oldTest := releaseKey, latestURL, executable, selfTest
	t.Cleanup(func() { releaseKey, latestURL, executable, selfTest = oldKey, oldURL, oldExe, oldTest })
	releaseKey = pub

	f := &fakeRelease{t: t, priv: priv, files: map[string][]byte{}, selfTest: "hush " + version}
	f.exe = filepath.Join(t.TempDir(), binaryName())
	os.WriteFile(f.exe, []byte("old binary"), 0o755)
	executable = func() (string, error) { return f.exe, nil }
	selfTest = func(context.Context, string) (string, error) { return f.selfTest, nil }

	archive := buildArchive(t, []byte("new binary"))
	f.files[ArchiveName(version)] = archive
	sum := sha256.Sum256(archive)
	f.files[checksumsName] = []byte(hex.EncodeToString(sum[:]) + "  " + ArchiveName(version) + "\n")
	f.sign(priv)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			type asset struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			}
			var assets []asset
			for name := range f.files {
				assets = append(assets, asset{name, "http://" + r.Host + "/files/" + name})
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v" + version, "assets": assets})
			return
		}
		data, ok := f.files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	latestURL = srv.URL + "/latest"
	return f
}

func (f *fakeRelease) sign(priv ed25519.PrivateKey) {
	sig := ed25519.Sign(priv, append([]byte(SignatureContext), f.files[checksumsName]...))
	f.files[signatureName] = []byte(base64.StdEncoding.EncodeToString(sig) + "\n")
}

func (f *fakeRelease) install() error {
	f.t.Helper()
	rel, err := Latest(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return Install(context.Background(), rel)
}

func (f *fakeRelease) installed() string {
	b, _ := os.ReadFile(f.exe)
	return string(b)
}

func buildArchive(t *testing.T, bin []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("README.md")
		w.Write([]byte("readme"))
		w, _ = zw.Create(binaryName())
		w.Write(bin)
		zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("readme")}, {binaryName(), bin}} {
		tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg})
		tw.Write(f.data)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestInstall(t *testing.T) {
	f := newFakeRelease(t, "9.9.9")
	if err := f.install(); err != nil {
		t.Fatal(err)
	}
	if got := f.installed(); got != "new binary" {
		t.Fatalf("installed %q", got)
	}
	Cleanup()
	if left, _ := filepath.Glob(f.exe + replacedSuffix + "*"); len(left) != 0 {
		t.Fatalf("old binary left behind: %v", left)
	}
}

func TestCleanupKeepsUserFiles(t *testing.T) {
	newFakeRelease(t, "9.9.9")
	exe, _ := executable()
	backup := exe + ".old"
	os.WriteFile(backup, []byte("user backup"), 0o644)
	Cleanup()
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("Cleanup removed a file hush did not create: %v", err)
	}
}

// Windows asks for administrator rights before running anything whose name
// looks like an installer, which broke the self-test of every update.
func TestTempNameDoesNotLookLikeAnInstaller(t *testing.T) {
	name := strings.ToLower(tempPattern())
	for _, word := range []string{"update", "setup", "install", "patch", "upgrade"} {
		if strings.Contains(name, word) {
			t.Errorf("temporary binary name %q contains %q", name, word)
		}
	}
}

func TestInstallRefusesUnsignedOrForeignSignature(t *testing.T) {
	f := newFakeRelease(t, "9.9.9")
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	f.sign(other)
	if err := f.install(); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("foreign key: err = %v, want ErrUnsigned", err)
	}
	delete(f.files, signatureName)
	if err := f.install(); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("no signature: err = %v, want ErrUnsigned", err)
	}
	if f.installed() != "old binary" {
		t.Fatal("binary replaced despite a bad signature")
	}
}

func TestInstallRefusesTamperedArchive(t *testing.T) {
	f := newFakeRelease(t, "9.9.9")
	f.files[ArchiveName("9.9.9")] = buildArchive(t, []byte("evil binary"))
	if err := f.install(); !errors.Is(err, ErrChecksum) || f.installed() != "old binary" {
		t.Fatalf("err = %v, installed %q", err, f.installed())
	}
}

// An old, genuinely signed release must not pass as a newer one.
func TestInstallRefusesReplayedOldRelease(t *testing.T) {
	f := newFakeRelease(t, "0.3.0")
	// The API now claims v9.9.9 but serves the files signed for 0.3.0.
	old := f.files
	f2 := newFakeRelease(t, "9.9.9")
	f2.files = map[string][]byte{checksumsName: old[checksumsName], signatureName: old[signatureName], ArchiveName("0.3.0"): old[ArchiveName("0.3.0")]}
	releaseKey = f.priv.Public().(ed25519.PublicKey)
	if err := f2.install(); !errors.Is(err, ErrNoDownload) || f2.installed() != "old binary" {
		t.Fatalf("err = %v, installed %q", err, f2.installed())
	}
}

func TestInstallKeepsOldBinaryIfNewOneFails(t *testing.T) {
	f := newFakeRelease(t, "9.9.9")
	f.selfTest = "exec format error"
	if err := f.install(); !errors.Is(err, ErrSelfTest) || f.installed() != "old binary" {
		t.Fatalf("err = %v, installed %q", err, f.installed())
	}
	entries, _ := os.ReadDir(filepath.Dir(f.exe))
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
