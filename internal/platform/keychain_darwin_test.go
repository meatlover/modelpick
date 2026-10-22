//go:build darwin

package platform

import (
	"context"
	"strings"
	"testing"
)

// shRunner records the command it was asked to run, then runs script under
// /bin/sh instead, so the real Keychain is never touched.
func shRunner(script string, gotName *string, gotArgs *[]string) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		*gotName, *gotArgs = name, args
		return ExecRunner(ctx, "/bin/sh", "-c", script)
	}
}

func TestKeychainPasswordInterpretsSecurity(t *testing.T) {
	const svc = "modelpick-test-service-does-not-exist"
	var name string
	var args []string

	secret, found, err := keychainPassword(context.Background(), shRunner(`printf '{"a":1}\n'`, &name, &args), svc)
	if err != nil || !found || secret != `{"a":1}` {
		t.Fatalf("found item: got (%q, %v, %v)", secret, found, err)
	}
	if name != "/usr/bin/security" || strings.Join(args, " ") != "find-generic-password -s "+svc+" -w" {
		t.Fatalf("command = %s %q", name, args)
	}

	secret, found, err = keychainPassword(context.Background(), shRunner("exit 44", &name, &args), svc)
	if err != nil || found || secret != "" {
		t.Fatalf("exit 44: want not found, got (%q, %v, %v)", secret, found, err)
	}

	_, found, err = keychainPassword(context.Background(), shRunner("echo SECRET-ON-STDERR >&2; exit 51", &name, &args), svc)
	if err == nil || found {
		t.Fatalf("exit 51: want an error, got found=%v err=%v", found, err)
	}
	if !strings.Contains(err.Error(), "exit status 51") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("exit 51: error should name the status and omit output: %v", err)
	}
}
