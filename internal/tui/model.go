// Package tui is hush's interactive terminal interface.
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/johnnyjack123/hush/internal/clip"
	"github.com/johnnyjack123/hush/internal/i18n"
	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/message"
	"github.com/johnnyjack123/hush/internal/store"
)

var T = i18n.T

// Clipboard access goes through these so tests never touch the real clipboard.
var (
	clipRead        = clip.Read
	clipWrite       = clip.Write
	clipWriteSecret = clip.WriteSecret
	clipClearIf     = clip.ClearIf
)

type screen int

const (
	screenSetup screen = iota
	screenUnlock
	screenMain
)

type mode int

const (
	modeBrowse mode = iota
	modeCompose
	modeSealed
	modePaste
	modeOpened
	modeMyKey
	modeAdd
	modeRename
	modePassphrase
	modeHelp
)

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmDelete
	confirmVerify
)

type statusKind int

const (
	statusInfo statusKind = iota
	statusOK
	statusWarn
	statusErr
)

const (
	statusDuration      = 6 * time.Second
	clipboardClearDelay = 30 * time.Second
	minWidth            = 64
	minHeight           = 18
)

type readyKind int

const (
	readyCreated readyKind = iota
	readyUnlocked
	readyPassphraseChanged
)

type (
	// identityMsg reports the result of a slow identity operation; scrypt
	// takes about a second, so these run outside the update loop.
	identityMsg struct {
		kind   readyKind
		id     *keys.Identity
		locked bool
		err    error
	}
	statusExpiredMsg    struct{ id int }
	clipboardExpiredMsg struct{ id int }
)

// Model is the Bubble Tea model for the whole application.
//
// Update has a value receiver as Bubble Tea expects; all helpers take a
// pointer and return only a command, which avoids the unspecified evaluation
// order of "return m, m.mutate()".
type Model struct {
	store  *store.Store
	id     *keys.Identity
	book   *store.Book
	locked bool

	screen        screen
	mode          mode
	width, height int

	cursor  int // index into book.Contacts; len(book.Contacts) is the "new contact" row
	confirm confirmKind

	compose textarea.Model
	paste   textarea.Model
	name    textinput.Model
	key     textinput.Model
	pass    textinput.Model
	pass2   textinput.Model
	focus   int
	formErr string

	sealed   string
	sealedTo string
	opened   *message.Opened
	content  viewport.Model

	spinner spinner.Model
	busy    string

	status     string
	statusKind statusKind
	statusID   int

	// clipSecret is decrypted text hush put on the clipboard and must clear again.
	clipSecret string
	clipID     int
}

// New builds the model, deciding between first-run setup, unlock and the main screen.
func New(s *store.Store) (Model, error) {
	book, err := s.LoadContacts()
	if err != nil {
		return Model{}, err
	}
	m := Model{
		store:   s,
		book:    book,
		compose: newTextarea(T("compose.placeholder")),
		paste:   newTextarea(T("paste.placeholder")),
		name:    newInput(T("add.name_placeholder"), false),
		key:     newInput("hush1…", false),
		pass:    newInput("", true),
		pass2:   newInput("", true),
		content: viewport.New(0, 0),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(sAccent)),
	}
	m.name.CharLimit = 40

	if !s.HasIdentity() {
		m.screen = screenSetup
		m.focusPass(0)
		return m, nil
	}
	if m.locked, err = s.IdentityLocked(); err != nil {
		return m, err
	}
	if m.locked {
		m.screen = screenUnlock
		m.focusPass(0)
		return m, nil
	}
	if m.id, err = s.LoadIdentity(""); err != nil {
		return m, err
	}
	m.screen = screenMain
	return m, nil
}

// Run starts the interface and blocks until the user quits.
func Run(s *store.Store) error {
	m, err := New(s)
	if err != nil {
		return err
	}
	// The alternate screen keeps decrypted text out of the terminal's scrollback.
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if fm, ok := final.(Model); ok {
		fm.clearClipboard()
	}
	return err
}

