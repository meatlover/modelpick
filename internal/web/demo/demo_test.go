package demo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/web"
)

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func newDemo() *Demo { return New(func() time.Time { return t0 }) }

func apply(t *testing.T, d *Demo, base, op string, args any) (web.OpResult, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return d.Apply(web.OpRequest{Base: base, Op: op, Args: raw})
}

// must applies op against the current sha and fails the test on error.
func must(t *testing.T, d *Demo, op string, args any) web.OpResult {
	t.Helper()
	res, err := apply(t, d, d.State().SHA256, op, args)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return res
}

func refusal(t *testing.T, d *Demo, op string, args any) web.Refused {
	t.Helper()
	_, err := apply(t, d, d.State().SHA256, op, args)
	var r web.Refused
	if !errors.As(err, &r) {
		t.Fatalf("%s: want Refused value, got %#v", op, err)
	}
	return r
}

func choiceJSON(t *testing.T, d *Demo, profile string, i int) json.RawMessage {
	t.Helper()
	for _, p := range d.State().Profiles {
		if p.Name == profile {
			raw, _ := json.Marshal(p.Choices[i])
			return raw
		}
	}
	t.Fatalf("no profile %s", profile)
	return nil
}

func profileOf(t *testing.T, d *Demo, name string) web.Profile {
	t.Helper()
	for _, p := range d.State().Profiles {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no profile %s", name)
	return web.Profile{}
}

func TestFixtureCounts(t *testing.T) {
	s := newDemo().State()
	if len(s.Accounts) != 6 || len(s.Profiles) != 2 || len(s.Schedules) != 1 {
		t.Fatalf("got %d accounts, %d profiles, %d schedules", len(s.Accounts), len(s.Profiles), len(s.Schedules))
	}
	if s.DefaultCap != 95 || len(s.Profiles[0].Choices) != 4 {
		t.Fatalf("default cap %v, choices %d", s.DefaultCap, len(s.Profiles[0].Choices))
	}
}

func TestStaleBaseConflicts(t *testing.T) {
	d := newDemo()
	_, err := apply(t, d, "stale", "profile.add", map[string]any{"name": "x"})
	var c web.Conflict
	if !errors.As(err, &c) || c.SHA256 != d.State().SHA256 {
		t.Fatalf("want Conflict with current sha, got %#v", err)
	}
}

func TestChoiceSetRefusals(t *testing.T) {
	d := newDemo()
	set := func(field string, value any) map[string]any {
		return map[string]any{"profile": "coder-difficult", "index": 0, "field": field,
			"value": value, "expect": choiceJSON(t, d, "coder-difficult", 0)}
	}
	cases := []struct {
		name  string
		field string
		value any
		want  string
	}{
		{"range", "cap", []any{120}, "Profile coder-difficult, choice 1, 5h cap: 120 is outside 0–100%"},
		// Owner ruling 2026-10-23 (round 6): pace N is 5–50; pace+0 is now
		// refused (owner spec change).
		{"pace over 50", "cap", []any{90, "pace+51"}, "Profile coder-difficult, choice 1, 7d cap: pace+51 is outside pace+5–pace+50"},
		{"pace below 5", "cap", []any{"pace+2"}, "Profile coder-difficult, choice 1, 5h cap: pace+2 is outside pace+5–pace+50"},
		{"pace+4", "cap", []any{"pace+4"}, "Profile coder-difficult, choice 1, 5h cap: pace+4 is outside pace+5–pace+50"},
		{"pace+0", "cap", []any{"pace+0"}, "Profile coder-difficult, choice 1, 5h cap: pace+0 is outside pace+5–pace+50"},
		{"not a number", "cap", []any{"fast"}, "Profile coder-difficult, choice 1, 5h cap: fast is not a number, pace+N or default"},
		{"null element", "cap", []any{nil}, "Profile coder-difficult, choice 1, 5h cap: null is not a number, pace+N or default"},
		{"Default is not default", "cap", []any{"Default"}, "Profile coder-difficult, choice 1, 5h cap: Default is not a number, pace+N or default"},
	}
	for _, c := range cases {
		if got := refusal(t, d, "choice.set", set(c.field, c.value)).Reason; got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	must(t, d, "choice.set", set("skip_during", []string{"deepseek-peak"}))
	args := set("allow_during", []string{"deepseek-peak"})
	args["expect"] = choiceJSON(t, d, "coder-difficult", 0)
	want := "Use Skip or Allow, not both"
	if got := refusal(t, d, "choice.set", args).Reason; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// Owner ruling 2026-10-23 (rounds 3 and 6): pace N is 5–50; both ends are
// saved.
func TestChoiceSetPaceRange(t *testing.T) {
	d := newDemo()
	for _, v := range []string{"pace+5", "pace+50"} {
		must(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 0, "field": "cap",
			"value": []any{v}, "expect": choiceJSON(t, d, "coder-difficult", 0)})
		var got struct {
			Cap []any `json:"cap"`
		}
		if err := json.Unmarshal(choiceJSON(t, d, "coder-difficult", 0), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Cap) != 1 || got.Cap[0] != v {
			t.Errorf("%s: stored cap %v", v, got.Cap)
		}
	}
}

// Owner ruling 2026-10-23 (round 5): "default" is a cap element (that
// position uses the Global default cap); the server stores the array as
// sent, trailing "default" elements included.
func TestChoiceSetDefaultElement(t *testing.T) {
	d := newDemo()
	for _, v := range [][]any{{"default", "default", 80.0}, {90.0, "default", 95.0}, {90.0, "default"}, {"default"}} {
		must(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 2, "field": "cap",
			"value": v, "expect": choiceJSON(t, d, "coder-difficult", 2)})
		var got struct {
			Cap []any `json:"cap"`
		}
		if err := json.Unmarshal(choiceJSON(t, d, "coder-difficult", 2), &got); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got.Cap) != fmt.Sprint(v) {
			t.Errorf("sent %v: stored cap %v", v, got.Cap)
		}
	}
}

