package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/message"
	"github.com/johnnyjack123/hush/internal/store"
)

// ageUnsigned encrypts text the way the plain age CLI, or anyone who knows
// the public key, can: without a hush envelope.
func ageUnsigned(t *testing.T, to *keys.Identity, text string) string {
	t.Helper()
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, to.Public().Recipient())
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(text))
	w.Close()
	aw.Close()
	return buf.String()
}

// runIdentityJob executes the background work started by startBusy and feeds
// its result back into the model.
func runIdentityJob(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command returned")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("expected a batch with the background job")
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if msg, ok := c().(identityMsg); ok {
			return send(m, msg)
		}
	}
	t.Fatal("no identity job in batch")
	return m
}

func withContact(t *testing.T, s *store.Store, name string) {
	t.Helper()
	other, _ := keys.Generate()
	book, _ := s.LoadContacts()
	if _, err := book.Add(name, other.Public()); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContacts(book); err != nil {
		t.Fatal(err)
	}
}

func TestDecryptedTextCannotDriveTerminal(t *testing.T) {
	clipboard := useFakeClipboard(t)
	s, id := newStoreWithIdentity(t)
	m := newModel(t, s)

	evil := "hi\x1b7\x1b[3A\r\x1b[2K✓ Signed by Alice (verified)\x1b8\x1b]52;c;aGk=\x07\x1b[?1049l"
	clipboard.text = ageUnsigned(t, id, evil)
	m = typeText(m, "d")
	if m.mode != modeOpened {
		t.Fatalf("not opened: %q", m.status)
	}
	v := m.View()
	for _, seq := range []string{"\x1b7", "\x1b[3A", "\r", "\x1b[2K", "\x1b8", "\x1b]52", "\x07", "\x1b[?1049l"} {
		if strings.Contains(v, seq) {
			t.Errorf("view passes %q through to the terminal", seq)
		}
	}
	if !strings.Contains(ansi.Strip(v), "␛[2K") {
		t.Error("escape sequence is not shown visibly")
	}
	m = typeText(m, "c")
	if clipboard.text != evil {
		t.Error("copying must keep the exact text")
	}
}

