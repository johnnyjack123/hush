package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/store"
)

func TestMessageToSeveralContacts(t *testing.T) {
	clipboard := useFakeClipboard(t)
	aliceStore, alice := newStoreWithIdentity(t)
	bobStore, bob := newStoreWithIdentity(t)
	carolStore, carol := newStoreWithIdentity(t)
	for _, c := range []struct {
		s    *store.Store
		name string
		key  string
	}{
		{aliceStore, "Bob", bob.Public().String()},
		{aliceStore, "Carol", carol.Public().String()},
		{bobStore, "Alice", alice.Public().String()},
		{carolStore, "Alice", alice.Public().String()},
	} {
		clipboard.text = c.key
		m := newModel(t, c.s)
		m = typeText(m, "a"+c.name)
		m = press(m, tea.KeyEnter, tea.KeyEnter)
		if m.formErr != "" {
			t.Fatal(m.formErr)
		}
	}

	a := newModel(t, aliceStore)
	a = send(a, tea.KeyMsg{Type: tea.KeySpace}) // Bob
	a = press(a, tea.KeyDown)
	a = send(a, tea.KeyMsg{Type: tea.KeySpace}) // Carol
	if len(a.marked) != 2 {
		t.Fatalf("marked %d contacts, want 2", len(a.marked))
	}
	checkView(t, a, "26-marked")
	a = typeText(a, "e")
	if v := checkView(t, a, "27-compose-several"); !strings.Contains(v, "Bob, Carol") {
		t.Fatal("compose view does not name both recipients")
	}
	a = typeText(a, "team-pw")
	a = press(a, tea.KeyCtrlS)
	if a.mode != modeSealed || len(a.marked) != 0 {
		t.Fatalf("mode=%v marked=%d", a.mode, len(a.marked))
	}
	sealed := clipboard.text

	for _, r := range []*store.Store{bobStore, carolStore} {
		clipboard.text = sealed
		m := newModel(t, r)
		m = typeText(m, "d")
		if m.mode != modeOpened || m.opened.Text != "team-pw" || len(m.opened.Recipients) != 2 {
			t.Fatalf("not opened: %q", m.status)
		}
		if v := checkView(t, m, "28-opened-several"); !strings.Contains(v, T("opened.also_to", "")) {
			t.Error("opened view does not list the other recipient")
		}
	}

	// Without marks, e still encrypts for the selected contact only.
	a = press(a, tea.KeyEsc)
	a = typeText(a, "e")
	if len(a.composeTo) != 1 || a.composeTo[0].Name != "Carol" {
		t.Fatalf("composeTo = %+v", a.composeTo)
	}
	// Esc in the list clears marks.
	a = press(a, tea.KeyEsc)
	a = send(a, tea.KeyMsg{Type: tea.KeySpace})
	a = press(a, tea.KeyEsc)
	if len(a.marked) != 0 {
		t.Fatal("esc did not clear the marks")
	}
}

func TestMarksStayVisibleAndComposeFits(t *testing.T) {
	useFakeClipboard(t)
	s, _ := newStoreWithIdentity(t)
	book, _ := s.LoadContacts()
	for i := range 30 {
		other, _ := keys.Generate()
		book.Add(fmt.Sprintf("Person-with-a-long-name-%02d", i), other.Public())
	}
	s.SaveContacts(book)
	m := newModel(t, s)
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	for range 30 {
		m = send(m, tea.KeyMsg{Type: tea.KeySpace})
		m = press(m, tea.KeyDown)
	}
	m = press(m, tea.KeyUp)
	m = send(m, statusExpiredMsg{id: m.statusID})
	if v := checkView(t, m, "29-many-marked"); !strings.Contains(v, T("contacts.title_marked", 30)) {
		t.Error("contact list title does not show the marks")
	}
	m = typeText(m, "e")
	m = typeText(m, "SECRETTEXT")
	if v := checkView(t, m, "30-compose-many"); !strings.Contains(v, "SECRETTEXT") {
		t.Error("the typed text is pushed out of view")
	}
	if names := contactNames(m.composeTo); !strings.HasSuffix(names, T("names.more", 27)) {
		t.Errorf("recipient summary %q", names)
	}
}
