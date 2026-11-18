package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/johnnyjack123/hush/internal/i18n"
	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/store"
	"github.com/johnnyjack123/hush/internal/termsafe"
)

const labelWidth = 15

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			sWarn.Render(T("ui.too_small", minWidth, minHeight)))
	}
	switch m.screen {
	case screenSetup:
		return m.viewSetup()
	case screenUnlock:
		return m.viewUnlock()
	}

	lw, rw, bh := m.dims()
	left := box(T("contacts.title"), m.viewContacts(lw-4, bh-2), lw, bh, m.mode == modeBrowse)
	title, body := m.viewRight(rw-4, bh-2)
	right := box(title, body, rw, bh, m.mode != modeBrowse)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewHeader(),
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		m.viewStatus(),
		m.viewHelp(),
	)
}

func (m Model) viewHeader() string {
	// The own fingerprint is deliberately not shown here: a short form in
	// plain sight invites comparing only part of it.
	left := sBadge.Render("hush") + "  " + sDim.Render(T("app.tagline"))
	right := ""
	if !m.locked {
		right = sWarn.Render("⚠ " + T("header.unprotected"))
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		left = sBadge.Render("hush")
		gap = m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	}
	return ansi.Truncate(left+strings.Repeat(" ", max(gap, 1))+right, m.width, "")
}

func (m Model) viewContacts(w, h int) string {
	var rows []string
	for i, c := range m.book.Contacts {
		fp := c.PublicKey().Fingerprint().Short()
		mark := " "
		if c.Verified {
			mark = sOK.Render("✓")
		}
		nameW := w - 2 - 2 - len(fp) - 1
		name := padRight(ansi.Truncate(c.Name, nameW, "…"), nameW)
		if i == m.cursor {
			rows = append(rows, sAccent.Bold(true).Render("▸ "+name)+" "+mark+" "+sAccent.Render(fp))
		} else {
			rows = append(rows, "  "+name+" "+mark+" "+sDim.Render(fp))
		}
	}
	if len(m.book.Contacts) == 0 {
		rows = append(rows, sDim.Render(T("contacts.empty")), "")
	} else {
		rows = append(rows, "")
	}
	newRow := "+ " + T("contacts.new")
	if m.cursor == len(m.book.Contacts) {
		rows = append(rows, sAccent.Bold(true).Render("▸ "+newRow))
	} else {
		rows = append(rows, sDim.Render("  "+newRow))
	}

	// Keep the cursor row visible when the list is taller than the pane.
	cursorRow := m.cursor
	if m.cursor == len(m.book.Contacts) {
		cursorRow = len(rows) - 1
	}
	if offset := cursorRow - h + 1; offset > 0 {
		rows = rows[offset:]
	}
	return strings.Join(rows, "\n")
}

func (m Model) viewRight(w, h int) (title, body string) {
	switch m.mode {
	case modeCompose:
		c := m.selected()
		return T("compose.title", c.Name), sDim.Render(T("compose.hint", c.Name)) + "\n\n" + m.compose.View()
	case modeSealed:
		return T("sealed.title", m.sealedTo), m.content.View()
	case modePaste:
		return T("paste.title"), sDim.Render(T("paste.hint")) + "\n\n" + m.paste.View()
	case modeOpened:
		return T("opened.title"), m.content.View()
	case modeMyKey:
		return T("mykey.title"), m.content.View()
	case modeHelp:
		return T("help.title"), m.content.View()
	case modeAdd:
		return T("add.title"), m.viewAdd()
	case modeRename:
		return T("rename.title"), field(T("add.name"), m.name.View(), true) + m.viewFormErr()
	case modePassphrase:
		return T("pass.title"), m.viewPassphraseForm(T("pass.change_hint"))
	}
	if c := m.selected(); c != nil {
		return c.Name, m.viewContact(c, w)
	}
	return T("start.title"), viewStart(w)
}

func (m Model) viewContact(c *store.Contact, w int) string {
	pk := c.PublicKey()
	status := sWarn.Render("⚠ " + T("contact.unverified"))
	if c.Verified {
		status = sOK.Render("✓ " + T("contact.verified"))
	}
	lines := []string{
		row(T("contact.fingerprint"), labelWidth, sAccent.Bold(true).Render(pk.Fingerprint().String())),
		row(T("contact.status"), labelWidth, status),
		row(T("contact.added"), labelWidth, i18n.Date(c.Added)),
		"",
		sDim.Render(T("contact.public_key")),
		hardWrap(pk.String(), w),
		"",
	}
	if !c.Verified {
		lines = append(lines,
			sWarn.Render(T("contact.verify_hint", c.Name)),
			sAccent.Bold(true).Render(m.id.Public().Fingerprint().String()),
			"")
	}
	lines = append(lines, keyHelp(
		"e", T("key.encrypt"), "v", T("key.verify"), "r", T("key.rename"), "x", T("key.delete")))
	return strings.Join(lines, "\n")
}

