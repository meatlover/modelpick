package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

const wantEnvWithErrors = `echo 'modelpick: codex-main: usage endpoint returned HTTP 401; log in to Codex with this account' >&2
export MODELPICK_STATUS='ok_with_errors'
export MODELPICK_PROFILE='codex-first'
export MODELPICK_VENDOR='claude'
export MODELPICK_ACCOUNT='claude-rec'
export MODELPICK_MODEL='claude-sonnet-5-5'
export MODELPICK_THINKING='medium'
export CLAUDE_CONFIG_DIR='/home/u/.claude-rec'
unset MODELPICK_KEY_FILE
unset MODELPICK_KEY_LINE
`

const wantEnvFailed = `unset MODELPICK_STATUS
unset MODELPICK_PROFILE
unset MODELPICK_VENDOR
unset MODELPICK_ACCOUNT
unset MODELPICK_MODEL
unset MODELPICK_THINKING
unset MODELPICK_KEY_FILE
unset MODELPICK_KEY_LINE
echo 'modelpick: same-account: no choice available' >&2
false
`

func envWithErrors(t *testing.T) (string, int) {
	t.Helper()
	codex := apptest.NewFake("codex")
	codex.Errs["codex-main"] = adapter.ErrUnauthorized
	d := apptest.NewDeps(t, testConfig, apptest.NewFake("claude"), codex)
	out, _, code := run(d, "env", "codex-first")
	return out, code
}

func envFailed(t *testing.T) (string, int) {
	t.Helper()
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(95, 10)
	out, _, code := run(apptest.NewDeps(t, testConfig, claude), "env", "same-account")
	return out, code
}

func lastLines(s string, n int) []string {
	ls := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	return ls[len(ls)-n:]
}

func TestEnvClaudeWithErrors(t *testing.T) {
	out, code := envWithErrors(t)
	if code != 2 || out != wantEnvWithErrors {
		t.Fatalf("exit %d, want 2; stdout\n%s\nwant\n%s", code, out, wantEnvWithErrors)
	}
}

func TestEnvDefaultClaudeDirUnsets(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
	out, _, code := run(apptest.NewDeps(t, testConfig, claude), "env", "two-claude")
	want := []string{"unset CLAUDE_CONFIG_DIR", "unset MODELPICK_KEY_FILE", "unset MODELPICK_KEY_LINE"}
	if code != 0 || strings.Join(lastLines(out, 3), "|") != strings.Join(want, "|") {
		t.Fatalf("exit %d; stdout\n%s", code, out)
	}
}

func TestEnvCodex(t *testing.T) {
	codex := apptest.NewFake("codex")
	codex.Readings["codex-main"] = apptest.ClaudeReading(1, 1)
	out, _, code := run(apptest.NewDeps(t, testConfig, apptest.NewFake("claude"), codex), "env", "codex-first")
	want := []string{"export CODEX_HOME='/home/u/.codex'", "unset MODELPICK_KEY_FILE", "unset MODELPICK_KEY_LINE"}
	if code != 0 || strings.Join(lastLines(out, 3), "|") != strings.Join(want, "|") {
		t.Fatalf("exit %d; stdout\n%s", code, out)
	}
}

func TestEnvOpencodeNullThinking(t *testing.T) {
	cfg := `{"version":1,
	"accounts":{"go-2":{"vendor":"opencode-go","key_file":"~/.ssh/k","key_line":2}},
	"profiles":{"p":[{"account":"go-2","model":"glm-5","thinking":null}]}}`
	goFake := apptest.NewFake("opencode-go")
	goFake.Readings["go-2"] = apptest.ClaudeReading(1, 1)
	out, _, code := run(apptest.NewDeps(t, cfg, goFake), "env", "p")
	if code != 0 {
		t.Fatalf("exit %d; stdout\n%s", code, out)
	}
	prev := -1
	for _, l := range []string{"unset MODELPICK_THINKING\n", "export MODELPICK_KEY_FILE='/home/u/.ssh/k'\n", "export MODELPICK_KEY_LINE='2'\n"} {
		i := strings.Index(out, l)
		if i <= prev {
			t.Fatalf("%q missing or out of order in\n%s", l, out)
		}
		prev = i
	}
}

