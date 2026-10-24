// Command modelpick picks which vendor account and model to use next.
package main

import (
	"os"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, app.DefaultDeps(cli.Version)))
}