func viewStart(w int) string {
	steps := []string{
		T("start.step1"), T("start.step2"), T("start.step3"), T("start.step4"), T("start.step5"),
	}
	var b strings.Builder
	b.WriteString(sBold.Render(T("start.heading")) + "\n\n")
	for i, s := range steps {
		b.WriteString(hanging(sAccent.Bold(true).Render(fmt.Sprintf("%d  ", i+1)), s, w) + "\n\n")
	}
	return b.String()
}

func (m Model) viewAdd() string {
	lines := []string{
		field(T("add.name"), m.name.View(), m.focus == 0),
		"",
		field(T("add.key"), m.key.View(), m.focus == 1),
	}
	if v := strings.TrimSpace(m.key.Value()); v != "" {
		if pk, err := keys.ParsePublicKey(keys.FindPublicKey(v)); err == nil {
			lines = append(lines, sOK.Render("✓ "+T("add.key_valid"))+"  "+
				sDim.Render(T("contact.fingerprint")+" ")+sAccent.Bold(true).Render(pk.Fingerprint().String()))
		} else {
			lines = append(lines, sErr.Render("✗ "+T("err.invalid_key")))
		}
	}
	lines = append(lines, "", sDim.Render(T("add.hint")))
	return strings.Join(lines, "\n") + m.viewFormErr()
}

func (m Model) viewPassphraseForm(hint string) string {
	labels := []string{T("pass.new"), T("pass.repeat")}
	inputs := []textinput.Model{m.pass, m.pass2}
	if m.screen == screenMain && m.locked {
		labels = []string{T("pass.current"), T("pass.new_change"), T("pass.repeat")}
		inputs = append([]textinput.Model{m.old}, inputs...)
	}
	lines := []string{sDim.Render(hint)}
	for i, in := range inputs {
		lines = append(lines, "", field(labels[i], in.View(), m.focus == i))
	}
	if m.busy != "" {
		lines = append(lines, "", m.spinner.View()+" "+m.busy)
	}
	return strings.Join(lines, "\n") + m.viewFormErr()
}

func (m Model) viewFormErr() string {
	if m.formErr == "" {
		return ""
	}
	return "\n\n" + sErr.Render("✗ "+m.formErr)
}

// refreshContent fills the scrollable viewport for the modes that use it.
func (m *Model) refreshContent() {
	w := m.content.Width
	var s string
	switch m.mode {
	case modeSealed:
		s = sOK.Render("✓ "+T("sealed.done")) + "\n" +
			sDim.Render(T("sealed.hint", m.sealedTo)) + "\n\n" + sAccent.Render(fitArmor(m.sealed, w))
	case modeOpened:
		s = m.openedContent(w)
	case modeMyKey:
		s = m.myKeyContent(w)
	case modeHelp:
		s = helpContent(w)
	default:
		// Drop whatever was shown before, a decrypted text in particular.
		m.content.SetContent("")
		return
	}
	m.content.SetContent(lipgloss.NewStyle().Width(max(w, 1)).Render(s))
}

func (m *Model) openedContent(w int) string {
	o := m.opened
	if o == nil {
		return ""
	}
	var lines []string
	switch {
	case o.Sender == nil:
		lines = append(lines, sWarn.Render("⚠ "+T("opened.unsigned")))
	case *o.Sender == m.id.Public():
		lines = append(lines, sOK.Render("✓ "+T("opened.signed_self")))
	case m.book.ByKey(*o.Sender) == nil:
		lines = append(lines,
			sWarn.Render("⚠ "+T("opened.unknown_sender")),
			row(T("contact.fingerprint"), labelWidth, sAccent.Render(o.Sender.Fingerprint().String())),
			keyHelp("a", T("key.save_sender")))
	default:
		c := m.book.ByKey(*o.Sender)
		if c.Verified {
			lines = append(lines, sOK.Render("✓ "+T("opened.signed_verified", c.Name)))
		} else {
			lines = append(lines,
				sOK.Render("✓ "+T("opened.signed", c.Name)),
				sWarn.Render("⚠ "+T("opened.not_verified")))
		}
	}
	if !o.SentAt.IsZero() {
		lines = append(lines, sDim.Render(T("opened.sent_at", i18n.Time(o.SentAt))))
	}
	// The text comes from the sender and must not be able to drive the terminal.
	text := termsafe.Text(o.Text)
	lines = append(lines, "", sQuote.Width(max(w-2, 1)).Render(text), "", keyHelp("c", T("key.copy_secret")))
	return strings.Join(lines, "\n")
}

func (m *Model) myKeyContent(w int) string {
	pk := m.id.Public()
	protection := sWarn.Render("⚠ " + T("mykey.unprotected"))
	if m.locked {
		protection = sOK.Render("✓ " + T("mykey.protected"))
	}
	return strings.Join([]string{
		T("mykey.intro"),
		"",
		sAccent.Bold(true).Render(hardWrap(pk.String(), w)),
		"",
		row(T("contact.fingerprint"), labelWidth, sAccent.Bold(true).Render(pk.Fingerprint().String())),
		row(T("mykey.passphrase"), labelWidth, protection),
		row(T("mykey.location"), labelWidth, sDim.Render(m.store.Dir)),
		"",
		sDim.Render(T("mykey.age_hint")),
		sDim.Render(hardWrap(pk.AgeRecipient(), w)),
		"",
		keyHelp("c", T("key.copy_key"), "p", T("key.passphrase")),
	}, "\n")
}