func TestExpectMismatchRefused(t *testing.T) {
	d := newDemo()
	stale := choiceJSON(t, d, "coder-difficult", 0)
	must(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 0,
		"field": "cap", "value": []any{80}, "expect": stale})
	got := refusal(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 0,
		"field": "cap", "value": []any{70}, "expect": stale}).Reason
	if got != "Choice 1 of coder-difficult was changed outside the UI" {
		t.Fatalf("got %q", got)
	}
}

func TestChoiceAddBeforeUncapped(t *testing.T) {
	d := newDemo()
	must(t, d, "choice.add", map[string]any{"profile": "tester"})
	ch := profileOf(t, d, "tester").Choices
	if len(ch) != 5 || ch[3].Account != "claude-main" || ch[3].Model != "claude-opus-5-5" ||
		ch[3].Thinking == nil || *ch[3].Thinking != "low" || ch[3].Cap == nil || len(ch[3].Cap) != 0 {
		t.Fatalf("new choice wrong: %+v", ch)
	}
	if ch[4].Cap != nil {
		t.Fatalf("uncapped choice should stay last: %+v", ch[4])
	}
}

func TestChoiceMoveAndRemove(t *testing.T) {
	d := newDemo()
	before := d.State().SHA256
	res := must(t, d, "choice.move", map[string]any{"profile": "coder-difficult", "from": 0, "to": 2,
		"expect": choiceJSON(t, d, "coder-difficult", 0)})
	if res.SHA256 == before {
		t.Fatal("sha unchanged")
	}
	ch := profileOf(t, d, "coder-difficult").Choices
	if ch[2].Model != "claude-opus-5-5" || ch[0].Model != "gpt-6.1-sol" {
		t.Fatalf("order wrong: %+v", ch)
	}
	must(t, d, "profile.add", map[string]any{"name": "solo"})
	got := refusal(t, d, "choice.remove", map[string]any{"profile": "solo", "index": 0,
		"expect": choiceJSON(t, d, "solo", 0)}).Reason
	if got != "Profile solo needs at least one choice" {
		t.Fatalf("got %q", got)
	}
}

