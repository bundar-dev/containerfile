// Command containerfile generates a Containerfile/Dockerfile by scanning a
// project directory.
package main

import (
	"os"

	"github.com/bundar-dev/containerfile/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version, os.Stdout, os.Stderr))
}
