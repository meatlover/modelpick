// Package cli wires configuration, usage store and vendor adapters into the
// modelpick commands.
package cli

import (
	"fmt"
	"io"

	"github.com/meatlover/modelpick/internal/app"
)

const usageText = `usage:
  modelpick pick [<profile>] [--refresh]
  modelpick env <profile> [--refresh]
  modelpick ls [<account>…] [--refresh] [--json]
  modelpick ls models [--json]
  modelpick config   (alias: conf)
  modelpick doctor [all]
  modelpick ui [--port N] [--no-open]
`

// Main runs the command in args (without the program name) and returns the
// process exit code.
func Main(args []string, stdout, stderr io.Writer, d app.Deps) int {
	if len(args) == 0 {
		return badUsage(stderr, "missing command")
	}
	switch args[0] {
	case "pick":
		return runPick(args[1:], stdout, stderr, d)
	case "env":
		return runEnv(args[1:], stdout, stderr, d)
	case "ls":
		return runLs(args[1:], stdout, stderr, d)
	case "config", "conf":
		return runConfig(args[1:], stdout, stderr, d)
	case "ui":
		return runUI(args[1:], stdout, stderr, d)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr, d)
	}
	return badUsage(stderr, fmt.Sprintf("unknown command %q", args[0]))
}

// badUsage prints msg and the usage text on stderr and returns exit 1.
func badUsage(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "modelpick: %s\n\n%s", msg, usageText)
	return 1
}
