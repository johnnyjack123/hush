// Package cli implements hush's command line: the interactive UI by default,
// plus subcommands for scripting.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/johnnyjack123/hush/internal/clip"
	"github.com/johnnyjack123/hush/internal/i18n"
	"github.com/johnnyjack123/hush/internal/keys"
	"github.com/johnnyjack123/hush/internal/message"
	"github.com/johnnyjack123/hush/internal/store"
	"github.com/johnnyjack123/hush/internal/termsafe"
	"github.com/johnnyjack123/hush/internal/tui"
)

var T = i18n.T

// errUsage marks errors that already printed their own message.
var errUsage = errors.New("usage")

// Run executes the command line and returns the process exit code.
func Run(args []string, version string) int {
	var err error
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "":
		err = runTUI()
	case "encrypt", "e":
		err = cmdEncrypt(args)
	case "decrypt", "d":
		err = cmdDecrypt(args)
	case "key":
		err = cmdKey()
	case "contacts", "ls":
		err = cmdContacts()
	case "add":
		err = cmdAdd(args)
	case "init":
		err = cmdInit(args)
	case "version", "--version", "-v":
		fmt.Println("hush", version)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, T("cli.unknown_command", cmd))
		printUsage(os.Stderr)
		return 2
	}
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(os.Stderr, "hush: "+i18n.Err(err))
		}
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	dir := "?"
	if s, err := store.Open(); err == nil {
		dir = s.Dir
	}
	fmt.Fprintf(w, T("cli.usage"), dir)
}

func usageError(key string) error {
	fmt.Fprintln(os.Stderr, T(key))
	return errUsage
}

// out writes a line to stdout and returns any failure, such as a full disk,
// which would otherwise leave a script with a cut-off result and exit code 0.
func out(s string) error {
	_, err := fmt.Fprintln(os.Stdout, s)
	return err
}

func runTUI() error {
	s, err := store.Open()
	if err != nil {
		return err
	}
	return tui.Run(s)
}

func loadIdentity(s *store.Store) (*keys.Identity, error) {
	locked, err := s.IdentityLocked()
	if err != nil {
		return nil, err
	}
	passphrase := ""
	if locked {
		if passphrase, err = readSecret(T("cli.passphrase")); err != nil {
			return nil, err
		}
	}
	return s.LoadIdentity(passphrase)
}

func cmdEncrypt(args []string) error {
	name := strings.Join(args, " ")
	if name == "" {
		return usageError("cli.usage_encrypt")
	}
	s, err := store.Open()
	if err != nil {
		return err
	}
	book, err := s.LoadContacts()
	if err != nil {
		return err
	}
	i := book.Index(name)
	if i < 0 {
		return errors.New(T("cli.unknown_contact", name))
	}
	id, err := loadIdentity(s)
	if err != nil {
		return err
	}
	var text string
	if isTerminal(os.Stdin) {
		text, err = readSecret(T("cli.enter_text"))
	} else {
		// Reading a little past the limit makes sure a cut never lands on
		// trailing newlines, where it would go unnoticed.
		var b []byte
		b, err = io.ReadAll(io.LimitReader(os.Stdin, message.MaxTextSize+3))
		if len(b) > message.MaxTextSize+2 {
			return message.ErrTooLong
		}
		text = strings.TrimRight(string(b), "\r\n")
	}
	if err != nil {
		return err
	}
	if text == "" {
		return errors.New(T("cli.empty_input"))
	}
	sealed, err := message.Seal(text, id, book.Contacts[i].PublicKey(), time.Now())
	if err != nil {
		return err
	}
	return out(sealed)
}

func cmdDecrypt(args []string) error {
	if len(args) > 0 {
		return usageError("cli.usage_decrypt")
	}
	s, err := store.Open()
	if err != nil {
		return err
	}
	var input string
	if isTerminal(os.Stdin) {
		// Nothing piped in: the message is most likely on the clipboard.
		if input, err = clip.Read(); err != nil {
			return err
		}
	} else {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 4*message.MaxTextSize))
		if err != nil {
			return err
		}
		input = string(b)
	}
	if !message.Contains(input) {
		return message.ErrNoMessage
	}
	id, err := loadIdentity(s)
	if err != nil {
		return err
	}
	book, err := s.LoadContacts()
	if err != nil {
		return err
	}
	o, err := message.Open(input, id)
	if err != nil {
		return err
	}
	reportSender(book, id, o)
	if !isTerminal(os.Stdout) {
		// Redirected: the exact text, as scripts need it.
		_, err := fmt.Fprint(os.Stdout, o.Text)
		return err
	}
	// The text comes from the sender. On a terminal it is stripped of
	// control characters, so it cannot move the cursor over the sender line,
	// and indented, so that none of its lines can pass for that line.
	for _, line := range strings.Split(termsafe.Text(strings.TrimSuffix(o.Text, "\n")), "\n") {
		if err := out("│ " + line); err != nil {
			return err
		}
	}
	return nil
}

