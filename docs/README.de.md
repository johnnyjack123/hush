# hush

[English](../README.md) · **Deutsch**

**Texte sicher verschlüsseln – z. B. um Passwörter per Telegram, WhatsApp & Co. zu verschicken.**

hush ist ein kleines Terminal-Programm mit komfortabler Tastatur-Oberfläche. Es baut auf
[age](https://age-encryption.org) auf, verwaltet deine Kontakte lokal und signiert jede
Nachricht, damit der Empfänger weiß, dass sie wirklich von dir kommt.

```
 hush   Texte sicher verschlüsseln
╭─ Kontakte ─────────────────────╮╭─ Nachricht an Alice ───────────────────────────────╮
│ ▸ Alice            ✓ 7F3A-91C2 ││ Wird für Alice verschlüsselt und von dir signiert. │
│   Bob                C04E-1B77 ││                                                    │
│                                ││ hunter2                                            │
│   + Neuer Kontakt              ││                                                    │
╰────────────────────────────────╯╰────────────────────────────────────────────────────╯
 ctrl+s verschlüsseln · esc abbrechen
```

- Eine einzige Datei (~5 MB), keine Installation, keine Abhängigkeiten
- Windows, Linux und macOS (Intel & Apple Silicon)
- Alles bleibt lokal – kein Server, kein Konto
- Deutsch und Englisch (automatisch nach Systemsprache)

## So funktioniert's

1. **Alice** startet hush. Beim ersten Start wird ihr Schlüsselpaar erzeugt (optional mit Passphrase).
2. Sie drückt `k`, kopiert ihren **öffentlichen Schlüssel** (`hush1…`) und schickt ihn dir.
3. Du kopierst den Schlüssel und drückst `a` – hush übernimmt ihn aus der Zwischenablage. Name eingeben, fertig.
4. Ihr vergleicht einmal beide **Fingerprints** vollständig (z. B. `7F3A-91C2-04BE-1D77-A0C3-5E19`) über einen zweiten Kanal,
   etwa per Anruf – hush zeigt beide auf Alices Kontaktseite. Stimmen sie, markierst du Alice mit `v` als verifiziert.
5. Kontakt wählen, `e` drücken, Passwort eingeben, `Strg+S`. Die verschlüsselte Nachricht liegt
   jetzt in der Zwischenablage – einfach in Telegram einfügen.
6. Alice kopiert die Nachricht und drückt in hush `d`. hush erkennt die Nachricht in der
   Zwischenablage, entschlüsselt sie und zeigt, dass sie von dir signiert ist.

## Installation

Fertige Programme gibt es unter [Releases](https://github.com/johnnyjack123/hush/releases). Herunterladen, entpacken, starten.

- **Windows:** `hush.exe` z. B. nach `%LOCALAPPDATA%\Programs\hush` legen und den Ordner zum `PATH` hinzufügen –
  oder einfach doppelklicken.
- **Linux:** `chmod +x hush` und nach `~/.local/bin` verschieben. Für die Zwischenablage wird
  `wl-clipboard` (Wayland) oder `xclip`/`xsel` (X11) benötigt.
- **macOS:** Die Programme sind nicht signiert. Einmalig `xattr -d com.apple.quarantine hush` ausführen.

Aus dem Quellcode (Go ≥ 1.27):

```sh
go install github.com/johnnyjack123/hush@latest
```

## Tastenkürzel

| Taste       | Aktion                                              |
|-------------|-----------------------------------------------------|
| `↑` `↓`     | Kontakt auswählen                                   |
| `enter`/`e` | Nachricht an den Kontakt verschlüsseln              |
| `d`         | Nachricht entschlüsseln (liest die Zwischenablage)  |
| `a`         | Kontakt hinzufügen                                  |
| `v`         | Fingerprint als verifiziert markieren               |
| `r` / `x`   | Kontakt umbenennen / löschen                        |
| `k`         | Eigenen Schlüssel anzeigen, Passphrase ändern (`p`) |
| `?`         | Hilfe                                               |
| `q`         | Beenden                                             |

## Kommandozeile

Für Skripte gibt es zusätzlich Unterbefehle:

```sh
hush                              # Oberfläche starten
echo "geheim" | hush encrypt alice > nachricht.txt
hush decrypt < nachricht.txt      # Klartext auf stdout, Absender-Prüfung auf stderr
hush decrypt                      # ohne Eingabe: liest die Zwischenablage
hush key                          # eigener öffentlicher Schlüssel
hush contacts                     # Kontakte auflisten
hush add Alice hush1…             # Kontakt hinzufügen
hush init [--no-passphrase]       # Schlüsselpaar ohne Oberfläche erzeugen
```

Im Terminal zeigt `hush decrypt` den Text eingerückt und mit sichtbar gemachten Steuerzeichen an, damit eine
Nachricht die Absenderzeile weder nachahmen noch überschreiben kann. Umgeleitet (`> datei`, `| clip`) kommt der
Text unverändert heraus; die Absender-Prüfung steht immer auf stderr.

## Sicherheit

**Verschlüsselung.** Nachrichten sind normale, ASCII-armierte [age](https://age-encryption.org)-Dateien
(X25519, ChaCha20-Poly1305). Wer den privaten Schlüssel nicht hat, kann sie nicht lesen oder unbemerkt verändern.

**Signatur.** age selbst weist den Absender nicht nach. hush signiert deshalb jede Nachricht innen mit einem
separaten Ed25519-Schlüssel. Die Signatur deckt auch den Schlüssel des Empfängers ab – ein Empfänger kann eine
signierte Nachricht also nicht an Dritte weiterverschlüsseln und so tun, als sei sie für diese bestimmt.

**Fingerprints.** Ein öffentlicher Schlüssel, der per Messenger kommt, könnte unterwegs ausgetauscht worden sein.
Vergleicht deshalb die 96-Bit-Fingerprints einmal vollständig über einen anderen Kanal (Anruf, persönlich).

**Länge.** Nachrichten werden auf Blöcke von 128 Byte aufgefüllt, damit man an der Länge nicht ablesen kann,
wie lang ein Passwort ist.

**Privater Schlüssel.** Er liegt nur auf deinem Gerät und wird auf Wunsch mit einer Passphrase verschlüsselt
(age/scrypt).

**Zwischenablage.** Kopierter Klartext wird nach 30 Sekunden wieder gelöscht. Unter Windows wird er zusätzlich vom
Zwischenablage-Verlauf (`Win+V`) und der Cloud-Synchronisierung ausgeschlossen; unter Linux und macOS können
Zwischenablage-Manager wie Klipper oder Maccy trotzdem eine eigene Kopie behalten. Die Oberfläche läuft im
alternativen Bildschirm des Terminals, entschlüsselte Texte bleiben also nicht im Scroll-Verlauf stehen.

**Grenzen.** hush schützt nicht vor Schadsoftware auf deinem Rechner oder dem des Empfängers. Gegen
Wiederholung einer alten Nachricht hilft nur der angezeigte Sendezeitpunkt.

**Kompatibilität.** Unter `k` steht zusätzlich dein `age1…`-Empfänger. Damit können andere auch mit dem
normalen `age`-CLI an dich verschlüsseln; hush zeigt solche Nachrichten als „nicht signiert“ an.

## Daten

| System  | Ordner                                  |
|---------|-----------------------------------------|
| Windows | `%AppData%\hush`                        |
| Linux   | `~/.config/hush`                        |
| macOS   | `~/Library/Application Support/hush`    |

`identity.key` enthält deinen privaten Schlüssel – sichern, aber niemals weitergeben.
`contacts.json` enthält deine Kontakte. Mit `HUSH_HOME` lässt sich ein anderer Ordner verwenden,
mit `HUSH_LANG=en` bzw. `HUSH_LANG=de` die Sprache festlegen. hush hält den Ordner privat: Unter Linux und
macOS lehnt es einen Ordner ab, der einem anderen Benutzer gehört, und verschärft zu offene Rechte; unter
Windows beschränkt es den Zugriff auf dein Konto.

## Entwicklung

```sh
go test ./...
go run .
```

Ein Release entsteht automatisch, sobald ein Tag gepusht wird:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

GitHub Actions baut dann mit [GoReleaser](https://goreleaser.com) die Programme für alle Plattformen.

Nach einer Änderung an den Abhängigkeiten muss die Lizenzübersicht neu erzeugt werden (die CI prüft das):

```sh
go run ./tools/licenses
```

## Ideen für später

- Android-App mit grafischer Oberfläche (die Schlüssel sind kompatibel)
- Mehrere Empfänger pro Nachricht

## Lizenz

[MIT](../LICENSE). hush nutzt [age](https://github.com/FiloSottile/age) (BSD-3-Clause) und die
Terminal-Bibliotheken von [Charm](https://github.com/charmbracelet) (MIT). Die Lizenztexte aller
enthaltenen Bibliotheken stehen in [THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt) und liegen
jedem Release bei.
