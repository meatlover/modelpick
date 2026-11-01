package platform

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Runner runs a program and returns its standard output. Adapters take one so
// tests can substitute a fake for the real process.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the real Runner: it runs name with args in ChildDir and
// returns stdout. A non-zero exit is an *exec.ExitError carrying stderr.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = ChildDir()
	return cmd.Output()
}

// ChildDir is the working directory every child process runs in: the home
// directory, else the temp directory, else "/". A child that inherited the
// caller's directory would fail when that directory has been deleted
// (opencode refuses to start), and a project directory can change how a tool
// behaves; modelpick's own directory never matters to the tools it runs.
func ChildDir() string {
	if home, err := os.UserHomeDir(); err == nil && isDir(home) {
		return home
	}
	if tmp := os.TempDir(); isDir(tmp) {
		return tmp
	}
	return "/"
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// LookPathFunc resolves a program on PATH, like exec.LookPath.
type LookPathFunc func(string) (string, error)

// Cmd describes a program to run. A nil Env inherits the parent's; an empty
// Dir means ChildDir.
type Cmd struct {
	Name  string
	Args  []string
	Env   []string
	Dir   string
	Stdin io.Reader
	// NewGroup starts the program in its own process group, so a terminal's
	// Ctrl-C does not reach it: only cancelling ctx stops it.
	NewGroup bool
}

// CmdRunner runs a Cmd and returns its stdout and stderr. Adapters take one
// where they need the environment, working directory or stderr, so tests can
// substitute a fake for the real process.
type CmdRunner func(ctx context.Context, c Cmd) (stdout, stderr []byte, err error)

// cmdWaitDelay bounds how long Wait lingers on I/O after the process is
// killed, so a grandchild holding a pipe cannot hang the caller.
const cmdWaitDelay = 2 * time.Second

// RunCmd is the real CmdRunner. An empty Dir runs the program in ChildDir.
// A non-zero exit is an *exec.ExitError.
func RunCmd(ctx context.Context, c Cmd) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env, cmd.Dir, cmd.Stdin = c.Env, c.Dir, c.Stdin
	if cmd.Dir == "" {
		cmd.Dir = ChildDir()
	}
	cmd.WaitDelay = cmdWaitDelay
	if c.NewGroup {
		NewProcessGroup(cmd)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// NewProcessGroup makes cmd start in its own process group, so a signal sent
// to the terminal's foreground group does not reach it.
func NewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
