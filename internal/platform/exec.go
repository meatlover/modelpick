package platform

import (
	"context"
	"os/exec"
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
