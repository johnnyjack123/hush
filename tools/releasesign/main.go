// Command releasesign signs a release's checksums.txt for hush's updater.
//
//	releasesign -keygen                          new key pair (needs gh)
//	HUSH_RELEASE_KEY=… releasesign dist/checksums.txt
//
// The private key is a base64-encoded Ed25519 seed. -keygen hands it
// straight to "gh secret set" for the repository's release environment and
// prints only the public key, as hex for internal/update/key.go.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/johnnyjack123/hush/internal/update"
)

func main() {
	keygen := flag.Bool("keygen", false, "create a key pair, store the private key as GitHub secret, print the public key")
	flag.Parse()
	var err error
	if *keygen {
		err = generate()
	} else {
		err = sign(flag.Arg(0))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasesign:", err)
		os.Exit(1)
	}
}

// generate never writes the private key to stdout: a terminal cannot always
// be told apart from a pipe (Git Bash's mintty is a pipe), and a key shown
// once stays in the scrollback.
func generate() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	cmd := exec.Command("gh", "secret", "set", "HUSH_RELEASE_KEY", "--env", "release")
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(priv.Seed()))
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh secret set: %w", err)
	}
	_, err = fmt.Println(hex.EncodeToString(pub))
	return err
}

func sign(file string) error {
	if file == "" {
		return errors.New("usage: releasesign <checksums file>")
	}
	seed, err := base64.StdEncoding.DecodeString(os.Getenv("HUSH_RELEASE_KEY"))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("HUSH_RELEASE_KEY is missing or not a base64 Ed25519 seed")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), append([]byte(update.SignatureContext), data...))
	encoded := []byte(base64.StdEncoding.EncodeToString(sig) + "\n")
	// Checking against the key compiled into hush makes a mismatched secret
	// fail the release instead of every user's update.
	if !update.VerifyChecksums(data, encoded) {
		return errors.New("signature does not verify with the release key in internal/update/key.go")
	}
	return os.WriteFile(file+".sig", encoded, 0o644)
}
