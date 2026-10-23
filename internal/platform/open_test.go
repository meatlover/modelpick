package platform

import (
	"errors"
	"testing"
)

func TestOpenCommand(t *testing.T) {
	if got := openCommand("darwin"); got != "open" {
		t.Errorf("darwin = %q, want open", got)
	}
	if got := openCommand("linux"); got != "xdg-open" {
		t.Errorf("linux = %q, want xdg-open", got)
	}
}

func TestOpenBrowserWith(t *testing.T) {
	var name string
	var args []string
	err := openBrowserWith("linux", func(n string, a ...string) error {
		name, args = n, a
		return nil
	}, "http://127.0.0.1:1/")
	if err != nil || name != "xdg-open" || len(args) != 1 || args[0] != "http://127.0.0.1:1/" {
		t.Fatalf("got %q %v %v", name, args, err)
	}
	boom := errors.New("boom")
	if err := openBrowserWith("darwin", func(string, ...string) error { return boom }, "u"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