func TestChoiceSetAccountKeepsOrFixes(t *testing.T) {
	d := newDemo()
	must(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 1, "field": "account",
		"value": "go-1", "expect": choiceJSON(t, d, "coder-difficult", 1)})
	c := profileOf(t, d, "coder-difficult").Choices[1]
	if c.Account != "go-1" || c.Model != "opencode-go/deepseek-v4.1-flash" || c.Thinking != nil {
		t.Fatalf("got %+v", c)
	}
}

func TestNamesAndReserved(t *testing.T) {
	d := newDemo()
	for _, c := range []struct{ op, name string }{
		{"profile.add", "Bad Name"}, {"profile.add", "tester"}, {"schedule.add", "deepseek-peak"}, {"account.add", "go-1"}} {
		args := map[string]any{"name": c.name, "vendor": "opencode-go"}
		if refusal(t, d, c.op, args).Reason == "" {
			t.Errorf("%s %s: empty reason", c.op, c.name)
		}
	}
	got := refusal(t, d, "profile.add", map[string]any{"name": "My Profile"}).Reason
	if got != `Profile name "My Profile": use a–z, 0–9 and -, starting with a letter or digit` {
		t.Fatalf("got %q", got)
	}
	for op, w := range map[string]string{"schedule.add": "Schedule name", "account.add": "Account name"} {
		got = refusal(t, d, op, map[string]any{"name": "My Profile", "vendor": "claude"}).Reason
		if !strings.HasPrefix(got, w+` "My Profile": use a–z`) {
			t.Errorf("%s: got %q", op, got)
		}
	}
	got = refusal(t, d, "account.add", map[string]any{"name": "models", "vendor": "claude"}).Reason
	if got != "models is a reserved account name" {
		t.Fatalf("got %q", got)
	}
}

// Owner, 2026-10-23 (N13, N14): cap refusals name the choice and window.
func TestCapRefusalNamesChoiceAndWindow(t *testing.T) {
	d := newDemo()
	ex := choiceJSON(t, d, "coder-difficult", 2)
	args := func(v any) map[string]any {
		return map[string]any{"profile": "coder-difficult", "index": 2, "field": "cap",
			"value": v, "expect": ex}
	}
	got := refusal(t, d, "choice.set", args([]any{"pace+70"})).Reason
	if !strings.HasPrefix(got, "Profile coder-difficult, choice 3, ") ||
		!strings.HasSuffix(got, " cap: pace+70 is outside pace+5–pace+50") {
		t.Errorf("got %q", got)
	}
	got = refusal(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 2,
		"field": "model", "value": "nope", "expect": ex}).Reason
	if !strings.HasPrefix(got, "Profile coder-difficult, choice 3: ") {
		t.Errorf("got %q", got)
	}
	got = refusal(t, d, "settings.default_cap", map[string]any{"value": 150}).Reason
	if got != "Global default cap: 150 is outside 0–100%" {
		t.Errorf("got %q", got)
	}
}

func TestCapRefusalNumberedWithoutWindows(t *testing.T) {
	if got := capWhere("Profile p, choice 1", nil, 1); got != "Profile p, choice 1, cap 2" {
		t.Fatalf("got %q", got)
	}
}

func TestAccountRemoveReferenced(t *testing.T) {
	d := newDemo()
	got := refusal(t, d, "account.remove", map[string]any{"name": "claude-main"}).Reason
	if got != "Account claude-main is used by profile coder-difficult, choice 1" {
		t.Fatalf("got %q", got)
	}
}

