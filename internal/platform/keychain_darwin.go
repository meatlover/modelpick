//go:build darwin

package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// securityPath is absolute so a binary earlier on PATH cannot stand in for it.
const securityPath = "/usr/bin/security"

// exitItemNotFound is security(1)'s exit status for "The specified item could
// not be found in the keychain."
const exitItemNotFound = 44

// KeychainPassword returns the password of the generic-password item whose
// service is service, via `security find-generic-password -s <service> -w`.
// A missing item is ("", false, nil). Errors never include the tool's output.
func KeychainPassword(ctx context.Context, service string) (secret string, found bool, err error) {
	return keychainPassword(ctx, ExecRunner, service)
}

func keychainPassword(ctx context.Context, run Runner, service string) (string, bool, error) {
	out, err := run(ctx, securityPath, "find-generic-password", "-s", service, "-w")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ee.ExitCode() == exitItemNotFound {
				return "", false, nil
			}
			return "", false, fmt.Errorf("keychain lookup for %q failed: exit status %d", service, ee.ExitCode())
		}
		return "", false, fmt.Errorf("keychain lookup for %q failed: %w", service, err)
	}
	return strings.TrimRight(string(out), "\r\n"), true, nil
}
