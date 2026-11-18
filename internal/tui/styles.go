package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Adaptive colours keep the UI readable on both light and dark terminals.
var (
	colAccent = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	colOK     = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	colErr    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	colDim    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#7C7F8A"}
	colFaint  = lipgloss.AdaptiveColor{Light: "#C4C4CC", Dark: "#3F3F46"}
	colBadge  = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1E1B2E"}
)

var (
	sAccent = lipgloss.NewStyle().Foreground(colAccent)
	sBold   = lipgloss.NewStyle().Bold(true)
	sOK     = lipgloss.NewStyle().Foreground(colOK)
	sWarn   = lipgloss.NewStyle().Foreground(colWarn)
	sErr    = lipgloss.NewStyle().Foreground(colErr)
	sDim    = lipgloss.NewStyle().Foreground(colDim)
	sBadge  = lipgloss.NewStyle().Bold(true).Foreground(colBadge).Background(colAccent).Padding(0, 1)
	sKey    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sQuote  = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(colAccent).
		PaddingLeft(1)
)

// box draws a rounded panel of exactly w×h cells with the title set into the top border.
func box(title, content string, w, h int, focused bool) string {
	border := lipgloss.NewStyle().Foreground(colFaint)
	titleStyle := sDim
	if focused {
		border = lipgloss.NewStyle().Foreground(colAccent)
		titleStyle = sAccent.Bold(true)
	}
	innerW, innerH := w-4, h-2

	t := ansi.Truncate(" "+title+" ", w-5, "…")
	fill := w - 3 - lipgloss.Width(t)
	var b strings.Builder
	b.WriteString(border.Render("╭─") + titleStyle.Render(t) + border.Render(strings.Repeat("─", max(fill, 0))+"╮"))

	lines := strings.Split(lipgloss.NewStyle().Width(innerW).Render(content), "\n")
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], innerW, "")
		}
		pad := innerW - lipgloss.Width(line)
		b.WriteString("\n" + border.Render("│") + " " + line + strings.Repeat(" ", max(pad, 0)) + " " + border.Render("│"))
	}
	b.WriteString("\n" + border.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}

// hardWrap breaks s into lines of at most n cells; for keys and ciphertext
// that have no spaces for word wrapping to use.
func hardWrap(s string, n int) string {
	if n <= 0 {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		for len(line) > n {
			out = append(out, line[:n])
			line = line[n:]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// hanging wraps text to width and aligns continuation lines under the text, not the prefix.
func hanging(prefix, text string, width int) string {
	pw := lipgloss.Width(prefix)
	lines := strings.Split(lipgloss.NewStyle().Width(max(width-pw, 1)).Render(text), "\n")
	indent := strings.Repeat(" ", pw)
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// fitArmor re-wraps an armored message narrower than age's 64 columns when the
// pane is too small. hush accepts any wrapping, so the shown text stays usable.
func fitArmor(armored string, width int) string {
	lines := strings.Split(armored, "\n")
	if width >= 64 || len(lines) < 3 {
		return armored
	}
	body := strings.Join(lines[1:len(lines)-1], "")
	return lines[0] + "\n" + hardWrap(body, width) + "\n" + lines[len(lines)-1]
}

// keyHelp renders "key description" pairs separated by dots.
func keyHelp(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, sKey.Render(pairs[i])+" "+sDim.Render(pairs[i+1]))
	}
	return strings.Join(parts, sDim.Render(" · "))
}

// field renders a label above an input, highlighted when focused.
func field(label, input string, focused bool) string {
	l := sDim.Render(label)
	if focused {
		l = sAccent.Bold(true).Render(label)
	}
	return l + "\n" + input
}

// row renders a label and value in two columns.
func row(label string, width int, value string) string {
	return sDim.Render(padRight(label, width)) + value
}

func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