// On Windows a paste arrives as one key event per character. Text after the
// message must not act as commands once the message has opened.
func TestPastedKeysCannotRunCommands(t *testing.T) {
	clipboard := useFakeClipboard(t)
	s, id := newStoreWithIdentity(t)
	withContact(t, s, "Alice")
	m := newModel(t, s)
	clipboard.text = ""
	m = typeText(m, "d")
	if m.mode != modePaste {
		t.Fatalf("mode = %v, want paste", m.mode)
	}

	advancing := m.now
	frozen := time.Unix(1_000_000, 0)
	m.now = func() time.Time { return frozen }
	sealed := ageUnsigned(t, id, "pw")
	for _, r := range "Mallory:\n" + sealed + "\nvery nice, enjoy\nxoxo, yours\nq" {
		if r == '\n' {
			m = press(m, tea.KeyEnter)
			continue
		}
		m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.mode != modeOpened || m.opened == nil || m.opened.Text != "pw" {
		t.Fatalf("message not opened: mode=%v status=%q", m.mode, m.status)
	}
	book, _ := s.LoadContacts()
	if len(book.Contacts) != 1 || book.Contacts[0].Verified {
		t.Fatalf("pasted text changed contacts: %+v", book.Contacts)
	}

	// Keys typed at human speed work again.
	m.now = advancing
	m = press(m, tea.KeyEsc)
	if m.mode != modeBrowse {
		t.Fatalf("esc after paste: mode = %v", m.mode)
	}
}

func TestConfirmTreatsOtherKeysAsNo(t *testing.T) {
	useFakeClipboard(t)
	s, _ := newStoreWithIdentity(t)
	withContact(t, s, "Alice")
	m := newModel(t, s)
	m = typeText(m, "v")
	if m.confirm != confirmVerify {
		t.Fatal("no confirmation asked")
	}
	m = typeText(m, "ey")
	if m.confirm != confirmNone || m.book.Contacts[0].Verified {
		t.Fatal("a key other than yes did not cancel the confirmation")
	}
}

func TestPassphraseChangeNeedsCurrentPassphrase(t *testing.T) {
	useFakeClipboard(t)
	s := &store.Store{Dir: t.TempDir()}
	id, _ := keys.Generate()
	if err := s.SaveIdentity(id, "old"); err != nil {
		t.Fatal(err)
	}
	m := newModel(t, s)
	m = send(m, identityMsg{kind: readyUnlocked, id: id, locked: true})
	m = typeText(m, "kp")
	if m.mode != modePassphrase || len(m.passFields()) != 3 {
		t.Fatalf("mode=%v fields=%d", m.mode, len(m.passFields()))
	}
	checkView(t, m, "19-passphrase-locked")

	// Removing the passphrase asks first.
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if m.confirm != confirmRemovePassphrase || m.busy != "" {
		t.Fatal("empty passphrase saved without confirmation")
	}
	checkView(t, m, "20-confirm-remove-passphrase")
	m = typeText(m, "n")
	if m.confirm != confirmNone || m.busy != "" {
		t.Fatal("declining did not cancel")
	}

	// A wrong current passphrase is rejected.
	m = press(m, tea.KeyTab) // wraps to the first field
	m = typeText(m, "wrong")
	m = press(m, tea.KeyEnter)
	m = typeText(m, "new")
	m = press(m, tea.KeyEnter)
	m = typeText(m, "new")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = runIdentityJob(t, nm.(Model), cmd)
	if m.formErr == "" || m.focus != 0 {
		t.Fatalf("wrong current passphrase accepted: formErr=%q focus=%d", m.formErr, m.focus)
	}
	if _, err := s.LoadIdentity("old"); err != nil {
		t.Fatalf("identity changed despite wrong passphrase: %v", err)
	}

	m = typeText(m, "old")
	m = press(m, tea.KeyEnter, tea.KeyEnter)
	nm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = runIdentityJob(t, nm.(Model), cmd)
	if m.mode != modeMyKey {
		t.Fatalf("change failed: %q", m.formErr)
	}
	if _, err := s.LoadIdentity("new"); err != nil {
		t.Fatalf("new passphrase does not unlock: %v", err)
	}
}

func TestClipboardClearIsRetried(t *testing.T) {
	clipboard := useFakeClipboard(t)
	s, id := newStoreWithIdentity(t)
	m := newModel(t, s)
	clipboard.text = ageUnsigned(t, id, "hunter2")
	m = typeText(m, "dc")
	if m.clipSecret != "hunter2" {
		t.Fatal("secret not copied")
	}

	working := clipClearIf
	clipClearIf = func(string) (bool, error) { return false, errors.New("clipboard busy") }
	m = send(m, clipboardExpiredMsg{id: m.clipID})
	if m.clipSecret == "" || m.statusKind != statusWarn {
		t.Fatal("failed clear was reported as success and forgotten")
	}
	clipClearIf = working
	m = send(m, clipboardExpiredMsg{id: m.clipID})
	if m.clipSecret != "" || clipboard.text != "" {
		t.Fatal("retry did not clear the clipboard")
	}
}

func TestSurrogateHalvesAreJoined(t *testing.T) {
	useFakeClipboard(t)
	s, _ := newStoreWithIdentity(t)
	withContact(t, s, "Alice")
	m := newModel(t, s)
	m = typeText(m, "e")
	for _, r := range []rune{'a', 0xD83D, 0xDD11, 'b'} {
		m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.compose.Value(); got != "a🔑b" {
		t.Fatalf("compose = %q", got)
	}
}

func TestOwnSignatureAndLeftoverPlaintext(t *testing.T) {
	clipboard := useFakeClipboard(t)
	s, id := newStoreWithIdentity(t)
	m := newModel(t, s)
	sealed, err := message.Seal("note to self", id, []keys.PublicKey{id.Public()}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	clipboard.text = sealed
	m = typeText(m, "d")
	if m.unknownSender() || !strings.Contains(checkView(t, m, "21-opened-self"), T("opened.signed_self")) {
		t.Fatal("own signature not recognised")
	}
	m = press(m, tea.KeyEsc)
	if m.opened != nil || strings.Contains(m.content.View(), "note to self") {
		t.Fatal("plain text kept after leaving the message")
	}
}
