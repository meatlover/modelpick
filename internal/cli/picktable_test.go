package cli

import (
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

const pickAllNotice = "For JSON, name a profile: modelpick pick <profile>"

const wantPickAll = `PROFILE       VENDOR  ACCOUNT     MODEL              THINKING
codex-first   claude  claude-rec  claude-sonnet-5-5  medium
same-account  no choice available
two-claude    claude  claude-rec  claude-sonnet-5-5  medium

codex-main: usage endpoint returned HTTP 401; log in to Codex with this account

` + pickAllNotice + "\n"

func TestPickAllTable(t *testing.T) {
	claude, codex := apptest.NewFake("claude"), apptest.NewFake("codex")
	claude.Readings["claude-main"] = apptest.ClaudeReading(95, 10)
	codex.Errs["codex-main"] = adapter.ErrUnauthorized
	stdout, stderr, code := run(apptest.NewDeps(t, testConfig, claude, codex), "pick")
	if code != 1 || stdout != wantPickAll {
		t.Errorf("exit %d stderr %q stdout:\n%s\nwant exit 1 and:\n%s", code, stderr, stdout, wantPickAll)
	}
	if claude.Calls["claude-main"] != 1 || codex.Calls["codex-main"] != 1 {
		t.Errorf("reads claude-main %d codex-main %d, want 1 each", claude.Calls["claude-main"], codex.Calls["codex-main"])
	}
}

func TestPickAllExitCodes(t *testing.T) {
	for _, c := range []struct {
		name     string
		codexErr error
		wantCode int
		wantErr  bool
	}{
		{"all readable", nil, 0, false},
		{"errors, no failure", adapter.ErrUnauthorized, 2, true},
	} {
		claude, codex := apptest.NewFake("claude"), apptest.NewFake("codex")
		claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
		claude.Readings["claude-rec"] = apptest.ClaudeReading(10, 10)
		codex.Readings["codex-main"] = apptest.ClaudeReading(10, 10)
		if c.codexErr != nil {
			delete(codex.Readings, "codex-main")
			codex.Errs["codex-main"] = c.codexErr
		}
		stdout, stderr, code := run(apptest.NewDeps(t, testConfig, claude, codex), "pick")
		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if code != c.wantCode || lines[len(lines)-1] != pickAllNotice || lines[len(lines)-2] != "" {
			t.Errorf("%s: exit %d stderr %q stdout:\n%s", c.name, code, stderr, stdout)
		}
		if n := strings.Count(stdout, "codex-main: "); (n == 1) != c.wantErr || n > 1 {
			t.Errorf("%s: error line count %d:\n%s", c.name, n, stdout)
		}
	}
}

func TestPickAllNullThinking(t *testing.T) {
	cfg := `{"version": 1,
  "accounts": {"claude-main": {"vendor": "claude", "config_dir": "~/.claude"}},
  "profiles": {"solo": [{"account": "claude-main", "model": "claude-sonnet-5-5", "thinking": null}]}}`
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
	stdout, stderr, code := run(apptest.NewDeps(t, cfg, claude), "pick")
	if code != 0 || strings.Join(strings.Fields(strings.Split(stdout, "\n")[1]), " ") != "solo claude claude-main claude-sonnet-5-5 -" {
		t.Errorf("exit %d stderr %q stdout:\n%s", code, stderr, stdout)
	}
}

func TestPickAllRefresh(t *testing.T) {
	claude, codex := apptest.NewFake("claude"), apptest.NewFake("codex")
	claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
	codex.Readings["codex-main"] = apptest.ClaudeReading(10, 10)
	d := apptest.NewDeps(t, testConfig, claude, codex)
	run(d, "pick")
	before := claude.Calls["claude-main"]
	beforeCodex := codex.Calls["codex-main"]
	if before == 0 || beforeCodex == 0 {
		t.Fatalf("first pick read claude-main %d codex-main %d", before, beforeCodex)
	}
	run(d, "pick", "--refresh")
	if claude.Calls["claude-main"] != before+1 || codex.Calls["codex-main"] != beforeCodex+1 {
		t.Errorf("after --refresh claude-main %d (was %d) codex-main %d (was %d); want +1 each",
			claude.Calls["claude-main"], before, codex.Calls["codex-main"], beforeCodex)
	}
}

func TestPickAllConfigError(t *testing.T) {
	claude := apptest.NewFake("claude")
	d := apptest.NewDeps(t, `{"version": 2}`, claude)
	stdout, stderr, code := run(d, "pick")
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "modelpick: ") || !strings.Contains(stderr, "version") ||
		strings.Count(stderr, "\n") != 1 || claude.TotalUsage() != 0 {
		t.Errorf("exit %d stdout %q stderr %q usage %d", code, stdout, stderr, claude.TotalUsage())
	}
}

func TestPickAllUnknownFlag(t *testing.T) {
	stdout, stderr, code := run(apptest.NewDeps(t, testConfig, apptest.NewFake("claude")), "pick", "--x")
	p := parsePick(t, stdout)
	if code != 1 || p.Status != "failed" || p.Reason == nil || *p.Reason != `pick: unknown flag "--x"` ||
		!strings.Contains(stderr, "usage:") {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestPickAllDedupesErrors(t *testing.T) {
	cfg := `{"version": 1,
  "accounts": {"codex-main": {"vendor": "codex", "codex_home": "~/.codex"},
               "claude-main": {"vendor": "claude", "config_dir": "~/.claude"}},
  "profiles": {
    "a": [{"account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [98]},
          {"account": "claude-main", "model": "claude-sonnet-5-5", "thinking": null}],
    "b": [{"account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [98]},
          {"account": "claude-main", "model": "claude-sonnet-5-5", "thinking": null}]}}`
	claude, codex := apptest.NewFake("claude"), apptest.NewFake("codex")
	claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
	codex.Errs["codex-main"] = adapter.ErrUnauthorized
	stdout, _, code := run(apptest.NewDeps(t, cfg, claude, codex), "pick")
	if n := strings.Count(stdout, "codex-main: "); code != 2 || n != 1 {
		t.Errorf("exit %d, error lines %d, want 2 and 1:\n%s", code, n, stdout)
	}
}
