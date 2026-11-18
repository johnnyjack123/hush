package cli

import (
	"testing"

	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/store"
)

func bookWith(t *testing.T, names ...string) *store.Book {
	t.Helper()
	var b store.Book
	for _, n := range names {
		id, _ := keys.Generate()
		if _, err := b.Add(n, id.Public()); err != nil {
			t.Fatal(err)
		}
	}
	return &b
}

func picked(t *testing.T, b *store.Book, args ...string) []string {
	t.Helper()
	cs, err := pickContacts(b, args)
	if err != nil {
		return nil
	}
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

func TestPickContacts(t *testing.T) {
	b := bookWith(t, "Alice", "Bob", "Alice Müller", "Jo Ann", "Jo")
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"alice", "BOB"}, []string{"Alice", "Bob"}},
		{[]string{"Alice", "alice"}, []string{"Alice"}},
		{[]string{"Alice Müller"}, []string{"Alice Müller"}},
		// The old reading of several words as one name, when no word is a contact.
		{[]string{"Jo", "Ann"}, nil}, // "Jo" exists, "Ann" not: refused, not sent to "Jo Ann"
		{[]string{"Bob", "Carol"}, nil},
	}
	for _, c := range cases {
		got := picked(t, b, c.args...)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %q, want %q", c.args, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %q, want %q", c.args, got, c.want)
			}
		}
	}
	legacy := bookWith(t, "Alice Müller")
	if got := picked(t, legacy, "Alice", "Müller"); len(got) != 1 || got[0] != "Alice Müller" {
		t.Errorf("legacy reading: got %q", got)
	}
}