func newTextarea(placeholder string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.Prompt = ""
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = sDim
	ta.BlurredStyle.Placeholder = sDim
	ta.Cursor.SetMode(cursor.CursorStatic)
	return ta
}

func newInput(placeholder string, secret bool) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = "› "
	ti.PromptStyle = sAccent
	ti.PlaceholderStyle = sDim
	ti.CharLimit = 0
	ti.Cursor.SetMode(cursor.CursorStatic)
	if secret {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return ti
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return nil
	case spinner.TickMsg:
		if m.busy == "" {
			return nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return cmd
	case statusExpiredMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
		return nil
	case clipboardExpiredMsg:
		if msg.id == m.clipID && m.clipSecret != "" {
			m.clearClipboard()
			m.clipSecret = ""
			return m.setStatus(statusInfo, T("status.clipboard_cleared"))
		}
		return nil
	case identityMsg:
		return m.identityReady(msg)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return tea.Quit
		}
		if m.busy != "" {
			return nil
		}
		switch m.screen {
		case screenSetup:
			return m.updateSetup(msg)
		case screenUnlock:
			return m.updateUnlock(msg)
		}
		return m.updateMain(msg)
	}
	return m.forward(msg)
}

// forward hands a message to whichever component currently has focus.
func (m *Model) forward(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case m.screen != screenMain || m.mode == modePassphrase:
		if m.focus == 0 {
			m.pass, cmd = m.pass.Update(msg)
		} else {
			m.pass2, cmd = m.pass2.Update(msg)
		}
	case m.mode == modeCompose:
		m.compose, cmd = m.compose.Update(msg)
	case m.mode == modePaste:
		m.paste, cmd = m.paste.Update(msg)
		if message.Contains(m.paste.Value()) {
			return tea.Batch(cmd, m.open(m.paste.Value()))
		}
	case m.mode == modeAdd:
		if m.focus == 0 {
			m.name, cmd = m.name.Update(msg)
		} else {
			m.key, cmd = m.key.Update(msg)
		}
	case m.mode == modeRename:
		m.name, cmd = m.name.Update(msg)
	case m.mode == modeSealed, m.mode == modeOpened, m.mode == modeMyKey, m.mode == modeHelp:
		m.content, cmd = m.content.Update(msg)
	}
	return cmd
}

func (m *Model) updateSetup(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "down", "shift+tab", "up":
		return m.focusPass(1 - m.focus)
	case "enter":
		if m.focus == 0 {
			return m.focusPass(1)
		}
		if m.pass.Value() != m.pass2.Value() {
			m.formErr = T("pass.mismatch")
			m.pass2.Reset()
			return nil
		}
		passphrase, s := m.pass.Value(), m.store
		return m.startBusy(T("busy.creating"), func() tea.Msg {
			id, err := keys.Generate()
			if err == nil {
				err = s.SaveIdentity(id, passphrase)
			}
			return identityMsg{kind: readyCreated, id: id, locked: passphrase != "", err: err}
		})
	}
	m.formErr = ""
	return m.forward(msg)
}

func (m *Model) updateUnlock(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "enter" {
		passphrase, s := m.pass.Value(), m.store
		if passphrase == "" {
			return nil
		}
		return m.startBusy(T("busy.unlocking"), func() tea.Msg {
			id, err := s.LoadIdentity(passphrase)
			return identityMsg{kind: readyUnlocked, id: id, locked: true, err: err}
		})
	}
	m.formErr = ""
	return m.forward(msg)
}

func (m *Model) startBusy(text string, work func() tea.Msg) tea.Cmd {
	m.busy = text
	m.formErr = ""
	return tea.Batch(m.spinner.Tick, work)
}

func (m *Model) identityReady(msg identityMsg) tea.Cmd {
	m.busy = ""
	if msg.err != nil {
		m.formErr = i18n.Err(msg.err)
		if msg.kind == readyUnlocked {
			m.pass.Reset()
		}
		return nil
	}
	m.pass.Reset()
	m.pass2.Reset()
	m.id = msg.id
	m.locked = msg.locked
	m.screen = screenMain

	switch msg.kind {
	case readyCreated:
		m.setMode(modeMyKey)
		return m.setStatus(statusOK, T("status.keys_created"))
	case readyPassphraseChanged:
		m.setMode(modeMyKey)
		if m.locked {
			return m.setStatus(statusOK, T("status.passphrase_set"))
		}
		return m.setStatus(statusWarn, T("status.passphrase_removed"))
	}
	m.setMode(modeBrowse)
	return nil
}