func TestScheduleRemoveUsesAndForce(t *testing.T) {
	d := newDemo()
	r := refusal(t, d, "schedule.remove", map[string]any{"name": "deepseek-peak", "force": false})
	if r.Uses != 4 || r.Reason != "Schedule deepseek-peak is used by 4 choices" {
		t.Fatalf("got %+v", r)
	}
	must(t, d, "schedule.remove", map[string]any{"name": "deepseek-peak", "force": true})
	s := d.State()
	if len(s.Schedules) != 0 {
		t.Fatalf("schedule left: %+v", s.Schedules)
	}
	for _, p := range s.Profiles {
		for _, c := range p.Choices {
			if len(c.SkipDuring) != 0 || len(c.AllowDuring) != 0 {
				t.Fatalf("name left in %s: %+v", p.Name, c)
			}
		}
	}
}

func TestSchedulePaintRoundTrip(t *testing.T) {
	d := newDemo()
	must(t, d, "schedule.add", map[string]any{"name": "night"})
	cells := [][2]int{{0, 0}, {6, 23}}
	must(t, d, "schedule.paint", map[string]any{"name": "night", "cells": cells, "on": true})
	night := func() web.Schedule {
		for _, s := range d.State().Schedules {
			if s.Name == "night" {
				return s
			}
		}
		return web.Schedule{}
	}
	if s := night(); s.Cells[0][0] != 1 || s.Cells[6][23] != 1 || s.Cells[3][3] != 0 {
		t.Fatalf("paint on failed")
	}
	must(t, d, "schedule.paint", map[string]any{"name": "night", "cells": cells[:1], "on": false})
	if s := night(); s.Cells[0][0] != 0 || s.Cells[6][23] != 1 {
		t.Fatalf("paint off failed")
	}
}

func TestHandEditInvalidatesBase(t *testing.T) {
	d := newDemo()
	old := d.State().SHA256
	d.HandEdit()
	if th := profileOf(t, d, "tester").Choices[3].Thinking; th == nil || *th != "low" {
		t.Fatalf("thinking %v", th)
	}
	_, err := apply(t, d, old, "profile.add", map[string]any{"name": "x"})
	var c web.Conflict
	if !errors.As(err, &c) {
		t.Fatalf("want Conflict, got %v", err)
	}
}

func TestBrokenIsReadOnlyAndRestores(t *testing.T) {
	d := newDemo()
	d.SetBroken("config.json: line 12: invalid character '}'")
	if d.State().ReadOnly == "" {
		t.Fatal("read_only empty")
	}
	if refusal(t, d, "profile.add", map[string]any{"name": "x"}).Reason == "" {
		t.Fatal("edit allowed while broken")
	}
	d.SetBroken("")
	must(t, d, "profile.add", map[string]any{"name": "x"})
}

func TestRefreshRules(t *testing.T) {
	now := t0
	d := New(func() time.Time { return now })
	for _, c := range []struct{ acct, want string }{
		{"claude-rec", "claude-rec: expired token"},
		{"go-3", "go-3: rate-limited until 2026-10-23T09:32:00Z"}} {
		_, err := d.Refresh(c.acct)
		var r web.Refused
		if !errors.As(err, &r) || r.Reason != c.want {
			t.Errorf("%s: got %#v want %q", c.acct, err, c.want)
		}
	}
	now = t0.Add(time.Hour)
	row, err := d.Refresh("go-1")
	if err != nil || row.ReadingAgeSeconds == nil || *row.ReadingAgeSeconds != 0 || !row.CanRefresh {
		t.Fatalf("got %+v, %v", row, err)
	}
}

func TestUsageFixture(t *testing.T) {
	rows := newDemo().Usage()
	if len(rows) != 6 || *rows[0].ReadingAgeSeconds != 240 || len(rows[1].Windows) != 0 {
		t.Fatalf("rows: %+v", rows)
	}
	w := rows[0].Windows[2]
	if w.Label != "7d Fable" || w.Scope == nil || *w.Scope != "claude-fable-5-1" ||
		w.Length != "168h0m0s" || w.ResetsAt != "2026-10-25T20:50:00Z" {
		t.Fatalf("fable window: %+v", w)
	}
	if rows[5].CanRefresh || *rows[5].ReadingAgeSeconds != 38*60 {
		t.Fatalf("go-3: %+v", rows[5])
	}
}

