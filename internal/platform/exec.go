package platform

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Runner runs a program and returns its standard output. Adapters take one so
// tests can substitute a fake for the real process.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the real Runner: it runs name with args and returns stdout.
// A non-zero exit is an *exec.ExitError.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// LookPathFunc resolves a program on PATH, like exec.LookPath.
type LookPathFunc func(string) (string, error)

// Cmd describes a program to run. A nil Env inherits the parent's.
type Cmd struct {
	Name  string
	Args  []string
	Env   []string
	Dir   string
	Stdin io.Reader
}

// CmdRunner runs a Cmd and returns its stdout and stderr. Adapters take one
// where they need the environment, working directory or stderr, so tests can
// substitute a fake for the real process.
type CmdRunner func(ctx context.Context, c Cmd) (stdout, stderr []byte, err error)

// cmdWaitDelay bounds how long Wait lingers on I/O after the process is
// killed, so a grandchild holding a pipe cannot hang the caller.
const cmdWaitDelay = 2 * time.Second

// RunCmd is the real CmdRunner. A non-zero exit is an *exec.ExitError.
func RunCmd(ctx context.Context, c Cmd) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env, cmd.Dir, cmd.Stdin = c.Env, c.Dir, c.Stdin
	cmd.WaitDelay = cmdWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// EnvWithout returns env without the entries for the named variables. Only an
// exact NAME=... entry is dropped: FOO does not drop FOOBAR=1.
func EnvWithout(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, e := range env {
		for _, n := range names {
			if strings.HasPrefix(e, n+"=") {
				continue outer
			}
		}
		out = append(out, e)
	}
	return out
}