func (m *Model) updateMain(msg tea.KeyMsg) tea.Cmd {
	if m.confirm != confirmNone {
		return m.updateConfirm(msg)
	}
	k := msg.String()
	switch m.mode {
	case modeBrowse:
		return m.updateBrowse(msg)

	case modeCompose:
		switch k {
		case "esc":
			m.compose.Reset()
			m.setMode(modeBrowse)
			return nil
		case "ctrl+s":
			return m.seal()
		}

	case modeSealed:
		switch k {
		case "esc", "enter":
			m.sealed = ""
			m.setMode(modeBrowse)
			return nil
		case "c":
			return m.copyPublic(m.sealed, T("status.copied_message"))
		}

	case modePaste:
		switch k {
		case "esc":
			m.paste.Reset()
			m.setMode(modeBrowse)
			return nil
		case "ctrl+s":
			return m.open(m.paste.Value())
		}

	case modeOpened:
		switch k {
		case "esc", "enter":
			m.opened = nil
			m.setMode(modeBrowse)
			return nil
		case "c":
			return m.copySecret(m.opened.Text)
		case "a":
			if m.unknownSender() {
				return m.startAdd(m.opened.Sender.String())
			}
		}

	case modeMyKey:
		switch k {
		case "esc", "enter":
			m.setMode(modeBrowse)
			return nil
		case "c":
			return m.copyPublic(m.id.Public().String(), T("status.copied_key"))
		case "p":
			m.setMode(modePassphrase)
			return m.focusPass(0)
		}

	case modeAdd:
		switch k {
		case "esc":
			m.setMode(modeBrowse)
			return nil
		case "tab", "down", "shift+tab", "up":
			return m.focusAdd(1 - m.focus)
		case "enter":
			if m.focus == 0 {
				return m.focusAdd(1)
			}
			return m.submitAdd()
		}
		m.formErr = ""

	case modeRename:
		switch k {
		case "esc":
			m.setMode(modeBrowse)
			return nil
		case "enter":
			return m.submitRename()
		}
		m.formErr = ""

	case modePassphrase:
		switch k {
		case "esc":
			m.pass.Reset()
			m.pass2.Reset()
			m.setMode(modeMyKey)
			return nil
		case "tab", "down", "shift+tab", "up":
			return m.focusPass(1 - m.focus)
		case "enter":
			if m.focus == 0 {
				return m.focusPass(1)
			}
			if m.pass.Value() != m.pass2.Value() {
				m.formErr = T("pass.mismatch")
				m.pass2.Reset()
				return nil
			}
			passphrase, s, id := m.pass.Value(), m.store, m.id
			return m.startBusy(T("busy.saving"), func() tea.Msg {
				err := s.SaveIdentity(id, passphrase)
				return identityMsg{kind: readyPassphraseChanged, id: id, locked: passphrase != "", err: err}
			})
		}
		m.formErr = ""

	case modeHelp:
		m.setMode(modeBrowse)
		return nil
	}
	return m.forward(msg)
}

func (m *Model) updateBrowse(msg tea.KeyMsg) tea.Cmd {
	c := m.selected()
	switch msg.String() {
	case "q":
		return tea.Quit
	case "up":
		m.cursor = max(m.cursor-1, 0)
	case "down":
		m.cursor = min(m.cursor+1, len(m.book.Contacts))
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = len(m.book.Contacts)
	case "enter", "e":
		if c == nil {
			if msg.String() == "enter" {
				return m.startAdd("")
			}
			return m.setStatus(statusWarn, T("status.select_contact"))
		}
		m.setMode(modeCompose)
		m.compose.Reset()
		return m.compose.Focus()
	case "d":
		return m.startOpen()
	case "a":
		return m.startAdd("")
	case "k":
		m.setMode(modeMyKey)
	case "?":
		m.setMode(modeHelp)
	case "v":
		if c == nil {
			return nil
		}
		if c.Verified {
			c.Verified = false
			return m.saveContacts(statusWarn, T("status.unverified", c.Name))
		}
		m.confirm = confirmVerify
	case "r":
		if c == nil {
			return nil
		}
		m.setMode(modeRename)
		m.name.SetValue(c.Name)
		m.name.CursorEnd()
		return m.name.Focus()
	case "x", "delete":
		if c != nil {
			m.confirm = confirmDelete
		}
	}
	return nil
}