func TestDoctorAndAddError(t *testing.T) {
	d := newDemo()
	if doc := d.Doctor(); doc.Errors != 0 || doc.Warnings != 1 || doc.Findings[0].Subject != "claude-rec" {
		t.Fatalf("got %+v", doc)
	}
	d.AddError()
	doc := d.Doctor()
	if doc.Errors != 1 || doc.Findings[0].Message != "key line 3 missing in ~/.ssh/opencode-go-api" {
		t.Fatalf("got %+v", doc)
	}
}

func TestNoUnexpectedNulls(t *testing.T) {
	d := newDemo()
	d.AddError()
	var all strings.Builder
	for _, v := range []any{d.State(), d.Usage(), d.Doctor()} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(raw)
	}
	s := all.String()
	for _, ok := range []string{`"cap":null`, `"thinking":null`, `"scope":null`, `"resets_at":null`, `"reading_age_seconds":null`} {
		s = strings.ReplaceAll(s, ok, "")
	}
	if i := strings.Index(s, "null"); i >= 0 {
		t.Fatalf("unexpected null near %q", s[max(0, i-60):i+10])
	}
	if !strings.Contains(string(mustJSON(t, d.State())), `"cap":null`) {
		t.Fatal("expected the uncapped choice to encode cap:null")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestNoCredentialFields(t *testing.T) {
	raw := mustJSON(t, newDemo().State())
	for _, bad := range []string{"sk-", "token\":", "secret", "password"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("state contains %q", bad)
		}
	}
}

func TestCodexFieldsAndLocation(t *testing.T) {
	d := newDemo()
	var a web.Account
	for _, x := range d.State().Accounts {
		if x.Name == "codex-main" {
			a = x
		}
	}
	if a.Fields["codex_home"] != "~/.codex" || len(a.Fields) != 1 {
		t.Fatalf("fixture fields: %v", a.Fields)
	}
	must(t, d, "account.add", map[string]any{"name": "codex-2", "vendor": "codex",
		"fields": map[string]any{"codex_home": "~/.codex2"}})
	for _, x := range d.State().Accounts {
		if x.Name == "codex-2" && (x.Location != "~/.codex2" || x.Source != "file: ~/.codex2/auth.json") {
			t.Fatalf("built from codex_home wrongly: %+v", x)
		}
	}
}

func TestNoReadingIsNullAndHasNoWindows(t *testing.T) {
	d := newDemo()
	raw := string(mustJSON(t, d.Usage()[1]))
	if !strings.Contains(raw, `"reading_age_seconds":null`) {
		t.Fatalf("claude-rec: %s", raw)
	}
	must(t, d, "account.add", map[string]any{"name": "go-4", "vendor": "opencode-go",
		"fields": map[string]any{"key_file": "~/.ssh/opencode-go-api", "key_line": 4}})
	w := d.State().Windows
	if _, ok := w["claude-rec"]; ok {
		t.Error("claude-rec has windows")
	}
	if _, ok := w["go-4"]; ok {
		t.Error("new account has windows")
	}
	if _, ok := w["go-1"]; !ok {
		t.Error("go-1 lost its windows")
	}
}

func TestHandEditKeepsStateValid(t *testing.T) {
	d := newDemo()
	d.HandEdit()
	if th := profileOf(t, d, "tester").Choices[3].Thinking; th == nil || *th != "low" {
		t.Fatalf("claude model should get low, got %v", th)
	}
	d = newDemo()
	must(t, d, "choice.set", map[string]any{"profile": "tester", "index": 3, "field": "account",
		"value": "go-1", "expect": choiceJSON(t, d, "tester", 3)})
	before := d.State()
	d.HandEdit()
	after := d.State()
	if th := profileOf(t, d, "tester").Choices[3].Thinking; th != nil {
		t.Fatalf("model without levels got thinking %q", *th)
	}
	if after.SHA256 == before.SHA256 {
		t.Fatal("sha unchanged")
	}
}

