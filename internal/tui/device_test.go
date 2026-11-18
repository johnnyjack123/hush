package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zalando/go-keyring"

	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/store"
)

// runBusyJob executes the background work of a startBusy or Init command and
// feeds its result back into the model.
func runBusyJob(t *testing.T, m Model, cmd tea.Cmd) Model {
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
		if msg := c(); msg != nil {
			if _, tick := msg.(spinner.TickMsg); !tick {
				return send(m, msg)
			}
		}
	}
	t.Fatal("no background job in batch")
	return m
}

// shows reports whether the start of text appears in the view; the rest may wrap.
func shows(view, text string) bool {
	words := strings.Fields(text)
	return strings.Contains(view, strings.Join(words[:min(3, len(words))], " "))
}

func lockedStore(t *testing.T) (*store.Store, *keys.Identity) {
	t.Helper()
	s := &store.Store{Dir: t.TempDir()}
	id, _ := keys.Generate()
	if err := s.CreateIdentity(id, "pw"); err != nil {
		t.Fatal(err)
	}
	return s, id
}

func TestRememberThisDevice(t *testing.T) {
	useFakeClipboard(t)
	keyring.MockInit()
	s, id := lockedStore(t)

	m := newModel(t, s)
	if m.screen != screenUnlock || m.busy != "" || m.Init() != nil {
		t.Fatal("a device that is not remembered must ask for the passphrase")
	}
	if !shows(checkView(t, m, "22-unlock-tip"), T("unlock.tip")) {
		t.Error("unlock screen does not mention remembering the device")
	}
	m = send(m, identityMsg{kind: readyUnlocked, id: id, locked: true})
	m = typeText(m, "k")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = runBusyJob(t, nm.(Model), cmd)
	if !m.remembered || !s.Remembered() {
		t.Fatalf("not remembered: %q", m.status)
	}
	if !shows(checkView(t, m, "23-mykey-remembered"), T("mykey.device_remembered")) {
		t.Error("my key view does not show the remembered device")
	}

	// The next start unlocks through the keyring.
	next := newModel(t, s)
	if next.screen != screenUnlock || next.busy == "" {
		t.Fatal("remembered device does not start unlocking")
	}
	checkView(t, next, "24-unlock-keyring")
	next = runBusyJob(t, next, next.Init())
	if next.screen != screenMain || next.id.Public() != id.Public() {
		t.Fatalf("not unlocked: formErr=%q", next.formErr)
	}

	// Forgetting needs the passphrase again on the next start.
	next = typeText(next, "k")
	nm, cmd = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	next = runBusyJob(t, nm.(Model), cmd)
	if next.remembered || s.Remembered() {
		t.Fatal("device not forgotten")
	}
	if again := newModel(t, s); again.busy != "" {
		t.Fatal("forgotten device still unlocks")
	}
}

func TestKeyringFailureFallsBackToPassphrase(t *testing.T) {
	useFakeClipboard(t)
	keyring.MockInit()
	s, id := lockedStore(t)
	if err := s.Remember(id); err != nil {
		t.Fatal(err)
	}
	keyring.MockInitWithError(errors.New("keyring locked"))
	m := newModel(t, s)
	m = runBusyJob(t, m, m.Init())
	if m.screen != screenUnlock || m.busy != "" || m.formErr == "" {
		t.Fatalf("screen=%v busy=%q formErr=%q", m.screen, m.busy, m.formErr)
	}
	checkView(t, m, "25-unlock-keyring-failed")
}

func TestRememberNeedsPassphrase(t *testing.T) {
	useFakeClipboard(t)
	keyring.MockInit()
	s, _ := newStoreWithIdentity(t)
	m := newModel(t, s)
	m = typeText(m, "km")
	if m.busy != "" || s.Remembered() || m.statusKind != statusInfo {
		t.Fatal("an identity without passphrase was remembered")
	}
}
