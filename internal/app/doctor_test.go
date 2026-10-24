package app_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/core"
)

const (
	codexA  = `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"high"`
	claudeA = `{"account":"claude-main","model":"claude-opus-5-5","thinking":null`
	mon     = `{"days":["mon"],"utc":["00:00-12:00"]}`
	monWide = `{"days":["mon","tue"],"utc":["00:00-24:00"]}`
	tue     = `{"days":["tue"],"utc":["00:00-12:00"]}`
)

// docConfig builds a config with codex-main and claude-main (both healthy
// under DoctorFakes), the given schedules object body and profile p.
func docConfig(schedules string, choices ...string) string {
	return `{"version":1,"settings":{"default_cap":95},"accounts":{
 "claude-main":{"vendor":"claude","config_dir":"~/.claude"},
 "codex-main":{"vendor":"codex","codex_home":"~/.codex"}},
 "schedules":{` + schedules + `},
 "profiles":{"p":[` + strings.Join(choices, ",") + `]}}`
}

func runDoctor(t *testing.T, cfgJSON string) []app.Finding {
	t.Helper()
	claude, codex := apptest.DoctorFakes()
	return runDoctorWith(t, cfgJSON, claude, codex, &app.CheckCache{})
}

func runDoctorWith(t *testing.T, cfgJSON string, claude, codex *apptest.Fake, cache *app.CheckCache) []app.Finding {
	t.Helper()
	d := apptest.NewDeps(t, cfgJSON, claude, codex)
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	return app.Doctor(cfg, d, cache)
}

// choice closes a choice object, adding extra fields when there are any.
func choice(base, extra string) string {
	if extra == "" {
		return base + "}"
	}
	return base + "," + extra + "}"
}

func never(i, j string) app.Finding {
	return app.Finding{Severity: "warning", Subject: "profile p, choice " + i,
		Message: "never picked, choice " + j + " always wins first"}
}

func TestDoctorStable(t *testing.T) {
	cfg := docConfig(`"s":{"days":[],"utc":[]}`, codexA+`}`, codexA+`,"cap":[90]}`,
		strings.Replace(claudeA, "null", `"nope"`, 1)+`}`)
	a, b := runDoctor(t, cfg), runDoctor(t, cfg)
	if len(a) < 3 || !reflect.DeepEqual(a, b) {
		t.Fatalf("runs differ or too few findings:\n%v\n%v", a, b)
	}
	for i := 1; i < len(a); i++ {
		x, y := a[i-1], a[i]
		if x.Severity == "warning" && y.Severity == "error" {
			t.Errorf("errors must sort before warnings: %v", a)
		}
	}
}

func TestNoUncappedWarningGone(t *testing.T) {
	got := runDoctor(t, docConfig("", codexA+`,"cap":[90]}`, claudeA+`,"cap":[90,90]}`))
	if len(got) != 0 {
		t.Fatalf("want no findings for an all-capped profile, got %v", got)
	}
}

