package claude

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

var authVars = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
	"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"}

// renewFixture is an adapter whose RunCmd records the Cmd it was given and
// returns canned output. Dir is checked to exist and be empty during the call.
type renewFixture struct {
	a    *Adapter
	home string
	got  platform.Cmd
	ran  int
}

func newRenewFixture(t *testing.T, stderr string, err error, environ []string) *renewFixture {
	t.Helper()
	f := &renewFixture{home: t.TempDir()}
	f.a = New(Options{Home: f.home, Environ: func() []string { return environ },
		RunCmd: func(_ context.Context, c platform.Cmd) ([]byte, []byte, error) {
			f.ran++
			f.got = c
			ents, derr := os.ReadDir(c.Dir)
			if derr != nil || len(ents) != 0 {
				t.Errorf("Dir %q must exist and be empty during the call (err %v, %d entries)", c.Dir, derr, len(ents))
			}
			return []byte("ok"), []byte(stderr), err
		}})
	return f
}

func (f *renewFixture) acct(dir string) adapter.Account {
	return adapter.Account{Name: "a", Vendor: "claude", ConfigDir: filepath.Join(f.home, dir)}
}

func envCount(env []string, name string) int {
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			n++
		}
	}
	return n
}

func TestRenewArgv(t *testing.T) {
	f := newRenewFixture(t, "", nil, nil)
	if err := f.a.Renew(context.Background(), f.acct(".claude")); err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "Reply with exactly one word: ok", "--model", "haiku", "--no-session-persistence",
		"--setting-sources", "", "--strict-mcp-config", "--tools", ""}
	if f.got.Name != "claude" || !reflect.DeepEqual(f.got.Args, want) {
		t.Fatalf("got %q %q", f.got.Name, f.got.Args)
	}
}

func TestRenewNonDefaultDirSetsConfigDir(t *testing.T) {
	f := newRenewFixture(t, "", nil, []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/elsewhere"})
	if err := f.a.Renew(context.Background(), f.acct(".claude-rec/")); err != nil {
		t.Fatal(err)
	}
	if n := envCount(f.got.Env, "CLAUDE_CONFIG_DIR"); n != 1 {
		t.Fatalf("want exactly one CLAUDE_CONFIG_DIR, got %d in %v", n, f.got.Env)
	}
	want := "CLAUDE_CONFIG_DIR=" + filepath.Join(f.home, ".claude-rec")
	found := false
	for _, e := range f.got.Env {
		found = found || e == want
	}
	if !found {
		t.Fatalf("want %q in %v", want, f.got.Env)
	}
}

func TestRenewDefaultDirUnsetsInheritedConfigDir(t *testing.T) {
	env := []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/x/.claude-rec"}
	for _, v := range authVars {
		env = append(env, v+"=secret")
	}
	f := newRenewFixture(t, "", nil, env)
	if err := f.a.Renew(context.Background(), f.acct(".claude")); err != nil {
		t.Fatal(err)
	}
	for _, v := range append([]string{"CLAUDE_CONFIG_DIR"}, authVars...) {
		if envCount(f.got.Env, v) != 0 {
			t.Errorf("%s must not reach the child: %v", v, f.got.Env)
		}
	}
	if envCount(f.got.Env, "PATH") != 1 {
		t.Errorf("PATH must be kept: %v", f.got.Env)
	}
}

func TestRenewTempDirRemoved(t *testing.T) {
	for name, err := range map[string]error{"success": nil, "failure": errors.New("exit status 1")} {
		t.Run(name, func(t *testing.T) {
			f := newRenewFixture(t, "", err, nil)
			_ = f.a.Renew(context.Background(), f.acct(".claude"))
			if f.ran != 1 || f.got.Dir == "" {
				t.Fatalf("not run: %d %q", f.ran, f.got.Dir)
			}
			if _, serr := os.Stat(f.got.Dir); !os.IsNotExist(serr) {
				t.Fatalf("Dir %q still exists (stat err %v)", f.got.Dir, serr)
			}
		})
	}
}

func TestRenewFailureReason(t *testing.T) {
	f := newRenewFixture(t, "Error: something\nmore", errors.New("exit status 1"), nil)
	err := f.a.Renew(context.Background(), f.acct(".claude"))
	if err == nil || err.Error() != "claude: exit status 1: Error: something" {
		t.Fatalf("got %v", err)
	}
	f = newRenewFixture(t, strings.Repeat("x", 500), errors.New("exit status 1"), nil)
	if err := f.a.Renew(context.Background(), f.acct(".claude")); err == nil || len(err.Error()) > 200 {
		t.Fatalf("want an error of at most 200 bytes, got %v", err)
	}
}

func TestRenewToolMissing(t *testing.T) {
	f := newRenewFixture(t, "", exec.ErrNotFound, nil)
	err := f.a.Renew(context.Background(), f.acct(".claude"))
	if err == nil || !strings.Contains(err.Error(), "claude not found on PATH") {
		t.Fatalf("got %v", err)
	}
}