func (m *Model) updateConfirm(msg tea.KeyMsg) tea.Cmd {
	kind := m.confirm
	switch msg.String() {
	case "y", "j", "Y", "J":
		m.confirm = confirmNone
		c := m.selected()
		if c == nil {
			return nil
		}
		name := c.Name
		if kind == confirmDelete {
			m.book.Remove(m.cursor)
			m.cursor = min(m.cursor, len(m.book.Contacts))
			return m.saveContacts(statusOK, T("status.deleted", name))
		}
		c.Verified = true
		return m.saveContacts(statusOK, T("status.verified", name))
	case "n", "N", "esc":
		m.confirm = confirmNone
	}
	return nil
}

func (m *Model) selected() *store.Contact {
	if m.cursor < len(m.book.Contacts) {
		return &m.book.Contacts[m.cursor]
	}
	return nil
}

func (m *Model) saveContacts(kind statusKind, text string) tea.Cmd {
	if err := m.store.SaveContacts(m.book); err != nil {
		return m.setStatus(statusErr, i18n.Err(err))
	}
	return m.setStatus(kind, text)
}

func (m *Model) seal() tea.Cmd {
	c := m.selected()
	// A trailing newline from pressing enter would otherwise end up in a pasted password.
	text := strings.TrimRight(m.compose.Value(), "\r\n")
	if c == nil || strings.TrimSpace(text) == "" {
		return m.setStatus(statusWarn, T("status.empty_message"))
	}
	sealed, err := message.Seal(text, m.id, c.PublicKey(), time.Now())
	if err != nil {
		return m.setStatus(statusErr, i18n.Err(err))
	}
	m.compose.Reset()
	m.sealed, m.sealedTo = sealed, c.Name
	m.setMode(modeSealed)
	if err := clipWrite(sealed); err != nil {
		return m.setStatus(statusWarn, T("status.clipboard_failed", err))
	}
	return m.setStatus(statusOK, T("status.sealed", c.Name))
}

// startOpen decrypts straight from the clipboard when it holds a message,
// and otherwise asks the user to paste one.
func (m *Model) startOpen() tea.Cmd {
	var warn tea.Cmd
	if s, err := clipRead(); err == nil && message.Contains(s) {
		o, err := message.Open(s, m.id)
		if err == nil {
			return m.showOpened(o)
		}
		warn = m.setStatus(statusWarn, T("status.clipboard_not_openable", i18n.Err(err)))
	}
	m.setMode(modePaste)
	m.paste.Reset()
	return tea.Batch(m.paste.Focus(), warn)
}

func (m *Model) open(text string) tea.Cmd {
	o, err := message.Open(text, m.id)
	if err != nil {
		return m.setStatus(statusErr, i18n.Err(err))
	}
	return m.showOpened(o)
}

func (m *Model) showOpened(o *message.Opened) tea.Cmd {
	m.paste.Reset()
	m.opened = o
	m.setMode(modeOpened)
	return m.setStatus(statusOK, T("status.decrypted"))
}

func (m *Model) unknownSender() bool {
	return m.opened != nil && m.opened.Sender != nil && m.book.ByKey(*m.opened.Sender) == nil
}

func (m *Model) startAdd(prefill string) tea.Cmd {
	if prefill == "" {
		if s, err := clipRead(); err == nil {
			prefill = keys.FindPublicKey(s)
		}
	}
	m.setMode(modeAdd)
	m.name.Reset()
	m.key.SetValue(prefill)
	m.key.CursorEnd()
	return m.focusAdd(0)
}

