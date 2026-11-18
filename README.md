# hush

**English** · [Deutsch](docs/README.de.md)

**Encrypt text safely – e.g. to send passwords over Telegram, WhatsApp and the like.**

hush is a small terminal program with a comfortable keyboard interface. It is built on
[age](https://age-encryption.org), keeps your contacts locally and signs every message, so the
recipient knows it really came from you.

```
 hush   encrypt text, safely
╭─ Contacts ─────────────────────╮╭─ Message to Alice ─────────────────────────────────╮
│ ▸ Alice            ✓ 7F3A-91C2 ││ Will be encrypted for Alice and signed by you.     │
│   Bob                C04E-1B77 ││                                                    │
│                                ││ hunter2                                            │
│   + New contact                ││                                                    │
╰────────────────────────────────╯╰────────────────────────────────────────────────────╯
 ctrl+s encrypt · esc cancel
```

- A single file (~5 MB): no installation, no dependencies
- Windows, Linux and macOS (Intel & Apple Silicon)
- Everything stays local: no server, no account
- English and German (picked from the system language)

## How it works

1. **Alice** starts hush. On first start it creates her key pair (optionally protected by a passphrase).
2. She presses `k` to show her **public key** (`hush1…`), copies it with `c` and sends it to you.
3. You copy the key and press `a`: hush takes it from the clipboard. Enter a name, done.
4. Once, you compare both **fingerprints** in full (e.g. `7F3A-91C2-04BE-1D77-A0C3-5E19`) over a
   second channel, such as a phone call; hush shows both on Alice's contact page. If they match,
   mark Alice as verified with `v`.
5. Pick the contact, press `e`, type the password, press `Ctrl+S`. The encrypted message is now
   on the clipboard: just paste it into Telegram. For a group, mark several contacts with `space`
   first; every one of them can open the same message.
6. Alice copies the message and presses `d` in hush. hush finds the message on the clipboard,
   decrypts it and shows that it was signed by you.

## Installation

Ready-made binaries are on the [Releases](https://github.com/johnnyjack123/hush/releases) page.
Download, unpack, run.

- **Windows:** put `hush.exe` somewhere like `%LOCALAPPDATA%\Programs\hush` and add that folder to
  your `PATH`, or simply double-click it.
- **Linux:** `chmod +x hush` and move it to `~/.local/bin`. Clipboard access needs `wl-clipboard`
  (Wayland) or `xclip`/`xsel` (X11).
- **macOS:** the binaries are not signed. Run `xattr -d com.apple.quarantine hush` once.

Later versions install with `hush update`. hush never checks for updates by itself.

From source (Go ≥ 1.27):

```sh
go install github.com/johnnyjack123/hush@latest
```

## Keys

| Key         | Action                                           |
|-------------|--------------------------------------------------|
| `↑` `↓`     | Select a contact                                 |
| `enter`/`e` | Encrypt a message to the contact                 |
| `d`         | Decrypt a message (reads the clipboard)          |
| `a`         | Add a contact                                    |
| `v`         | Mark the fingerprint as verified                 |
| `r` / `x`   | Rename / delete a contact                        |
| `k`         | Your key: copy (`c`), passphrase (`p`), remember device (`m`) |
| `space`     | Mark contacts for a message to several            |
| `esc`       | Back to the contact list, clear marks            |
| `?`         | Help                                             |
| `q`         | Quit                                             |

## Command line

For scripts there are subcommands as well:

```sh
hush                              # start the interface
echo "secret" | hush encrypt alice > message.txt
echo "secret" | hush encrypt alice bob    # one message for several contacts
hush decrypt < message.txt        # plain text on stdout, sender check on stderr
hush decrypt                      # no input: reads the clipboard
hush key                          # your own public key
hush contacts                     # list contacts
hush add Alice hush1…             # add a contact
hush init [--no-passphrase]       # create a key pair without the interface
hush remember                     # unlock without the passphrase on this device
hush forget                       # ask for the passphrase on this device again
hush update                       # install the latest release
```

In a terminal, `hush decrypt` shows the text indented and with control characters made visible, so
a message can neither imitate nor overwrite the sender line. Redirected (`> file`, `| clip`), the
text comes out unchanged; the sender check always goes to stderr.

## Security

**Encryption.** Messages are regular ASCII-armored [age](https://age-encryption.org) files
(X25519, ChaCha20-Poly1305). Without the private key nobody can read them or change them unnoticed.

**Signature.** age on its own does not prove who sent a message. hush therefore signs every message
inside the encryption with a separate Ed25519 key. The signature also covers the recipient's key, so
a recipient cannot re-encrypt a signed message to someone else and pass it off as meant for them.

**Fingerprints.** A public key that arrives over a messenger could have been swapped on the way.
Compare the 96-bit fingerprints once, in full, over another channel (a call, in person).

**Length.** Messages are padded to blocks of 128 bytes, so their length does not reveal how long a
password is.

**Private key.** It stays on your device and can be encrypted with a passphrase (age/scrypt).

**Remember this device.** Typing the passphrase at every start can be skipped: under `k`, press `m`.
hush then keeps a second copy of your private key, encrypted with a random key that it hands to
the system keyring: Windows Credential Manager, the macOS Keychain, or GNOME Keyring/KWallet on
Linux. The keyring opens when you log in, so hush starts without asking. `identity.key` and its
backups stay protected by the passphrase alone, and the copy is useless without your system login
on this device. The trade-off: anyone who can use your unlocked user account can open hush, so
lock the screen when you step away. Pressing `m` again, or `hush forget`, removes the copy.

**Clipboard.** Copied plain text is cleared again after 30 seconds. On Windows it is also kept out of
the clipboard history (`Win+V`) and cloud sync; on Linux and macOS, clipboard managers such as
Klipper or Maccy may still keep their own copy. The interface runs in the terminal's alternate
screen, so decrypted text does not remain in the scrollback.

**Several recipients.** A message for several contacts is encrypted once to all of them. The
signature covers the whole list, so nobody on it can forward the message to someone else as if it
had been meant for them. Every recipient sees who else received it.

**Updates.** `hush update` only installs a release whose checksums are signed with the hush release
key built into the program, checks the download against them and makes sure the new version starts
before replacing the old one. The signing key exists only as a secret of this repository's
release workflow.

**Limits.** hush does not protect against malware on your machine or the recipient's. Against the
replay of an old message, only the displayed send time helps.

**Compatibility.** Under `k` you will also find your `age1…` recipient. With it, others can encrypt
to you with the regular `age` CLI; hush shows such messages as "not signed".

## Data

| System  | Folder                                  |
|---------|-----------------------------------------|
| Windows | `%AppData%\hush`                        |
| Linux   | `~/.config/hush`                        |
| macOS   | `~/Library/Application Support/hush`    |

`identity.key` holds your private key: back it up, but never share it. `contacts.json` holds your
contacts. `HUSH_HOME` selects a different folder, `HUSH_LANG=en` or `HUSH_LANG=de` sets the
language. hush keeps the folder private: on Linux and macOS it refuses a folder owned by another
user and tightens loose permissions, on Windows it restricts access to your account.

## Development

```sh
go test ./...
go run .
```

Pushing a tag creates a release automatically:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

GitHub Actions then builds the binaries for all platforms with [GoReleaser](https://goreleaser.com),
signs `checksums.txt` with the `HUSH_RELEASE_KEY` secret of the `release` environment and publishes
the release. `go run ./tools/releasesign -keygen` creates a new key pair, stores the private key as
that secret via `gh` and prints the public key for `internal/update/key.go`.

After changing dependencies, regenerate the license overview (CI checks it):

```sh
go run ./tools/licenses
```

## Ideas for later

- Android app with a graphical interface (the keys are compatible)

## License

[MIT](LICENSE). hush uses [age](https://github.com/FiloSottile/age) (BSD-3-Clause) and the
terminal libraries by [Charm](https://github.com/charmbracelet) (MIT). The license texts of all
included libraries are in [docs/THIRD_PARTY_LICENSES.txt](docs/THIRD_PARTY_LICENSES.txt) and ship
with every release.
