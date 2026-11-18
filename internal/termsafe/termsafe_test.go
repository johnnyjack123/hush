package termsafe

import "testing"

func TestText(t *testing.T) {
	cases := map[string]string{
		"hunter2":                "hunter2",
		"äöü 🔑 مرحبا\nline 2\tx": "äöü 🔑 مرحبا\nline 2\tx",
		"\x1b[1A\x1b[2K✓ Signed": "␛[1A␛[2K✓ Signed",
		"a\rb\bc\x7f":            "a␍b␈c␡",
		"\x1b]52;c;aGk=\x07":     "␛]52;c;aGk=␇",
		"\u009b31m":              "\uFFFD31m",
		"x\u202Eevil":            "x\uFFFDevil",
		"bad \x9b byte":          "bad \uFFFD byte",
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLine(t *testing.T) {
	if got := Line("Alice\nBob\t!"); got != "Alice␊Bob␉!" {
		t.Errorf("Line = %q", got)
	}
	if !IsClean("Alice Müller") || IsClean("Al\x1bice") || IsClean("a\nb") {
		t.Error("IsClean misclassifies")
	}
}