func TestEnvFailed(t *testing.T) {
	out, code := envFailed(t)
	if code != 1 || out != wantEnvFailed {
		t.Fatalf("exit %d, want 1; stdout\n%s\nwant\n%s", code, out, wantEnvFailed)
	}
}

func TestEnvUsageErrors(t *testing.T) {
	for _, c := range []struct {
		args   []string
		reason string
	}{
		{[]string{"env"}, "env: missing <profile>"},
		{[]string{"env", "a", "b"}, `env: unexpected argument "b"`},
		{[]string{"env", "x", "--refrsh"}, `env: unknown flag "--refrsh"`},
	} {
		claude := apptest.NewFake("claude")
		out, errOut, code := run(apptest.NewDeps(t, testConfig, claude), c.args...)
		want := strings.Replace(wantEnvFailed, "echo 'modelpick: same-account: no choice available' >&2",
			"echo 'modelpick: "+c.reason+"' >&2", 1)
		if code != 1 || !strings.Contains(errOut, "usage:") || out != want || claude.TotalUsage() != 0 {
			t.Errorf("%q: exit %d stderr %q stdout\n%s\nwant\n%s", c.args, code, errOut, out, want)
		}
	}
}

func TestEnvNeverPrintsCredential(t *testing.T) {
	for _, s := range []string{wantEnvWithErrors, wantEnvFailed} {
		if strings.Contains(s, "fake: ") {
			t.Fatalf("golden contains a credential source: %s", s)
		}
	}
	out, _ := envWithErrors(t)
	out2, _ := envFailed(t)
	if strings.Contains(out, "fake: ") || strings.Contains(out2, "fake: ") {
		t.Fatalf("env printed a credential source:\n%s\n%s", out, out2)
	}
}

// shells returns the absolute paths of the sh, bash, zsh and dash found.
func shells(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	for _, n := range []string{"sh", "bash", "zsh", "dash"} {
		if p, err := exec.LookPath(n); err == nil {
			found[n] = p
		}
	}
	if len(found) == 0 {
		t.Fatal("no POSIX shell found")
	}
	return found
}

func runShell(t *testing.T, name, path, script string) string {
	t.Helper()
	args := []string{"-c", script}
	if name == "zsh" {
		args = []string{"-f", "-c", script}
	}
	cmd := exec.Command(path, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", name, err, out)
	}
	return string(out)
}

func TestShellQuoteRoundTrip(t *testing.T) {
	for name, path := range shells(t) {
		for _, v := range []string{"it's", "a b", "$HOME", "`id`", "a\nb", "''", `\`} {
			got := runShell(t, name, path, `X=`+shellQuote(v)+`; printf %s "$X"`)
			if got != v {
				t.Errorf("%s: %q round-tripped to %q", name, v, got)
			}
		}
	}
}

func TestEnvEvalInShell(t *testing.T) {
	okOut, _ := envWithErrors(t)
	failOut, _ := envFailed(t)
	dir := t.TempDir()
	okFile, failFile := filepath.Join(dir, "ok.sh"), filepath.Join(dir, "fail.sh")
	for f, s := range map[string]string{okFile: okOut, failFile: failOut} {
		if err := os.WriteFile(f, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, path := range shells(t) {
		got := runShell(t, name, path, `MODELPICK_KEY_FILE=old; export MODELPICK_KEY_FILE; eval "$(cat `+okFile+
			`)" 2>/dev/null; printf %s "$CLAUDE_CONFIG_DIR|$MODELPICK_ACCOUNT|${MODELPICK_KEY_FILE-unset}"`)
		if want := "/home/u/.claude-rec|claude-rec|unset"; got != want {
			t.Errorf("%s ok: got %q, want %q", name, got, want)
		}
		got = runShell(t, name, path, `MODELPICK_ACCOUNT=old; export MODELPICK_ACCOUNT; eval "$(cat `+failFile+
			`)" 2>/dev/null; echo "rc=$? acct=${MODELPICK_ACCOUNT-unset}"`)
		if want := "rc=1 acct=unset\n"; got != want {
			t.Errorf("%s failed: got %q, want %q", name, got, want)
		}
	}
}
