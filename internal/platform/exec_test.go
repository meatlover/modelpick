package platform

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestEnvWithout(t *testing.T) {
	in := []string{"FOO=1", "FOOBAR=1", "BAR=2", "PATH=/bin", "NOEQUALS"}
	got := EnvWithout(in, "FOO", "BAR")
	want := []string{"FOOBAR=1", "PATH=/bin", "NOEQUALS"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// pgidOf runs `ps` in a child started by RunCmd and returns that child's
// process group.
func pgidOf(t *testing.T, newGroup bool) int {
	t.Helper()
	out, _, err := RunCmd(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "ps -o pgid= -p $$"}, NewGroup: newGroup})
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("pgid %q: %v", out, err)
	}
	return n
}

func TestRunCmdNewGroup(t *testing.T) {
	own, _ := syscall.Getpgid(os.Getpid())
	if got := pgidOf(t, false); got != own {
		t.Errorf("default child pgid = %d, want the parent's %d", got, own)
	}
	if got := pgidOf(t, true); got == own {
		t.Errorf("NewGroup child pgid = %d, want a group of its own", got)
	}
}
