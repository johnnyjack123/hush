package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/store"
)

// fakeClipboard replaces the system clipboard for the duration of a test.
type fakeClipboard struct {
	text   string
	secret bool
}

func useFakeClipboard(t *testing.T) *fakeClipboard {
	t.Helper()
	fc := &fakeClipboard{}
	oldRead, oldWrite, oldSecret, oldClear := clipRead, clipWrite, clipWriteSecret, clipClearIf
	clipRead = func() (string, error) { return fc.text, nil }
	clipWrite = func(s string) error { fc.text, fc.secret = s, false; return nil }
	clipWriteSecret = func(s string) error { fc.text, fc.secret = s, true; return nil }
	clipClearIf = func(s string) error {
		if fc.text == s {
			fc.text = ""
		}
		return nil
	}
	t.Cleanup(func() { clipRead, clipWrite, clipWriteSecret, clipClearIf = oldRead, oldWrite, oldSecret, oldClear })
	return fc
}

func newStoreWithIdentity(t *testing.T) (*store.Store, *keys.Identity) {
	t.Helper()
	s := &store.Store{Dir: t.TempDir()}
	id, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(id, ""); err != nil {
		t.Fatal(err)
	}
	return s, id
}

func newModel(t *testing.T, s *store.Store) Model {
	t.Helper()
	m, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	return send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

func send(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func press(m Model, keys ...tea.KeyType) Model {
	for _, k := range keys {
		m = send(m, tea.KeyMsg{Type: k})
	}
	return m
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func paste(m Model, s string) Model {
	return send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true})
}

// checkView asserts the frame fits the terminal and optionally saves it for
// manual inspection when HUSH_DUMP_VIEWS names a directory.
func checkView(t *testing.T, m Model, name string) string {
	t.Helper()
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) > m.height {
		t.Errorf("%s: %d lines, terminal has %d", name, len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Errorf("%s: line %d is %d wide, terminal is %d", name, i, w, m.width)
		}
	}
	if dir := os.Getenv("HUSH_DUMP_VIEWS"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(v)), 0o644)
	}
	return ansi.Strip(v)
}

func TestFirstRunSetup(t *testing.T) {
	useFakeClipboard(t)
	s := &store.Store{Dir: t.TempDir()}
	m := newModel(t, s)
	if m.screen != screenSetup {
		t.Fatalf("screen = %v, want setup", m.screen)
	}
	checkView(t, m, "01-setup")

	m = typeText(m, "geheim")
	m = press(m, tea.KeyEnter)
	m = typeText(m, "anders")
	m = press(m, tea.KeyEnter)
	if m.formErr == "" || m.busy != "" {
		t.Fatal("mismatched passphrases were accepted")
	}
	m = typeText(m, "geheim")
	m = press(m, tea.KeyEnter)
	if m.busy == "" {
		t.Fatal("setup did not start")
	}
	checkView(t, m, "02-setup-busy")

	// Run the background work synchronously instead of executing the command.
	id, _ := keys.Generate()
	if err := s.SaveIdentity(id, "geheim"); err != nil {
		t.Fatal(err)
	}
	m = send(m, identityMsg{kind: readyCreated, id: id, locked: true})
	if m.screen != screenMain || m.mode != modeMyKey {
		t.Fatalf("after setup: screen=%v mode=%v", m.screen, m.mode)
	}
	if v := checkView(t, m, "03-mykey"); !strings.Contains(v, id.Public().Fingerprint().String()) {
		t.Error("my key view does not show the fingerprint")
	}
}

func TestUnlock(t *testing.T) {
	useFakeClipboard(t)
	s := &store.Store{Dir: t.TempDir()}
	id, _ := keys.Generate()
	_ = s.SaveIdentity(id, "pw")
	m := newModel(t, s)
	if m.screen != screenUnlock {
		t.Fatalf("screen = %v, want unlock", m.screen)
	}
	checkView(t, m, "04-unlock")
	m = typeText(m, "pw")
	m = press(m, tea.KeyEnter)
	if m.busy == "" {
		t.Fatal("unlock did not start")
	}
	m = send(m, identityMsg{kind: readyUnlocked, err: store.ErrWrongPassphrase})
	if m.screen != screenUnlock || m.formErr == "" {
		t.Fatal("wrong passphrase not reported")
	}
	checkView(t, m, "05-unlock-error")
	m = send(m, identityMsg{kind: readyUnlocked, id: id, locked: true})
	if m.screen != screenMain || m.mode != modeBrowse {
		t.Fatalf("after unlock: screen=%v mode=%v", m.screen, m.mode)
	}
}