func (m *Model) submitAdd() tea.Cmd {
	pk, err := keys.ParsePublicKey(keys.FindPublicKey(m.key.Value()))
	switch {
	case err != nil:
		m.formErr = i18n.Err(err)
		return nil
	case pk == m.id.Public():
		m.formErr = T("err.own_key")
		return nil
	}
	i, err := m.book.Add(m.name.Value(), pk)
	if err != nil {
		m.formErr = i18n.Err(err)
		return nil
	}
	m.cursor = i
	m.setMode(modeBrowse)
	return m.saveContacts(statusOK, T("status.added", m.book.Contacts[i].Name))
}

func (m *Model) submitRename() tea.Cmd {
	i, err := m.book.Rename(m.cursor, m.name.Value())
	if err != nil {
		m.formErr = i18n.Err(err)
		return nil
	}
	m.cursor = i
	m.setMode(modeBrowse)
	return m.saveContacts(statusOK, T("status.renamed", m.book.Contacts[i].Name))
}

// copyPublic copies non-secret text such as a ciphertext or public key.
func (m *Model) copyPublic(s, okText string) tea.Cmd {
	if err := clipWrite(s); err != nil {
		return m.setStatus(statusErr, T("status.clipboard_failed", err))
	}
	return m.setStatus(statusOK, okText)
}

func (m *Model) copySecret(s string) tea.Cmd {
	if err := clipWriteSecret(s); err != nil {
		return m.setStatus(statusErr, T("status.clipboard_failed", err))
	}
	m.clipSecret = s
	m.clipID++
	id := m.clipID
	return tea.Batch(
		m.setStatus(statusOK, T("status.copied_secret", int(clipboardClearDelay.Seconds()))),
		tea.Tick(clipboardClearDelay, func(time.Time) tea.Msg { return clipboardExpiredMsg{id: id} }),
	)
}

func (m Model) clearClipboard() {
	if m.clipSecret != "" {
		_ = clipClearIf(m.clipSecret)
	}
}

func (m *Model) setStatus(kind statusKind, text string) tea.Cmd {
	m.statusID++
	m.status, m.statusKind = text, kind
	id := m.statusID
	return tea.Tick(statusDuration, func(time.Time) tea.Msg { return statusExpiredMsg{id: id} })
}

func (m *Model) setMode(md mode) {
	m.mode = md
	m.formErr = ""
	m.confirm = confirmNone
	m.compose.Blur()
	m.paste.Blur()
	m.name.Blur()
	m.key.Blur()
	m.pass.Blur()
	m.pass2.Blur()
	m.refreshContent()
	m.content.GotoTop()
}

func (m *Model) focusPass(i int) tea.Cmd {
	m.focus = i
	if i == 0 {
		m.pass2.Blur()
		return m.pass.Focus()
	}
	m.pass.Blur()
	return m.pass2.Focus()
}

func (m *Model) focusAdd(i int) tea.Cmd {
	m.focus = i
	if i == 0 {
		m.key.Blur()
		return m.name.Focus()
	}
	m.name.Blur()
	return m.key.Focus()
}

// dims returns the widths of the two panes and the height of the body.
func (m Model) dims() (left, right, body int) {
	left = 34
	if m.width < 90 {
		left = 28
	}
	return left, m.width - left, m.height - 3
}

func (m *Model) layout() {
	_, rw, bh := m.dims()
	innerW, innerH := rw-4, bh-2
	m.compose.SetWidth(innerW)
	m.compose.SetHeight(max(innerH-2, 3))
	m.paste.SetWidth(innerW)
	m.paste.SetHeight(max(innerH-2, 3))
	for _, in := range []*textinput.Model{&m.name, &m.key} {
		in.Width = max(innerW-3, 10)
	}
	for _, in := range []*textinput.Model{&m.pass, &m.pass2} {
		in.Width = max(m.setupWidth()-7, 10)
	}
	m.content.Width = innerW
	m.content.Height = max(innerH, 1)
	m.refreshContent()
}

func (m Model) setupWidth() int {
	return min(72, m.width-4)
}
