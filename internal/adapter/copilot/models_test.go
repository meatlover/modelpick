package copilot

import (
	"context"
	"os/exec"
	"reflect"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
)

func TestModels(t *testing.T) {
	var gotArgs []string
	a := New(Options{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("\x1b[2K\n  github-copilot/claude-sonnet-4.5\r\n\ngithub-copilot/gpt-5\n\x1b[0m github-copilot/gemini-2.5-pro \n"), nil
	}})
	got, err := a.Models(context.Background(), adapter.Account{})
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Model{{ID: "github-copilot/claude-sonnet-4.5"}, {ID: "github-copilot/gpt-5"}, {ID: "github-copilot/gemini-2.5-pro"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %+v", got)
	}
	if !reflect.DeepEqual(gotArgs, []string{"opencode", "models", "github-copilot"}) {
		t.Fatalf("argv = %v", gotArgs)
	}
}

func TestModelsExitNamesLastStderrLine(t *testing.T) {
	a := New(Options{Run: func(context.Context, string, ...string) ([]byte, error) {
		return exec.Command("/bin/sh", "-c",
			`echo "noise" >&2; printf '\033[31merror:\033[0m rejected key gho_abcdefghijklmnopqrstuvwxyz0123456789\n' >&2; exit 1`).Output()
	}})
	_, err := a.Models(context.Background(), adapter.Account{})
	if want := "copilot: opencode models exited with status 1: error: rejected key [redacted]"; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