func sourceOf(t *testing.T, d *Demo, name string) string {
	t.Helper()
	for _, a := range d.State().Accounts {
		if a.Name == name {
			return a.Source
		}
	}
	t.Fatalf("no account %s", name)
	return ""
}

func TestKeyLineSourceCountsAccountsOnTheFile(t *testing.T) {
	d := newDemo()
	if got := sourceOf(t, d, "go-2"); got != "key line 2 of 3" {
		t.Fatalf("fixture go-2: %q", got)
	}
	must(t, d, "account.add", map[string]any{"name": "go-4", "vendor": "opencode-go",
		"fields": map[string]any{"key_file": "~/.ssh/opencode-go-api", "key_line": 4}})
	must(t, d, "account.add", map[string]any{"name": "other-1", "vendor": "opencode-go",
		"fields": map[string]any{"key_file": "~/.ssh/other-key", "key_line": 1}})
	for name, want := range map[string]string{"go-4": "key line 4 of 4", "go-1": "key line 1 of 4",
		"other-1": "key line 1 of 1"} {
		if got := sourceOf(t, d, name); got != want {
			t.Errorf("%s: %q want %q", name, got, want)
		}
	}
	must(t, d, "account.remove", map[string]any{"name": "go-4"})
	if got := sourceOf(t, d, "go-1"); got != "key line 1 of 3" {
		t.Errorf("after remove go-1: %q", got)
	}
}

func usageOf(t *testing.T, d *Demo, name string) web.UsageRow {
	t.Helper()
	for _, r := range d.Usage() {
		if r.Account == name {
			return r
		}
	}
	t.Fatalf("no usage row %s", name)
	return web.UsageRow{}
}

func TestNoReadingIsRefreshableUnlessBlocked(t *testing.T) {
	d := newDemo()
	if usageOf(t, d, "claude-rec").CanRefresh {
		t.Error("expired claude-rec refreshable")
	}
	if usageOf(t, d, "go-3").CanRefresh {
		t.Error("rate-limited go-3 refreshable")
	}
	must(t, d, "account.add", map[string]any{"name": "go-4", "vendor": "opencode-go",
		"fields": map[string]any{"key_file": "~/.ssh/opencode-go-api", "key_line": 4}})
	if r := usageOf(t, d, "go-4"); !r.CanRefresh || r.ReadingAgeSeconds != nil {
		t.Fatalf("new go-4: %+v", r)
	}
	row, err := d.Refresh("go-4")
	if err != nil {
		t.Fatal(err)
	}
	labels := []string{}
	for _, w := range row.Windows {
		labels = append(labels, w.Label)
	}
	if row.State != "ok" || !row.CanRefresh || row.ReadingAgeSeconds == nil || *row.ReadingAgeSeconds != 0 ||
		strings.Join(labels, ",") != "5h,7d,30d" {
		t.Fatalf("refreshed go-4: %+v", row)
	}
	w, ok := d.State().Windows["go-4"]
	if !ok || strings.Join(w["opencode-go/deepseek-v4.1-flash"], ",") != "5h,7d,30d" {
		t.Fatalf("windows[go-4] after refresh: %v", d.State().Windows)
	}
	if _, err := d.Refresh("claude-rec"); err == nil {
		t.Error("claude-rec refreshed")
	}
}

// Owner, 2026-10-23 (wording): a count of 1 is singular, and a refusal
// reason that starts with a kind word starts with a capital.
func TestPluralAndCapital(t *testing.T) {
	if got := plural(1, "choice"); got != "1 choice" {
		t.Fatalf("got %q", got)
	}
	if got := plural(4, "choice"); got != "4 choices" {
		t.Fatalf("got %q", got)
	}
	if got := capital("profile"); got != "Profile" {
		t.Fatalf("got %q", got)
	}
}
