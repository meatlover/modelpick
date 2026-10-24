package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

const doctorConfig = `{
  "version": 1,
  "accounts": {
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" },
    "codex-main":  { "vendor": "codex", "codex_home": "~/.codex" },
    "codex-two":   { "vendor": "codex", "codex_home": "~/.codex2" }
  },
  "profiles": {
    "p": [
      { "account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [98] },
      { "account": "claude-main", "model": "claude-opus-5-5", "thinking": null }
    ],
    "q": [
      { "account": "codex-two", "model": "gpt-6.1-sol", "thinking": "high" }
    ]
  }
}`

func TestDoctorCleanSetup(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	stdout, stderr, code := run(apptest.NewDeps(t, doctorConfig, claude, codex), "doctor")
	if code != 0 || stdout != "no problems found\n" || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if claude.TotalUsage()+codex.TotalUsage() != 0 {
		t.Errorf("doctor called Usage")
	}
}

func TestDoctorMissingBinaryIsError(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	missing := adapter.Finding{Severity: adapter.SeverityError, Message: "codex not found on PATH"}
	for _, n := range []string{"codex-main", "codex-two"} {
		f := missing
		f.Account = n
		codex.Checks[n] = []adapter.Finding{f}
	}
	stdout, _, code := run(apptest.NewDeps(t, doctorConfig, claude, codex), "doctor")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	// Same message, different accounts: one grouped line.
	if !strings.Contains(stdout, "ERROR  codex-main, codex-two: codex not found on PATH\n") ||
		!strings.Contains(stdout, "1 error(s), 0 warning(s)\n") {
		t.Errorf("unexpected output:\n%s", stdout)
	}
}

func TestDoctorBadThinking(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	cfg := strings.Replace(doctorConfig, `"thinking": "high", "cap": [98]`, `"thinking": "ultra", "cap": [98]`, 1)
	stdout, _, code := run(apptest.NewDeps(t, cfg, claude, codex), "doctor")
	want := `ERROR  profile p, choice 1: thinking "ultra" not supported by gpt-6.1-sol (supported: low, high)` + "\n"
	if code != 1 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d, want line %q in:\n%s", code, want, stdout)
	}
}

func TestDoctorThinkingOnModelWithoutLevelsIsError(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	claude.ModelList["claude-main"] = []adapter.Model{{ID: "claude-opus-5-5"}}
	cfg := strings.Replace(doctorConfig, `"model": "claude-opus-5-5", "thinking": null`, `"model": "claude-opus-5-5", "thinking": "high"`, 1)
	stdout, _, code := run(apptest.NewDeps(t, cfg, claude, codex), "doctor")
	if code != 1 || !strings.Contains(stdout, `profile p, choice 2: thinking "high" not supported by claude-opus-5-5 (supported: none)`) {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestDoctorModelNotOffered(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	codex.ModelList["codex-two"] = []adapter.Model{{ID: "other"}}
	stdout, _, code := run(apptest.NewDeps(t, doctorConfig, claude, codex), "doctor")
	want := "WARN   profile q, choice 1: model gpt-6.1-sol not offered by codex-two\n"
	if code != 0 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d, want %q in:\n%s", code, want, stdout)
	}
}

const twoWindowState = `{"last_good": {"taken_at": "2026-10-22T12:00:00Z", "windows": [
 {"label":"session","length":18000000000000,"used_pct":10,"resets_at":"2026-10-25T12:00:00Z"},
 {"label":"weekly","length":604800000000000,"used_pct":10,"resets_at":"2026-10-25T12:00:00Z"}],
 "notes": ["unrecognised window group \"x\""]}}`

func TestDoctorCapMismatchWarnsOnly(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	d := apptest.NewDeps(t, doctorConfig, claude, codex)
	apptest.WriteStored(t, d, "codex-main", twoWindowState)
	stdout, _, code := run(d, "doctor")
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, stdout)
	}
	for _, want := range []string{
		"WARN   profile p, choice 1: codex-main: account reports 2 windows (5h, 7d), cap has 1 value; filled position 2 with default_cap 95\n",
		"WARN   codex-main: unrecognised window group \"x\"",
		"0 error(s), 2 warning(s)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestDoctorNoUncappedChoiceIsFine(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	cfg := strings.Replace(doctorConfig, `"model": "claude-opus-5-5", "thinking": null`, `"model": "claude-opus-5-5", "thinking": null, "cap": [90]`, 1)
	stdout, _, code := run(apptest.NewDeps(t, cfg, claude, codex), "doctor")
	if code != 0 || stdout != "no problems found\n" {
		t.Fatalf("exit %d, want a clean report (v1's no-uncapped warning is gone):\n%s", code, stdout)
	}
}

