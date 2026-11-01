package platform

import (
	"context"
	"os"
	"path/filepath"
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

// inDeletedDir makes the test process's working directory a directory that
// no longer exists, as when `modelpick ui` was started in a folder since
// removed.
func inDeletedDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir() + "/gone"
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
}

func samePath(t *testing.T, got, want string) {
	t.Helper()
	g, err1 := filepath.EvalSymlinks(strings.TrimSpace(got))
	w, err2 := filepath.EvalSymlinks(want)
	if err1 != nil || err2 != nil || g != w {
		t.Errorf("child ran in %q, want %q (%v, %v)", strings.TrimSpace(got), want, err1, err2)
	}
}

func TestExecRunnerRunsInChildDir(t *testing.T) {
	inDeletedDir(t)
	out, err := ExecRunner(context.Background(), "/bin/sh", "-c", "pwd -P")
	if err != nil {
		t.Fatalf("child failed in a deleted working directory: %v", err)
	}
	samePath(t, string(out), ChildDir())
}

func TestRunCmdDefaultsToChildDir(t *testing.T) {
	inDeletedDir(t)
	out, _, err := RunCmd(context.Background(), Cmd{Name: "/bin/sh", Args: []string{"-c", "pwd -P"}})
	if err != nil {
		t.Fatalf("child failed in a deleted working directory: %v", err)
	}
	samePath(t, string(out), ChildDir())
}

func TestRunCmdKeepsExplicitDir(t *testing.T) {
	dir := t.TempDir()
	out, _, err := RunCmd(context.Background(), Cmd{Name: "/bin/sh", Args: []string{"-c", "pwd -P"}, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	samePath(t, string(out), dir)
}

func TestChildDirExists(t *testing.T) {
	if fi, err := os.Stat(ChildDir()); err != nil || !fi.IsDir() {
		t.Fatalf("ChildDir() = %q is not a directory: %v", ChildDir(), err)
	}
}
