// Package tui is hush's interactive terminal interface.
package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf16"

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
	confirmRemovePassphrase
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
	clipboardRetryDelay = 5 * time.Second
	// pasteGap separates typing from pasting. Windows consoles deliver a
	// paste as separate key events without bracketed-paste markers, a few
	// microseconds apart; even key repeat is slower than this.
	pasteGap  = 25 * time.Millisecond
	minWidth  = 64
	minHeight = 18
)

type readyKind int

const (
	readyCreated readyKind = iota
	readyUnlocked
	readyUnlockedDevice
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
	// deviceMsg reports the result of remembering or forgetting this device.
	deviceMsg struct{ err error }
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
	// remembered means the system keyring can unlock the identity here.
	remembered bool

	screen        screen
	mode          mode
	width, height int

	cursor  int // index into book.Contacts; len(book.Contacts) is the "new contact" row
	confirm confirmKind

	compose textarea.Model
	paste   textarea.Model
	name    textinput.Model
	key     textinput.Model
	old     textinput.Model // current passphrase, required to change it
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

	now           func() time.Time
	lastKey       time.Time // when handling the previous key finished
	highSurrogate rune
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
		old:     newInput("", true),
		pass:    newInput("", true),
		pass2:   newInput("", true),
		content: viewport.New(0, 0),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(sAccent)),
		now:     time.Now,
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
	m.remembered = s.Remembered()
	if m.locked {
		m.screen = screenUnlock
		m.focusPass(0)
		if m.remembered {
			// Init asks the keyring; the passphrase field is the fallback.
			m.busy = T("busy.keyring")
		}
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
		if _, cerr := fm.clearClipboard(); cerr != nil {
			fmt.Fprintln(os.Stderr, "⚠ "+T("clip.not_cleared"))
		}
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

func (m Model) Init() tea.Cmd {
	if m.screen != screenUnlock || !m.remembered {
		return nil
	}
	s := m.store
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		id, err := s.LoadRemembered()
		return identityMsg{kind: readyUnlockedDevice, id: id, locked: true, err: err}
	})
}

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
		if msg.id != m.clipID || m.clipSecret == "" {
			return nil
		}
		cleared, err := m.clearClipboard()
		if err != nil {
			// Keep the secret so that the retry and the clear on exit still happen.
			return tea.Batch(m.setStatus(statusWarn, T("status.clipboard_clear_failed")), m.clipTimer(clipboardRetryDelay))
		}
		m.clipSecret = ""
		if cleared {
			return m.setStatus(statusInfo, T("status.clipboard_cleared"))
		}
		return nil
	case identityMsg:
		return m.identityReady(msg)
	case deviceMsg:
		m.busy = ""
		m.remembered = m.store.Remembered()
		m.refreshContent()
		switch {
		case msg.err != nil:
			return m.setStatus(statusErr, i18n.Err(msg.err))
		case m.remembered:
			return m.setStatus(statusOK, T("status.remembered"))
		}
		return m.setStatus(statusOK, T("status.forgotten"))
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return tea.Quit
		}
		msg, ok := m.joinSurrogates(msg)
		if !ok {
			return nil
		}
		since := m.now().Sub(m.lastKey)
		pasted := msg.Paste || !m.lastKey.IsZero() && since >= 0 && since < pasteGap
		defer func() { m.lastKey = m.now() }()
		if m.busy != "" || pasted && !m.takesPastedKey(msg) {
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
		fields := m.passFields()
		f := fields[min(m.focus, len(fields)-1)]
		*f, cmd = f.Update(msg)
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
	case "tab", "down":
		return m.focusPass(m.focus + 1)
	case "shift+tab", "up":
		return m.focusPass(m.focus - 1)
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
				err = s.CreateIdentity(id, passphrase)
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
		switch msg.kind {
		case readyUnlocked:
			m.pass.Reset()
		case readyUnlockedDevice:
			m.formErr = T("unlock.device_failed", i18n.Err(msg.err))
		case readyPassphraseChanged:
			m.old.Reset()
			return m.focusPass(0)
		}
		return nil
	}
	m.old.Reset()
	m.pass.Reset()
	m.pass2.Reset()
	m.id = msg.id
	m.locked = msg.locked
	m.remembered = m.store.Remembered()
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
	if msg.kind == readyUnlockedDevice {
		return m.setStatus(statusOK, T("status.unlocked_device"))
	}
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
		case "m":
			return m.toggleDevice()
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
			m.old.Reset()
			m.pass.Reset()
			m.pass2.Reset()
			m.setMode(modeMyKey)
			return nil
		case "tab", "down":
			return m.focusPass(m.focus + 1)
		case "shift+tab", "up":
			return m.focusPass(m.focus - 1)
		case "enter":
			if m.focus < len(m.passFields())-1 {
				return m.focusPass(m.focus + 1)
			}
			if m.pass.Value() != m.pass2.Value() {
				m.formErr = T("pass.mismatch")
				m.pass2.Reset()
				return nil
			}
			if m.pass.Value() == "" {
				m.confirm = confirmRemovePassphrase
				return nil
			}
			return m.savePassphrase()
		}
		m.formErr = ""

	case modeHelp:
		switch k {
		case "esc", "enter", "?":
			m.setMode(modeBrowse)
			return nil
		}
	}
	// The read-only panes keep the main shortcuts working, so that adding a
	// contact or decrypting does not depend on finding "esc" first.
	if m.readOnlyPane() && mainShortcuts[k] {
		m.setMode(modeBrowse)
		return m.updateBrowse(msg)
	}
	return m.forward(msg)
}

