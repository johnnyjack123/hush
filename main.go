// Command hush encrypts short texts such as passwords so they can be sent
// safely over a messenger.
package main

import (
	"os"

	"github.com/johnnyjack123/hush/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