func TestSendAndReceive(t *testing.T) {
	clipboard := useFakeClipboard(t)
	aliceStore, alice := newStoreWithIdentity(t)
	bobStore, bob := newStoreWithIdentity(t)

	a := newModel(t, aliceStore)
	checkView(t, a, "06-empty")

	// Bob sent his key with a greeting; "a" picks it up from the clipboard.
	clipboard.text = "Hi Alice, hier mein Schlüssel: " + bob.Public().String()
	a = typeText(a, "a")
	if a.mode != modeAdd || a.key.Value() != bob.Public().String() {
		t.Fatalf("add form not prefilled: mode=%v key=%q", a.mode, a.key.Value())
	}
	a = typeText(a, "Bob")
	checkView(t, a, "07-add")
	a = press(a, tea.KeyEnter, tea.KeyEnter)
	if a.mode != modeBrowse || len(a.book.Contacts) != 1 || a.book.Contacts[0].Name != "Bob" {
		t.Fatalf("contact not added: mode=%v err=%q", a.mode, a.formErr)
	}
	checkView(t, a, "08-contact")

	// Verify Bob's fingerprint.
	a = typeText(a, "v")
	checkView(t, a, "09-confirm-verify")
	a = typeText(a, "j")
	if !a.book.Contacts[0].Verified {
		t.Fatal("contact not verified")
	}

	// Compose and seal.
	a = typeText(a, "e")
	a = typeText(a, "hunter2")
	checkView(t, a, "10-compose")
	a = press(a, tea.KeyCtrlS)
	if a.mode != modeSealed || !strings.Contains(clipboard.text, "BEGIN AGE ENCRYPTED FILE") {
		t.Fatalf("not sealed: mode=%v", a.mode)
	}
	checkView(t, a, "11-sealed")
	sealed := clipboard.text

	// Bob decrypts straight from the clipboard; Alice is unknown to him.
	b := newModel(t, bobStore)
	b = typeText(b, "d")
	if b.mode != modeOpened || b.opened.Text != "hunter2" {
		t.Fatalf("not opened: mode=%v status=%q", b.mode, b.status)
	}
	if v := checkView(t, b, "12-opened-unknown"); !strings.Contains(v, "hunter2") {
		t.Error("plain text not shown")
	}

	// Copying the secret marks it for clearing.
	b = typeText(b, "c")
	if !clipboard.secret || clipboard.text != "hunter2" {
		t.Fatal("secret not copied with WriteSecret")
	}
	b = send(b, clipboardExpiredMsg{id: b.clipID})
	if clipboard.text != "" {
		t.Fatal("clipboard not cleared after expiry")
	}

	// Save Alice from the message.
	b = typeText(b, "a")
	if b.mode != modeAdd || b.key.Value() != alice.Public().String() {
		t.Fatalf("sender key not prefilled: %q", b.key.Value())
	}
	b = typeText(b, "Alice")
	b = press(b, tea.KeyEnter, tea.KeyEnter)
	if len(b.book.Contacts) != 1 {
		t.Fatalf("sender not saved: %q", b.formErr)
	}

	// Pasting works too, and the message is detected without pressing a key.
	clipboard.text = ""
	b = typeText(b, "d")
	if b.mode != modePaste {
		t.Fatalf("mode = %v, want paste", b.mode)
	}
	checkView(t, b, "13-paste")
	b = paste(b, "Hier:\r\n"+strings.ReplaceAll(sealed, "\n", "\r\n"))
	if b.mode != modeOpened {
		t.Fatalf("pasted message not opened: status=%q", b.status)
	}
	checkView(t, b, "14-opened-known")

	// Contacts were persisted.
	reloaded, _ := bobStore.LoadContacts()
	if len(reloaded.Contacts) != 1 || reloaded.Contacts[0].Name != "Alice" {
		t.Fatal("contacts not saved to disk")
	}
}

func TestOwnKeyRejected(t *testing.T) {
	clipboard := useFakeClipboard(t)
	s, id := newStoreWithIdentity(t)
	m := newModel(t, s)
	clipboard.text = id.Public().String()
	m = typeText(m, "a")
	m = typeText(m, "Ich")
	m = press(m, tea.KeyEnter, tea.KeyEnter)
	if m.mode != modeAdd || m.formErr == "" || len(m.book.Contacts) != 0 {
		t.Fatal("own key was accepted as contact")
	}
}

func TestDeleteContactAndViews(t *testing.T) {
	useFakeClipboard(t)
	s, _ := newStoreWithIdentity(t)
	other, _ := keys.Generate()
	book, _ := s.LoadContacts()
	book.Add("Charlie mit einem sehr langen Namen", other.Public())
	_ = s.SaveContacts(book)

	m := newModel(t, s)
	m = typeText(m, "?")
	checkView(t, m, "15-help")
	m = press(m, tea.KeyEsc)
	m = typeText(m, "k")
	m = typeText(m, "p")
	checkView(t, m, "16-passphrase")
	m = press(m, tea.KeyEsc, tea.KeyEsc)

	for _, size := range [][2]int{{64, 18}, {80, 24}, {160, 50}} {
		m = send(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		checkView(t, m, "17-size")
	}
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m = typeText(m, "x")
	checkView(t, m, "18-confirm-delete")
	m = typeText(m, "n")
	if len(m.book.Contacts) != 1 {
		t.Fatal("contact deleted although cancelled")
	}
	m = typeText(m, "x")
	m = typeText(m, "y")
	if len(m.book.Contacts) != 0 {
		t.Fatal("contact not deleted")
	}
}
