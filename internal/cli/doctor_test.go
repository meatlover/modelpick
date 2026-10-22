package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
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

// doctorFakes: every model listed, every account healthy.
func doctorFakes() (*fakeAdapter, *fakeAdapter) {
	claude := newFake("claude")
	claude.models["claude-main"] = []adapter.Model{{ID: "claude-opus-5-5", Thinking: []string{"low", "high"}}}
	codex := newFake("codex")
	for _, n := range []string{"codex-main", "codex-two"} {
		codex.models[n] = []adapter.Model{{ID: "gpt-6.1-sol", Thinking: []string{"low", "high"}}}
	}
	return claude, codex
}

func TestDoctorCleanSetup(t *testing.T) {
	claude, codex := doctorFakes()
	stdout, stderr, code := run(testDeps(t, doctorConfig, claude, codex), "doctor")
	if code != 0 || stdout != "no problems found\n" || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if claude.totalUsage()+codex.totalUsage() != 0 {
		t.Errorf("doctor called Usage")
	}
}

func TestDoctorMissingBinaryIsError(t *testing.T) {
	claude, codex := doctorFakes()
	missing := adapter.Finding{Severity: adapter.SeverityError, Message: "codex not found on PATH"}
	for _, n := range []string{"codex-main", "codex-two"} {
		f := missing
		f.Account = n
		codex.checks[n] = []adapter.Finding{f}
	}
	stdout, _, code := run(testDeps(t, doctorConfig, claude, codex), "doctor")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got := strings.Count(stdout, "codex not found on PATH"); got != 1 {
		t.Errorf("binary finding printed %d times, want 1:\n%s", got, stdout)
	}
	if !strings.Contains(stdout, "ERROR  codex-main: codex not found on PATH\n") ||
		!strings.Contains(stdout, "1 error(s), 0 warning(s)\n") {
		t.Errorf("unexpected output:\n%s", stdout)
	}
}

func TestDoctorBadThinking(t *testing.T) {
	claude, codex := doctorFakes()
	cfg := strings.Replace(doctorConfig, `"thinking": "high", "cap": [98]`, `"thinking": "ultra", "cap": [98]`, 1)
	stdout, _, code := run(testDeps(t, cfg, claude, codex), "doctor")
	want := `ERROR  profile p: profiles.p[0]: thinking "ultra" not supported by gpt-6.1-sol (supported: low, high)` + "\n"
	if code != 1 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d, want line %q in:\n%s", code, want, stdout)
	}
}

func TestDoctorThinkingOnModelWithoutLevelsIsError(t *testing.T) {
	claude, codex := doctorFakes()
	claude.models["claude-main"] = []adapter.Model{{ID: "claude-opus-5-5"}}
	cfg := strings.Replace(doctorConfig, `"model": "claude-opus-5-5", "thinking": null`, `"model": "claude-opus-5-5", "thinking": "high"`, 1)
	stdout, _, code := run(testDeps(t, cfg, claude, codex), "doctor")
	if code != 1 || !strings.Contains(stdout, `profiles.p[1]: thinking "high" not supported by claude-opus-5-5 (supported: none)`) {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestDoctorModelNotOffered(t *testing.T) {
	claude, codex := doctorFakes()
	codex.models["codex-two"] = []adapter.Model{{ID: "other"}}
	stdout, _, code := run(testDeps(t, doctorConfig, claude, codex), "doctor")
	want := "WARN   profile q: profiles.q[0]: model gpt-6.1-sol not offered by codex-two\n"
	if code != 0 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d, want %q in:\n%s", code, want, stdout)
	}
}

// writeStored stores a reading where doctor's Peek will find it.
func writeStored(t *testing.T, d Deps, account, json string) {
	t.Helper()
	cfg, err := loadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	st := newStore(d, cfg)
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir, account+".json"), []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
}

const twoWindowState = `{"last_good": {"taken_at": "2026-10-22T12:00:00Z", "windows": [
 {"label":"session","length":18000000000000,"used_pct":10,"resets_at":"2026-10-25T12:00:00Z"},
 {"label":"weekly","length":604800000000000,"used_pct":10,"resets_at":"2026-10-25T12:00:00Z"}],
 "notes": ["unrecognised window group \"x\""]}}`

func TestDoctorCapMismatchWarnsOnly(t *testing.T) {
	claude, codex := doctorFakes()
	d := testDeps(t, doctorConfig, claude, codex)
	writeStored(t, d, "codex-main", twoWindowState)
	stdout, _, code := run(d, "doctor")
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, stdout)
	}
	for _, want := range []string{
		"WARN   codex-main: codex-main: account reports 2 windows (5h, 7d), cap has 1 value; filled position 2 with default_cap",
		"WARN   codex-main: unrecognised window group \"x\"",
		"0 error(s), 2 warning(s)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestDoctorNoUncappedChoiceWarns(t *testing.T) {
	claude, codex := doctorFakes()
	cfg := strings.Replace(doctorConfig, `"model": "claude-opus-5-5", "thinking": null`, `"model": "claude-opus-5-5", "thinking": null, "cap": [90]`, 1)
	stdout, _, code := run(testDeps(t, cfg, claude, codex), "doctor")
	want := "WARN   profile p: profile p has no uncapped choice; pick can return failed\n"
	if code != 0 || !strings.Contains(stdout, want) {
		t.Fatalf("exit %d, want %q in:\n%s", code, want, stdout)
	}
}

func TestDoctorConfigError(t *testing.T) {
	claude, codex := doctorFakes()
	stdout, _, code := run(testDeps(t, `{"version": 9}`, claude, codex), "doctor")
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if code != 1 || len(lines) != 2 || !strings.HasPrefix(lines[0], "ERROR  config: ") || lines[1] != "1 error(s), 0 warning(s)" {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	_, stderr, code := run(testDeps(t, doctorConfig), "doctor", "x")
	if code != 1 || !strings.Contains(stderr, "doctor: unexpected argument") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}
