package demo

import (
	"encoding/json"
	"errors"
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
		{"range", "cap", []any{120}, "profiles.coder-difficult[0].cap[0]: 120 is outside 0–100"},
		{"pace zero", "cap", []any{90, "pace+0"}, `profiles.coder-difficult[0].cap[1]: "pace+0": 0 must be 1-100`},
		{"not a number", "cap", []any{"fast"}, `profiles.coder-difficult[0].cap[0]: "fast" is not a number or pace+N`},
		{"both lists", "skip_during", []string{"deepseek-peak"}, ""},
	}
	for _, c := range cases[:3] {
		if got := refusal(t, d, "choice.set", set(c.field, c.value)).Reason; got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	must(t, d, "choice.set", set("skip_during", []string{"deepseek-peak"}))
	args := set("allow_during", []string{"deepseek-peak"})
	args["expect"] = choiceJSON(t, d, "coder-difficult", 0)
	want := "profiles.coder-difficult[0]: use skip_during or allow_during, not both"
	if got := refusal(t, d, "choice.set", args).Reason; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestExpectMismatchRefused(t *testing.T) {
	d := newDemo()
	stale := choiceJSON(t, d, "coder-difficult", 0)
	must(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 0,
		"field": "cap", "value": []any{80}, "expect": stale})
	got := refusal(t, d, "choice.set", map[string]any{"profile": "coder-difficult", "index": 0,
		"field": "cap", "value": []any{70}, "expect": stale}).Reason
	if got != "the choice you edited was changed in the file; load the file's version" {
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
	if got != "profile solo needs at least one choice" {
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
	got := refusal(t, d, "account.add", map[string]any{"name": "models", "vendor": "claude"}).Reason
	if got != "accounts.models: reserved name" {
		t.Fatalf("got %q", got)
	}
}

func TestAccountRemoveReferenced(t *testing.T) {
	d := newDemo()
	got := refusal(t, d, "account.remove", map[string]any{"name": "claude-main"}).Reason
	if got != "account claude-main is used by coder-difficult[0]" {
		t.Fatalf("got %q", got)
	}
}

func TestScheduleRemoveUsesAndForce(t *testing.T) {
	d := newDemo()
	r := refusal(t, d, "schedule.remove", map[string]any{"name": "deepseek-peak", "force": false})
	if r.Uses != 4 || r.Reason != "schedule deepseek-peak is used by 4 choice(s)" {
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
		{"claude-rec", "claude-rec: token expired"},
		{"go-3", "go-3: rate-limited until 2026-10-23T09:32:00Z"}} {
		_, err := d.Refresh(c.acct)
		var r web.Refused
		if !errors.As(err, &r) || r.Reason != c.want {
			t.Errorf("%s: got %#v want %q", c.acct, err, c.want)
		}
	}
	now = t0.Add(time.Hour)
	row, err := d.Refresh("go-1")
	if err != nil || row.ReadingAgeSeconds != 0 || !row.CanRefresh {
		t.Fatalf("got %+v, %v", row, err)
	}
}

func TestUsageFixture(t *testing.T) {
	rows := newDemo().Usage()
	if len(rows) != 6 || rows[0].ReadingAgeSeconds != 240 || len(rows[1].Windows) != 0 {
		t.Fatalf("rows: %+v", rows)
	}
	w := rows[0].Windows[2]
	if w.Label != "7d Fable" || w.Scope == nil || *w.Scope != "claude-fable-5-1" ||
		w.Length != "168h0m0s" || w.ResetsAt != "2026-10-25T20:50:00Z" {
		t.Fatalf("fable window: %+v", w)
	}
	if rows[5].CanRefresh || rows[5].ReadingAgeSeconds != 38*60 {
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