func helpContent(w int) string {
	keysList := [][2]string{
		{"↑ ↓", T("help.select")},
		{"enter / e", T("help.encrypt")},
		{"d", T("help.decrypt")},
		{"a", T("help.add")},
		{"v", T("help.verify")},
		{"r", T("help.rename")},
		{"x", T("help.delete")},
		{"k", T("help.mykey")},
		{"esc", T("help.back")},
		{"q", T("help.quit")},
	}
	var b strings.Builder
	b.WriteString(sBold.Render(T("help.keys")) + "\n")
	for _, k := range keysList {
		b.WriteString(hanging("  "+sKey.Render(padRight(k[0], 11)), k[1], w) + "\n")
	}
	b.WriteString("\n" + sBold.Render(T("help.security")) + "\n")
	for _, s := range []string{T("help.sec1"), T("help.sec2"), T("help.sec3"), T("help.sec4")} {
		b.WriteString(hanging(sAccent.Render("  • "), s, w) + "\n")
	}
	return b.String()
}

func (m Model) viewStatus() string {
	if m.confirm != confirmNone {
		var q string
		switch c := m.selected(); m.confirm {
		case confirmDelete:
			q = T("confirm.delete", c.Name)
		case confirmVerify:
			q = T("confirm.verify", c.Name)
		case confirmRemovePassphrase:
			q = T("confirm.remove_passphrase")
		}
		return ansi.Truncate(" "+sWarn.Bold(true).Render(q)+"  "+keyHelp(T("key.yes_key"), T("key.yes"), "n", T("key.no")), m.width, "…")
	}
	if m.status == "" {
		return ""
	}
	style := map[statusKind]lipgloss.Style{statusInfo: sAccent, statusOK: sOK, statusWarn: sWarn, statusErr: sErr}[m.statusKind]
	icon := map[statusKind]string{statusInfo: "●", statusOK: "✓", statusWarn: "⚠", statusErr: "✗"}[m.statusKind]
	return ansi.Truncate(" "+style.Render(icon+" "+m.status), m.width, "…")
}

func (m Model) viewHelp() string {
	var pairs []string
	switch m.screen {
	case screenSetup:
		pairs = []string{"tab", T("key.next_field"), "enter", T("key.create"), "ctrl+c", T("key.quit")}
	case screenUnlock:
		pairs = []string{"enter", T("key.unlock"), "ctrl+c", T("key.quit")}
	default:
		switch m.mode {
		case modeBrowse:
			pairs = []string{"e", T("key.encrypt"), "d", T("key.decrypt"),
				"a", T("key.add"), "k", T("key.mykey"), "?", T("key.help"), "q", T("key.quit")}
		case modeCompose:
			pairs = []string{"ctrl+s", T("key.encrypt"), "esc", T("key.cancel")}
		case modeSealed:
			pairs = []string{"c", T("key.copy_again"), "esc", T("key.back")}
		case modePaste:
			pairs = []string{"ctrl+s", T("key.decrypt"), "esc", T("key.cancel")}
		case modeOpened:
			pairs = []string{"c", T("key.copy_secret"), "↑↓", T("key.scroll"), "esc", T("key.back")}
		case modeMyKey:
			pairs = []string{"c", T("key.copy_key"), "p", T("key.passphrase"), "esc", T("key.back")}
		case modeAdd, modePassphrase:
			pairs = []string{"tab", T("key.next_field"), "enter", T("key.save"), "esc", T("key.cancel")}
		case modeRename:
			pairs = []string{"enter", T("key.save"), "esc", T("key.cancel")}
		case modeHelp:
			pairs = []string{"esc", T("key.back")}
		}
	}
	return ansi.Truncate(" "+keyHelp(pairs...), m.width, "…")
}

func (m Model) viewSetup() string {
	w := m.setupWidth()
	body := strings.Join([]string{
		T("setup.intro"),
		"",
		T("setup.keys"),
		"",
		m.viewPassphraseForm(T("setup.passphrase_hint")),
	}, "\n")
	return m.centered(box(T("setup.title"), body, w, lipgloss.Height(lipgloss.NewStyle().Width(w-4).Render(body))+2, true))
}

func (m Model) viewUnlock() string {
	w := min(60, m.width-4)
	lines := []string{T("unlock.intro"), "", field(T("unlock.passphrase"), m.pass.View(), true)}
	if m.busy != "" {
		lines = append(lines, "", m.spinner.View()+" "+m.busy)
	}
	body := strings.Join(lines, "\n") + m.viewFormErr()
	return m.centered(box(T("unlock.title"), body, w, lipgloss.Height(lipgloss.NewStyle().Width(w-4).Render(body))+2, true))
}

func (m Model) centered(panel string) string {
	logo := sBadge.Render("hush") + "  " + sDim.Render(T("app.tagline"))
	content := lipgloss.JoinVertical(lipgloss.Center, logo, "", panel, "", m.viewStatus(), m.viewHelp())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}
