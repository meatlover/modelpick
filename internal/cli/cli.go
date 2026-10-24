// Package cli wires configuration, usage store and vendor adapters into the
// modelpick commands.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

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
	case "pick", "env", "ls", "doctor":
		// Ctrl-C or SIGTERM cancels a login renewal or usage call under way.
		// The first signal only cancels: stop then restores the default
		// action, so a second one kills a run stuck in a wait ctx cannot end.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		context.AfterFunc(ctx, stop)
		d.Ctx = ctx
		code := dispatch(args, stdout, stderr, d)
		if ctx.Err() != nil {
			return exitInterrupted
		}
		return code
	}
	return dispatch(args, stdout, stderr, d)
}

// exitInterrupted is the shell's code for a process ended by SIGINT.
const exitInterrupted = 130

func dispatch(args []string, stdout, stderr io.Writer, d app.Deps) int {
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