// reportSender writes the signature status to stderr so stdout carries only the text.
func reportSender(book *store.Book, me *keys.Identity, o *message.Opened) {
	var c *store.Contact
	if o.Sender != nil {
		c = book.ByKey(*o.Sender)
	}
	switch {
	case o.Sender == nil:
		fmt.Fprintln(os.Stderr, "⚠ "+T("opened.unsigned"))
	case *o.Sender == me.Public():
		fmt.Fprintln(os.Stderr, "✓ "+T("opened.signed_self"))
	case c == nil:
		fmt.Fprintln(os.Stderr, "⚠ "+T("opened.unknown_sender"))
		fmt.Fprintln(os.Stderr, "  "+T("contact.fingerprint")+": "+o.Sender.Fingerprint().String())
		fmt.Fprintln(os.Stderr, "  "+o.Sender.String())
	case c.Verified:
		fmt.Fprintln(os.Stderr, "✓ "+T("opened.signed_verified", termsafe.Line(c.Name)))
	default:
		fmt.Fprintln(os.Stderr, "✓ "+T("opened.signed", termsafe.Line(c.Name)))
		fmt.Fprintln(os.Stderr, "⚠ "+T("opened.not_verified"))
	}
	// Only the time tells a replayed old message from a new one.
	if !o.SentAt.IsZero() {
		fmt.Fprintln(os.Stderr, "  "+T("opened.sent_at", i18n.Time(o.SentAt)))
	}
}

func cmdKey() error {
	s, err := store.Open()
	if err != nil {
		return err
	}
	id, err := loadIdentity(s)
	if err != nil {
		return err
	}
	if err := out(id.Public().String()); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, T("contact.fingerprint")+": "+id.Public().Fingerprint().String())
	return nil
}

func cmdContacts() error {
	s, err := store.Open()
	if err != nil {
		return err
	}
	book, err := s.LoadContacts()
	if err != nil {
		return err
	}
	if len(book.Contacts) == 0 {
		return out(T("contacts.empty"))
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, c := range book.Contacts {
		status := T("contact.unverified")
		if c.Verified {
			status = T("contact.verified")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", termsafe.Line(c.Name), c.PublicKey().Fingerprint(), status)
	}
	return tw.Flush()
}

func cmdAdd(args []string) error {
	if len(args) != 2 {
		return usageError("cli.usage_add")
	}
	s, err := store.Open()
	if err != nil {
		return err
	}
	pk, err := keys.ParsePublicKey(keys.FindPublicKey(args[1]))
	if err != nil {
		return err
	}
	// Only checked when it needs no passphrase prompt; adding your own key is harmless.
	if locked, err := s.IdentityLocked(); err == nil && !locked {
		if id, err := s.LoadIdentity(""); err == nil && id.Public() == pk {
			return errors.New(T("err.own_key"))
		}
	}
	book, err := s.LoadContacts()
	if err != nil {
		return err
	}
	i, err := book.Add(args[0], pk)
	if err != nil {
		return err
	}
	if err := s.SaveContacts(book); err != nil {
		return err
	}
	return out(T("cli.added", termsafe.Line(book.Contacts[i].Name), pk.Fingerprint()))
}

func cmdInit(args []string) error {
	noPassphrase := len(args) == 1 && args[0] == "--no-passphrase"
	if len(args) > 1 || len(args) == 1 && !noPassphrase {
		return usageError("cli.usage_init")
	}
	s, err := store.Open()
	if err != nil {
		return err
	}
	if s.HasIdentity() {
		return errors.New(T("cli.identity_exists", s.Dir))
	}
	passphrase := ""
	if !noPassphrase {
		if passphrase, err = readSecret(T("cli.passphrase_new")); err != nil {
			return err
		}
		if passphrase != "" {
			again, err := readSecret(T("cli.passphrase_repeat"))
			if err != nil {
				return err
			}
			if again != passphrase {
				return errors.New(T("pass.mismatch"))
			}
		}
	}
	id, err := keys.Generate()
	if err != nil {
		return err
	}
	if err := s.CreateIdentity(id, passphrase); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, T("cli.created"))
	if err := out(id.Public().String()); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, T("contact.fingerprint")+": "+id.Public().Fingerprint().String())
	return nil
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// maxPromptInput is the longest line a terminal prompt passes on intact: in
// canonical mode macOS cuts lines at 1024 bytes and Linux at 4096.
const maxPromptInput = 1023

// readSecret prompts on the terminal without echo, even when stdin is a pipe.
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr)
	f := os.Stdin
	if !isTerminal(f) {
		tty, err := openTTY()
		if err != nil {
			return "", errors.New(T("cli.no_tty"))
		}
		defer tty.Close()
		f = tty
	}
	b, err := term.ReadPassword(int(f.Fd()))
	if err != nil {
		return "", err
	}
	// The prompt reads one line. Further pasted lines would stay queued and
	// reach the shell after hush exits, which would run them as commands and
	// keep them in its history, so they are discarded and the input refused.
	pending, _ := discardPending(f)
	if pending || len(b) >= maxPromptInput {
		return "", errors.New(T("cli.multiline"))
	}
	return string(b), nil
}