func TestScheduleEmptyWarning(t *testing.T) {
	got := runDoctor(t, docConfig(`"s":{"days":[],"utc":[]},"t":`+mon, codexA+`,"cap":[90]}`))
	want := []app.Finding{{Severity: "warning", Subject: "schedule s", Message: "empty"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNeverPickedUncapped(t *testing.T) {
	sch := `"mon":` + mon + `,"tue":` + tue + `,"wide":` + monWide
	cases := []struct {
		name    string
		choices []string
		want    []app.Finding
	}{
		{"uncapped first", []string{codexA + `}`, claudeA + `}`}, []app.Finding{never("2", "1")}},
		{"capped first", []string{codexA + `,"cap":[90]}`, claudeA + `}`}, nil},
		{"skip leaves minutes the later may run",
			[]string{codexA + `,"skip_during":["mon"]}`, claudeA + `}`}, nil},
		{"later is skipped where the first is not",
			[]string{codexA + `}`, claudeA + `,"skip_during":["mon"]}`}, []app.Finding{never("2", "1")}},
		{"disjoint allow windows",
			[]string{codexA + `,"skip_during":["tue"]}`, claudeA + `,"allow_during":["tue"]}`}, nil},
		{"first skips what the later allows is not enough",
			[]string{codexA + `,"skip_during":["mon"]}`, claudeA + `,"allow_during":["tue"]}`}, []app.Finding{never("2", "1")}},
		{"third blames the lowest j",
			[]string{codexA + `}`, claudeA + `}`, codexA + `}`},
			[]app.Finding{never("2", "1"), never("3", "1")}},
	}
	for _, c := range cases {
		got := onlyNever(runDoctor(t, docConfig(sch, c.choices...)))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// onlyNever keeps the never-picked findings, so cases stay independent of the
// duplicate-model findings of other checks.
func onlyNever(fs []app.Finding) []app.Finding {
	var out []app.Finding
	for _, f := range fs {
		if strings.HasPrefix(f.Message, "never picked") {
			out = append(out, f)
		}
	}
	return out
}

func TestNeverPickedSameModel(t *testing.T) {
	cases := []struct {
		name string
		j, i string
		want bool
	}{
		{"looser first", `"cap":[90]`, `"cap":[80]`, true},
		{"equal duplicates", `"cap":[90]`, `"cap":[90]`, true},
		{"tighter first", `"cap":[80]`, `"cap":[90]`, false},
		{"pace by N, larger first", `"cap":["pace+10"]`, `"cap":["pace+5"]`, true},
		{"pace by N, smaller first", `"cap":["pace+5"]`, `"cap":["pace+10"]`, false},
		{"fixed vs pace not compared", `"cap":[99]`, `"cap":["pace+5"]`, false},
		{"pace vs fixed not compared", `"cap":["pace+50"]`, `"cap":[10]`, false},
		{"mix at a later position", `"cap":[90,99]`, `"cap":[80,"pace+5"]`, false},
		{"default resolves to default_cap", `"cap":["default"]`, `"cap":[90]`, true},
		{"missing position is default_cap", `"cap":[90]`, `"cap":[90,96]`, false},
		{"missing position, looser", `"cap":[90]`, `"cap":[90,90]`, true},
		{"later uncapped", `"cap":[90]`, ``, false},
	}
	for _, c := range cases {
		got := onlyNever(runDoctor(t, docConfig("", choice(codexA, c.j), choice(codexA, c.i))))
		if (len(got) == 1) != c.want || (c.want && !reflect.DeepEqual(got, []app.Finding{never("2", "1")})) {
			t.Errorf("%s: got %v, want warning=%v", c.name, got, c.want)
		}
	}
}

func TestNeverPickedSameModelNeedsSameAccountAndModel(t *testing.T) {
	got := onlyNever(runDoctor(t, docConfig("", codexA+`,"cap":[90]}`, claudeA+`,"cap":[80]}`)))
	if len(got) != 0 {
		t.Fatalf("different accounts: got %v", got)
	}
}

func TestNeverPickedAllowDuring(t *testing.T) {
	sch := `"mon":` + mon + `,"tue":` + tue + `,"wide":` + monWide
	cases := []struct {
		name    string
		j, i    string
		warning bool
	}{
		{"later allow inside first allow", `"allow_during":["wide"]`, `"allow_during":["mon"]`, true},
		{"later allow wider than first", `"allow_during":["mon"]`, `"allow_during":["wide"]`, false},
		{"first narrow, later unrestricted", `"allow_during":["mon"]`, ``, false},
		{"first unrestricted, later allow", ``, `"allow_during":["mon"]`, true},
		{"two allows union", `"allow_during":["mon","tue"]`, `"allow_during":["wide"]`, false},
		{"union covers later", `"allow_during":["mon","tue","wide"]`, `"allow_during":["tue"]`, true},
	}
	for _, c := range cases {
		got := onlyNever(runDoctor(t, docConfig(sch, choice(codexA, c.j), choice(claudeA, c.i))))
		if (len(got) == 1) != c.warning {
			t.Errorf("%s: got %v, want warning=%v", c.name, got, c.warning)
		}
	}
}

func TestCheckCacheReused(t *testing.T) {
	cfg := docConfig("", codexA+`,"cap":[90]}`, claudeA+`,"cap":[90]}`)
	claude, codex := apptest.DoctorFakes()
	cache := &app.CheckCache{}
	runDoctorWith(t, cfg, claude, codex, cache)
	runDoctorWith(t, cfg, claude, codex, cache)
	if codex.CheckCalls["codex-main"] != 1 || claude.CheckCalls["claude-main"] != 1 {
		t.Fatalf("Check calls after two runs: codex %d, claude %d, want 1 each",
			codex.CheckCalls["codex-main"], claude.CheckCalls["claude-main"])
	}
	cache.Forget("codex-main")
	runDoctorWith(t, cfg, claude, codex, cache)
	if codex.CheckCalls["codex-main"] != 2 || claude.CheckCalls["claude-main"] != 1 {
		t.Fatalf("after Forget: codex %d (want 2), claude %d (want 1)",
			codex.CheckCalls["codex-main"], claude.CheckCalls["claude-main"])
	}
}

func TestDoctorSubjects(t *testing.T) {
	cfg := docConfig("", codexA+`,"cap":[90]}`, strings.Replace(claudeA, "null", `"ultra"`, 1)+`}`)
	got := runDoctor(t, cfg)
	if len(got) != 1 || got[0].Severity != "error" || got[0].Subject != "profile p, choice 2" ||
		!strings.HasPrefix(got[0].Message, `thinking "ultra" not supported by claude-opus-5-5`) {
		t.Fatalf("got %v", got)
	}
}

func TestConfigFindingForPaceBelowMinimum(t *testing.T) {
	d := apptest.NewDeps(t, docConfig("", codexA+`,"cap":["pace+4"]}`))
	_, err := app.LoadConfig(d)
	if err == nil {
		t.Fatal("pace+4 must not parse")
	}
	f := app.ConfigFinding(err)
	if f.Severity != "error" || f.Subject != "config" || f.Message != err.Error() {
		t.Fatalf("got %+v", f)
	}
	_ = core.MinPaceMargin
}

// goConfig has n opencode-go accounts go-1..go-n and one profile using go-1.
func goConfig(n int) string {
	var accts []string
	for i := 1; i <= n; i++ {
		accts = append(accts, fmt.Sprintf(`"go-%d":{"vendor":"opencode-go","key_file":"~/.ssh/k","key_line":%d}`, i, i))
	}
	return `{"version":1,"accounts":{` + strings.Join(accts, ",") + `},
 "profiles":{"p":[{"account":"go-1","model":"m","thinking":null}]}}`
}

func goFindings(t *testing.T, n int, checks map[string]string) []app.Finding {
	t.Helper()
	fake := apptest.NewFake("opencode-go")
	for name, msg := range checks {
		fake.Checks[name] = []adapter.Finding{{Severity: adapter.SeverityError, Account: name, Message: msg}}
	}
	d := apptest.NewDeps(t, goConfig(n), fake)
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	return app.Doctor(cfg, d, nil)
}

func TestDoctorGroupsMissingTool(t *testing.T) {
	msg := "opencode not found on PATH"
	got := goFindings(t, 3, map[string]string{"go-1": msg, "go-2": msg, "go-3": msg})
	want := []app.Finding{{Severity: "error", Subject: "go-1, go-2, go-3", Message: msg,
		Accounts: []string{"go-1", "go-2", "go-3"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}

func TestDoctorKeepsListingWarningWhenToolPresent(t *testing.T) {
	got := goFindings(t, 2, nil)
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want 2 listing warnings", got)
	}
	for i, f := range got {
		if f.Accounts != nil || f.Subject != fmt.Sprintf("go-%d", i+1) ||
			!strings.HasPrefix(f.Message, "cannot list models, thinking values not checked: ") {
			t.Errorf("finding %d = %+v", i, f)
		}
	}
}

func TestDoctorDoesNotGroupDifferentMessages(t *testing.T) {
	got := goFindings(t, 2, map[string]string{"go-1": "no credential: /a", "go-2": "no credential: /b"})
	want := []app.Finding{
		{Severity: "error", Subject: "go-1", Message: "no credential: /a"},
		{Severity: "error", Subject: "go-2", Message: "no credential: /b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}

func TestDoctorSingleAccountNotGrouped(t *testing.T) {
	got := goFindings(t, 1, map[string]string{"go-1": "opencode not found on PATH"})
	want := []app.Finding{{Severity: "error", Subject: "go-1", Message: "opencode not found on PATH"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}

// storedDoctor runs Doctor on the healthy docConfig with codex-main's state
// file set to stored (the fake clock is 2026-10-22T12:00:00Z).
func storedDoctor(t *testing.T, stored string, claude, codex *apptest.Fake) []app.Finding {
	t.Helper()
	d := apptest.NewDeps(t, docConfig("", codexA+`}`), claude, codex)
	apptest.WriteStored(t, d, "codex-main", stored)
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	return app.Doctor(cfg, d, nil)
}

const (
	reading1100 = `"last_good":{"taken_at":"2026-10-22T11:00:00Z","windows":[]}`
	reading1155 = `"last_good":{"taken_at":"2026-10-22T11:55:00Z","windows":[]}`
	failed503   = `"last_attempt":"2026-10-22T11:50:00Z","last_error":"usage endpoint returned HTTP 503"`
	failedWarn  = "last usage reading failed: usage endpoint returned HTTP 503"
)

func TestDoctorReportsLastFailedReading(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	got := storedDoctor(t, `{`+reading1100+`,`+failed503+`}`, claude, codex)
	want := []app.Finding{{Severity: "warning", Subject: "codex-main", Message: failedWarn}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}

func TestDoctorFailedReadingClearsAfterSuccess(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	got := storedDoctor(t, `{`+reading1155+`,`+failed503+`}`, claude, codex)
	if len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestDoctorRateLimitedNotReported(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	got := storedDoctor(t, `{`+reading1100+`,`+failed503+`,"retry_until":"2026-10-22T12:30:00Z"}`, claude, codex)
	if len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
}

func TestDoctorFailedReadingSkippedWhenCheckReportsCause(t *testing.T) {
	claude, codex := apptest.DoctorFakes()
	codex.Checks["codex-main"] = []adapter.Finding{{Severity: adapter.SeverityError, Message: "no credential: /x"}}
	got := storedDoctor(t, `{`+reading1100+`,`+failed503+`}`, claude, codex)
	want := []app.Finding{{Severity: "error", Subject: "codex-main", Message: "no credential: /x"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}