func TestDoctorConfigError(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	stdout, _, code := run(apptest.NewDeps(t, `{"version": 9}`, claude, codex), "doctor")
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if code != 1 || len(lines) != 2 || !strings.HasPrefix(lines[0], "ERROR  config: ") || lines[1] != "1 error(s), 0 warning(s)" {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	_, stderr, code := run(apptest.NewDeps(t, doctorConfig), "doctor", "bogus")
	if code != 1 || !strings.Contains(stderr, `doctor: unexpected argument "bogus"`) {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestDoctorSameFindingSameSubjectPrintsOnce(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	f := adapter.Finding{Severity: adapter.SeverityWarning, Account: "codex-main", Message: "token expired — open Codex with this account to refresh it"}
	codex.Checks["codex-main"] = []adapter.Finding{f, f}
	stdout, _, code := run(apptest.NewDeps(t, doctorConfig, claude, codex), "doctor")
	if code != 0 || strings.Count(stdout, "token expired") != 1 || !strings.Contains(stdout, "0 error(s), 1 warning(s)\n") {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestDoctorIdenticalCheckMessageTwoAccountsOneGroupedLine(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	msg := "token expired — open Claude Code with this account to refresh it"
	for _, n := range []string{"codex-main", "codex-two"} {
		codex.Checks[n] = []adapter.Finding{{Severity: adapter.SeverityWarning, Account: n, Message: msg}}
	}
	stdout, _, _ := run(apptest.NewDeps(t, doctorConfig, claude, codex), "doctor")
	if strings.Count(stdout, msg) != 1 || !strings.Contains(stdout, "WARN   codex-main, codex-two: "+msg+"\n") {
		t.Fatalf("want one grouped line:\n%s", stdout)
	}
}

func TestDoctorCannotListModelsWarns(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	delete(codex.ModelList, "codex-two") // fake returns an error
	stdout, _, code := run(apptest.NewDeps(t, doctorConfig, claude, codex), "doctor")
	want := "WARN   codex-two: cannot list models, thinking values not checked: fake: no models\n"
	if code != 0 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d, want %q in:\n%s", code, want, stdout)
	}
}

func TestDoctorCannotReadStoredUsageWarns(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	d := apptest.NewDeps(t, doctorConfig, claude, codex)
	apptest.WriteStored(t, d, "codex-main", "{not json")
	stdout, _, code := run(d, "doctor")
	if code != 0 || !strings.Contains(stdout, "WARN   codex-main: cannot read stored usage: ") {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestDoctorAllArgument(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	codex.Errs["codex-main"] = errors.New("dial tcp: connection refused")
	d := apptest.NewDeps(t, doctorConfig, claude, codex)
	for _, n := range []string{"claude-main", "codex-two"} {
		apptest.WriteStored(t, d, n, `{"last_good":{"taken_at":"2026-10-22T11:55:00Z","windows":[]}}`)
	}
	apptest.WriteStored(t, d, "codex-main", `{"last_good":{"taken_at":"2026-10-22T11:00:00Z","windows":[]}}`)
	stdout, stderr, code := run(d, "doctor", "all")
	if got := claude.TotalUsage() + codex.TotalUsage(); got != 1 || codex.Calls["codex-main"] != 1 {
		t.Errorf("usage calls = %d (codex-main %d), want 1 for codex-main", got, codex.Calls["codex-main"])
	}
	want := "WARN   codex-main: last usage reading failed: dial tcp: connection refused\n"
	if code != 0 || !strings.Contains(stdout, want) || !strings.HasSuffix(stdout, "0 error(s), 2 warning(s)\n") || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q, want a line %q", code, stdout, stderr, want)
	}
}

func TestUsageTextListsDoctorAll(t *testing.T) {
	_, stderr, code := run(apptest.NewDeps(t, doctorConfig))
	if code != 1 || !strings.Contains(stderr, "\n  modelpick doctor [all]\n") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}
