package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zalando/go-keyring"
)

func TestMainShortcutsWorkFromReadOnlyPanes(t *testing.T) {
	useFakeClipboard(t)
	s, _ := newStoreWithIdentity(t)
	m := newModel(t, s)
	for _, pane := range []string{"k", "?"} {
		m = typeText(m, pane)
		m = typeText(m, "a")
		if m.mode != modeAdd {
			t.Fatalf("a after %q: mode = %v, want add", pane, m.mode)
		}
		m = press(m, tea.KeyEsc)
	}
}

func TestHelpBarKeepsWayBack(t *testing.T) {
	useFakeClipboard(t)
	keyring.MockInit()
	s, id := lockedStore(t)
	m := newModel(t, s)
	m = send(m, identityMsg{kind: readyUnlocked, id: id, locked: true})
	m = typeText(m, "k")
	for _, w := range []int{64, 80, 100} {
		m = send(m, tea.WindowSizeMsg{Width: w, Height: 24})
		bar := ansi.Strip(m.viewHelp())
		if !strings.Contains(bar, "esc "+T("key.back")) || strings.HasSuffix(strings.TrimSpace(bar), "…") {
			t.Errorf("width %d: help bar %q lost the way back", w, bar)
		}
	}
}