// mainShortcuts are the contact list keys that also work from read-only
// panes. Keys acting on the selected contact (v, r, x) are left out, as
// that contact is not what the pane shows.
var mainShortcuts = map[string]bool{"a": true, "d": true, "e": true, "k": true, "?": true, "q": true}

func (m *Model) readOnlyPane() bool {
	switch m.mode {
	case modeSealed, modeOpened, modeMyKey, modeHelp:
		return true
	}
	return false
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

// updateConfirm treats every key other than yes as no, so that stray input
// cannot wait in front of the prompt until a "y" comes along.
func (m *Model) updateConfirm(msg tea.KeyMsg) tea.Cmd {
	kind := m.confirm
	m.confirm = confirmNone
	switch msg.String() {
	case "y", "j", "Y", "J":
	default:
		return nil
	}
	if kind == confirmRemovePassphrase {
		return m.savePassphrase()
	}
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
}

// toggleDevice remembers or forgets this device in the system keyring. It
// runs in the background because the system may ask to unlock the keyring.
func (m *Model) toggleDevice() tea.Cmd {
	if !m.locked {
		return m.setStatus(statusInfo, T("status.remember_unprotected"))
	}
	s, id, forget := m.store, m.id, m.remembered
	return m.startBusy(T("busy.keyring"), func() tea.Msg {
		if forget {
			return deviceMsg{err: s.Forget()}
		}
		return deviceMsg{err: s.Remember(id)}
	})
}

func (m *Model) savePassphrase() tea.Cmd {
	old, passphrase, s, id, locked := m.old.Value(), m.pass.Value(), m.store, m.id, m.locked
	return m.startBusy(T("busy.saving"), func() tea.Msg {
		// Whoever sits at an unlocked session must not be able to replace or
		// remove the passphrase without knowing it.
		if locked {
			if _, err := s.LoadIdentity(old); err != nil {
				return identityMsg{kind: readyPassphraseChanged, err: err}
			}
		}
		err := s.SaveIdentity(id, passphrase)
		return identityMsg{kind: readyPassphraseChanged, id: id, locked: passphrase != "", err: err}
	})
}

func (m *Model) selected() *store.Contact {
	if m.cursor < len(m.book.Contacts) {
		return &m.book.Contacts[m.cursor]
	}
	return nil
}

func (m *Model) saveContacts(kind statusKind, text string) tea.Cmd {
	if err := m.store.SaveContacts(m.book); err != nil {
		if errors.Is(err, store.ErrContactsChanged) {
			// Another hush process wrote the file. Show its state; the user
			// repeats the change on top of it instead of overwriting it.
			if book, lerr := m.store.LoadContacts(); lerr == nil {
				m.book = book
				m.cursor = min(m.cursor, len(book.Contacts))
			}
		}
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
	return m.opened != nil && m.opened.Sender != nil &&
		*m.opened.Sender != m.id.Public() && m.book.ByKey(*m.opened.Sender) == nil
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
	return tea.Batch(
		m.setStatus(statusOK, T("status.copied_secret", int(clipboardClearDelay.Seconds()))),
		m.clipTimer(clipboardClearDelay),
	)
}

func (m *Model) clipTimer(d time.Duration) tea.Cmd {
	m.clipID++
	id := m.clipID
	return tea.Tick(d, func(time.Time) tea.Msg { return clipboardExpiredMsg{id: id} })
}

// clearClipboard removes the copied secret unless the user has copied
// something else since; cleared reports whether it did.
func (m Model) clearClipboard() (cleared bool, err error) {
	if m.clipSecret == "" {
		return false, nil
	}
	return clipClearIf(m.clipSecret)
}

func (m *Model) setStatus(kind statusKind, text string) tea.Cmd {
	m.statusID++
	m.status, m.statusKind = text, kind
	id := m.statusID
	return tea.Tick(statusDuration, func(time.Time) tea.Msg { return statusExpiredMsg{id: id} })
}

func (m *Model) setMode(md mode) {
	if md != modeOpened {
		m.opened = nil
	}
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

// passFields returns the passphrase inputs of the current form in focus order.
func (m *Model) passFields() []*textinput.Model {
	switch {
	case m.screen == screenUnlock:
		return []*textinput.Model{&m.pass}
	case m.screen == screenMain && m.locked:
		return []*textinput.Model{&m.old, &m.pass, &m.pass2}
	}
	return []*textinput.Model{&m.pass, &m.pass2}
}

func (m *Model) focusPass(i int) tea.Cmd {
	fields := m.passFields()
	m.focus = (i + len(fields)) % len(fields)
	for _, f := range fields {
		f.Blur()
	}
	return fields[m.focus].Focus()
}

// takesPastedKey reports whether a key that arrived as part of a paste may be
// handled. Only text typed into the focused input qualifies: anything else
// would let the words around a pasted message act as commands, e.g. "v" and
// "y" marking a contact as verified.
func (m *Model) takesPastedKey(msg tea.KeyMsg) bool {
	text := msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
	switch {
	case m.confirm != confirmNone:
		return false
	case m.screen != screenMain:
		return text
	case m.mode == modeCompose || m.mode == modePaste:
		return text || msg.Type == tea.KeyEnter
	case m.mode == modeAdd || m.mode == modeRename || m.mode == modePassphrase:
		return text
	}
	return false
}

// joinSurrogates reassembles characters outside the Basic Multilingual Plane,
// which Bubble Tea delivers on Windows as two UTF-16 halves. Each half on its
// own would be stored as U+FFFD, silently changing a pasted password.
func (m *Model) joinSurrogates(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	high := m.highSurrogate
	m.highSurrogate = 0
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return msg, true
	}
	switch r := msg.Runes[0]; {
	case r >= 0xD800 && r < 0xDC00:
		m.highSurrogate = r
		return msg, false
	case r >= 0xDC00 && r < 0xE000 && high != 0:
		msg.Runes = []rune{utf16.DecodeRune(high, r)}
	}
	return msg, true
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
