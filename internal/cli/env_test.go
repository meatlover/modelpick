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

// wantEnvOpencode ends with the key reference: nothing may unset it after.
const wantEnvOpencode = `export MODELPICK_STATUS='ok'
export MODELPICK_PROFILE='p'
export MODELPICK_VENDOR='opencode-go'
export MODELPICK_ACCOUNT='go-2'
export MODELPICK_MODEL='glm-5'
unset MODELPICK_THINKING
export MODELPICK_KEY_FILE='/home/u/.ssh/k'
export MODELPICK_KEY_LINE='2'
`

func envOpencode(t *testing.T) (string, int) {
	t.Helper()
	cfg := `{"version":1,
	"accounts":{"go-2":{"vendor":"opencode-go","key_file":"~/.ssh/k","key_line":2}},
	"profiles":{"p":[{"account":"go-2","model":"glm-5","thinking":null}]}}`
	goFake := apptest.NewFake("opencode-go")
	goFake.Readings["go-2"] = apptest.ClaudeReading(1, 1)
	out, _, code := run(apptest.NewDeps(t, cfg, goFake), "env", "p")
	return out, code
}

func TestEnvOpencodeNullThinking(t *testing.T) {
	out, code := envOpencode(t)
	if code != 0 || out != wantEnvOpencode {
		t.Fatalf("exit %d, want 0; stdout\n%s\nwant\n%s", code, out, wantEnvOpencode)
	}
}

// wantEnvQuotedDir is spec §10's quoting case: a config_dir with a quote
// and a space.
const wantEnvQuotedDir = `export MODELPICK_STATUS='ok'
export MODELPICK_PROFILE='q'
export MODELPICK_VENDOR='claude'
export MODELPICK_ACCOUNT='odd'
export MODELPICK_MODEL='claude-sonnet-5-5'
unset MODELPICK_THINKING
export CLAUDE_CONFIG_DIR='/home/u/it'\''s here'
unset MODELPICK_KEY_FILE
unset MODELPICK_KEY_LINE
`

func envQuotedDir(t *testing.T) (string, int) {
	t.Helper()
	cfg := `{"version":1,
	"accounts":{"odd":{"vendor":"claude","config_dir":"~/it's here"}},
	"profiles":{"q":[{"account":"odd","model":"claude-sonnet-5-5","thinking":null}]}}`
	claude := apptest.NewFake("claude")
	claude.Readings["odd"] = apptest.ClaudeReading(1, 1)
	out, _, code := run(apptest.NewDeps(t, cfg, claude), "env", "q")
	return out, code
}

func TestEnvQuotedConfigDir(t *testing.T) {
	out, code := envQuotedDir(t)
	if code != 0 || out != wantEnvQuotedDir {
		t.Fatalf("exit %d, want 0; stdout\n%s\nwant\n%s", code, out, wantEnvQuotedDir)
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
		{[]string{"env", ""}, "env: missing <profile>"},
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

// TestEnvEvalInShell evals each env output in every shell found, after
// pre has left a stale value, and prints the exit status and the variables.
func TestEnvEvalInShell(t *testing.T) {
	okOut, _ := envWithErrors(t)
	failOut, _ := envFailed(t)
	goOut, _ := envOpencode(t)
	quotedOut, _ := envQuotedDir(t)
	cases := []struct{ name, script, pre, show, want string }{
		{"ok", okOut, "MODELPICK_KEY_FILE=old", "$CLAUDE_CONFIG_DIR|$MODELPICK_ACCOUNT|${MODELPICK_KEY_FILE-unset}",
			"rc=0 /home/u/.claude-rec|claude-rec|unset"},
		{"failed", failOut, "MODELPICK_ACCOUNT=old", "${MODELPICK_ACCOUNT-unset}", "rc=1 unset"},
		{"opencode", goOut, "MODELPICK_KEY_FILE=old", "$MODELPICK_KEY_FILE|$MODELPICK_KEY_LINE", "rc=0 /home/u/.ssh/k|2"},
		{"quoted", quotedOut, "CLAUDE_CONFIG_DIR=old", "$CLAUDE_CONFIG_DIR", "rc=0 /home/u/it's here"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		f := filepath.Join(dir, c.name+".sh")
		if err := os.WriteFile(f, []byte(c.script), 0o600); err != nil {
			t.Fatal(err)
		}
		variable := c.pre[:strings.Index(c.pre, "=")]
		script := c.pre + "; export " + variable + `; eval "$(cat ` + f + `)" 2>/dev/null; rc=$?; printf %s "rc=$rc ` + c.show + `"`
		for name, path := range shells(t) {
			if got := runShell(t, name, path, script); got != c.want {
				t.Errorf("%s %s: got %q, want %q", name, c.name, got, c.want)
			}
		}
	}
}

// TestEnvFailedWithErrors: the error echoes come first, then the unsets,
// the reason and false (spec §4 order, steps 1 and 2).
func TestEnvFailedWithErrors(t *testing.T) {
	cfg := `{"version":1,
	"accounts":{"codex-main":{"vendor":"codex","codex_home":"~/.codex"}},
	"profiles":{"only-codex":[{"account":"codex-main","model":"gpt-6.1-sol","thinking":"high","cap":[98]}]}}`
	codex := apptest.NewFake("codex")
	codex.Errs["codex-main"] = adapter.ErrUnauthorized
	out, _, code := run(apptest.NewDeps(t, cfg, codex), "env", "only-codex")
	want := "echo 'modelpick: codex-main: usage endpoint returned HTTP 401; log in to Codex with this account' >&2\n" +
		strings.Replace(wantEnvFailed, "same-account: no choice available", "only-codex: no choice available", 1)
	if code != 1 || out != want {
		t.Fatalf("exit %d, want 1; stdout\n%s\nwant\n%s", code, out, want)
	}
}
