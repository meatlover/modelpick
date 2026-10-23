// Package webtest is the conformance suite every web.Backend runs: the
// back-end half of the contract the page relies on (api.go, "Back-end
// contract"). The demo and the live back end both pass it.
package webtest

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/web"
)

// Harness builds one back end per case and changes what is behind it.
// HandEdit, Break and Fix act on the back end New returned last.
type Harness struct {
	New      func(t *testing.T) web.Backend
	HandEdit func(t *testing.T) // changes the config behind the backend's back
	Break    func(t *testing.T) // makes it unparsable
	Fix      func(t *testing.T)
	Secrets  []string // must never appear in any response body
	// Refreshable names an account whose refresh succeeds with a reading
	// that has windows.
	Refreshable string
}

// The fixture behind New must have at least one account, one profile and
// a choice with no schedule lists.
func Run(t *testing.T, h Harness) {
	t.Helper()
	cases := []struct {
		name string
		run  func(*testing.T, Harness)
	}{
		{"NoNullLists", noNullLists},
		{"SortedByName", sortedByName},
		{"UsageRows", usageRows},
		{"DoctorCountsAndSubjects", doctorShape},
		{"StaleBaseConflicts", staleBase},
		{"RefusedOpKeepsSHA", refusedKeepsSHA},
		{"ReadOnlyRefusesEveryOp", readOnly},
		{"ScheduleRemoveReportsUses", scheduleUses},
		{"EmptyScheduleListAccepted", emptyScheduleList},
		{"ExpectMismatchRefused", expectMismatch},
		{"ExpectToleratesNullLists", expectNullLists},
		{"RefreshNamesTheAccount", refreshNames},
		{"RefreshReturnsTheRow", refreshRow},
		{"NoSecretInAnyBody", noSecrets},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, h) })
	}
}

func apply(t *testing.T, b web.Backend, base, op string, args any) (web.OpResult, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return b.Apply(web.OpRequest{Base: base, Op: op, Args: raw})
}

// mustApply applies op against the current sha; the answer's sha must be
// the one State reports next.
func mustApply(t *testing.T, b web.Backend, op string, args any) web.OpResult {
	t.Helper()
	res, err := apply(t, b, b.State().SHA256, op, args)
	if err != nil {
		t.Fatalf("%s %v: %v", op, args, err)
	}
	if now := b.State().SHA256; res.SHA256 != now {
		t.Fatalf("%s: answer sha %s, state sha %s", op, res.SHA256, now)
	}
	return res
}

// asRefused and asConflict accept a value or a pointer, as the server does.
func asRefused(err error) (web.Refused, bool) {
	var v web.Refused
	if errors.As(err, &v) {
		return v, true
	}
	var p *web.Refused
	if errors.As(err, &p) && p != nil {
		return *p, true
	}
	return web.Refused{}, false
}

func asConflict(err error) (web.Conflict, bool) {
	var v web.Conflict
	if errors.As(err, &v) {
		return v, true
	}
	var p *web.Conflict
	if errors.As(err, &p) && p != nil {
		return *p, true
	}
	return web.Conflict{}, false
}

func mustRefuse(t *testing.T, err error, what string) web.Refused {
	t.Helper()
	r, ok := asRefused(err)
	if !ok {
		t.Fatalf("%s: want Refused, got %#v", what, err)
	}
	if r.Reason == "" {
		t.Fatalf("%s: refused with an empty reason", what)
	}
	return r
}

// nullable are the keys whose null means something (contract 11).
var nullable = map[string]bool{"cap": true, "thinking": true, "scope": true,
	"resets_at": true, "reading_age_seconds": true}

// nullPaths lists every JSON null in v outside the nullable keys.
func nullPaths(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var x any
	if err := json.Unmarshal(raw, &x); err != nil {
		t.Fatal(err)
	}
	var out []string
	walkNulls("$", "", x, &out)
	sort.Strings(out)
	return out
}

func walkNulls(path, key string, x any, out *[]string) {
	switch v := x.(type) {
	case nil:
		if !nullable[key] {
			*out = append(*out, path)
		}
	case map[string]any:
		for k, e := range v {
			walkNulls(path+"."+k, k, e, out)
		}
	case []any:
		for i, e := range v {
			walkNulls(fmt.Sprintf("%s[%d]", path, i), "", e, out)
		}
	}
}

func checkNoNulls(t *testing.T, b web.Backend, when string) {
	t.Helper()
	for what, v := range map[string]any{"state": b.State(), "usage": b.Usage(), "doctor": b.Doctor()} {
		if p := nullPaths(t, v); len(p) > 0 {
			t.Errorf("%s %s: null at %v", when, what, p)
		}
	}
}

func noNullLists(t *testing.T, h Harness) {
	b := h.New(t)
	checkNoNulls(t, b, "fixture")
	res := mustApply(t, b, "schedule.add", map[string]any{"name": "wt-nulls"})
	if p := nullPaths(t, res); len(p) > 0 {
		t.Errorf("op answer: null at %v", p)
	}
	if rows := b.Usage(); rows == nil {
		t.Error("usage: nil row list")
	}
	h.Break(t)
	checkNoNulls(t, b, "read-only")
	h.Fix(t)
}

// plainChoice is the first choice with no schedule lists.
func plainChoice(t *testing.T, s web.State) (string, int, web.Choice) {
	t.Helper()
	for _, p := range s.Profiles {
		for i, c := range p.Choices {
			if len(c.SkipDuring) == 0 && len(c.AllowDuring) == 0 {
				return p.Name, i, c
			}
		}
	}
	t.Fatal("fixture has no choice without schedules")
	return "", 0, web.Choice{}
}

func choiceAt(t *testing.T, s web.State, profile string, i int) web.Choice {
	t.Helper()
	for _, p := range s.Profiles {
		if p.Name == profile && i < len(p.Choices) {
			return p.Choices[i]
		}
	}
	t.Fatalf("no choice %d in profile %s", i+1, profile)
	return web.Choice{}
}

func names[T any](items []T, name func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, name(it))
	}
	return out
}

func sortedByName(t *testing.T, h Harness) {
	b := h.New(t)
	// Names that sort first, so a back end that appends is caught.
	mustApply(t, b, "schedule.add", map[string]any{"name": "0-wt"})
	mustApply(t, b, "profile.add", map[string]any{"name": "0-wt"})
	mustApply(t, b, "account.add", map[string]any{"name": "0-wt", "vendor": "claude",
		"fields": map[string]any{"config_dir": "~/.wt"}})
	s := b.State()
	lists := map[string][]string{
		"accounts":  names(s.Accounts, func(a web.Account) string { return a.Name }),
		"schedules": names(s.Schedules, func(x web.Schedule) string { return x.Name }),
		"profiles":  names(s.Profiles, func(p web.Profile) string { return p.Name }),
		"usage":     names(b.Usage(), func(r web.UsageRow) string { return r.Account }),
	}
	for what, l := range lists {
		if !sort.StringsAreSorted(l) {
			t.Errorf("%s not sorted by name: %v", what, l)
		}
	}
	if a, u := lists["accounts"], lists["usage"]; strings.Join(a, ",") != strings.Join(u, ",") {
		t.Errorf("usage rows %v, want one per account %v", u, a)
	}
}

// stateKnown is the UsageRow.state vocabulary (contract 1, ruling 2).
func stateKnown(s string) bool {
	switch {
	case s == "ok", s == "expired token", s == "no reading yet", s == "rate-limited (no retry time given)":
		return true
	case strings.HasPrefix(s, "rate-limited until "):
		_, err := time.Parse(time.RFC3339, strings.TrimPrefix(s, "rate-limited until "))
		return err == nil
	}
	return strings.HasPrefix(s, "error: ")
}

func checkRow(t *testing.T, r web.UsageRow) {
	t.Helper()
	if !stateKnown(r.State) {
		t.Errorf("%s: state %q is not in the vocabulary", r.Account, r.State)
	}
	blocked := r.State == "expired token" || strings.HasPrefix(r.State, "rate-limited until ")
	if r.CanRefresh == blocked {
		t.Errorf("%s: can_refresh %v with state %q", r.Account, r.CanRefresh, r.State)
	}
	if r.ReadingAgeSeconds == nil && len(r.Windows) > 0 {
		t.Errorf("%s: windows without a reading", r.Account)
	}
	for _, w := range r.Windows {
		if _, err := time.Parse(time.RFC3339, w.ResetsAt); w.ResetsAt != "" && (err != nil || !strings.HasSuffix(w.ResetsAt, "Z")) {
			t.Errorf("%s: resets_at %q is not RFC 3339 UTC", r.Account, w.ResetsAt)
		}
	}
}

func usageRows(t *testing.T, h Harness) {
	b := h.New(t)
	mustApply(t, b, "account.add", map[string]any{"name": "wt-new", "vendor": "claude",
		"fields": map[string]any{"config_dir": "~/.wt"}})
	s := b.State()
	withReading := map[string]bool{}
	for _, r := range b.Usage() {
		checkRow(t, r)
		withReading[r.Account] = r.ReadingAgeSeconds != nil
		if r.Account == "wt-new" && (r.State != "no reading yet" || !r.CanRefresh) {
			t.Errorf("new account: state %q can_refresh %v, want no reading yet, true", r.State, r.CanRefresh)
		}
	}
	for acct := range s.Windows {
		if !withReading[acct] {
			t.Errorf("windows[%s] present without a saved reading", acct)
		}
	}
}

func doctorShape(t *testing.T, h Harness) {
	b := h.New(t)
	s := b.State()
	accounts := map[string]bool{}
	for _, a := range s.Accounts {
		accounts[a.Name] = true
	}
	d := b.Doctor()
	count := map[string]int{}
	for _, f := range d.Findings {
		count[f.Severity]++
		if f.Severity != "error" && f.Severity != "warning" {
			t.Errorf("finding %+v: severity", f)
		}
		ok := accounts[f.Subject] || f.Subject == "config" ||
			strings.HasPrefix(f.Subject, "profile ") || strings.HasPrefix(f.Subject, "schedule ")
		if !ok {
			t.Errorf("finding %+v: subject is not an account, profile, schedule or config", f)
		}
	}
	if d.Errors != count["error"] || d.Warnings != count["warning"] {
		t.Errorf("counts %d/%d, findings say %d/%d", d.Errors, d.Warnings, count["error"], count["warning"])
	}
}

func staleBase(t *testing.T, h Harness) {
	b := h.New(t)
	s0 := b.State()
	mustApply(t, b, "schedule.add", map[string]any{"name": "wt-stale"})
	_, err := apply(t, b, s0.SHA256, "schedule.add", map[string]any{"name": "wt-stale-2"})
	if c, ok := asConflict(err); !ok || c.SHA256 != b.State().SHA256 {
		t.Fatalf("stale base: got %#v, want Conflict with the current sha", err)
	}
	s1 := b.State()
	h.HandEdit(t)
	s2 := b.State()
	if s2.SHA256 == s1.SHA256 {
		t.Fatal("HandEdit did not change the sha")
	}
	_, err = apply(t, b, s1.SHA256, "schedule.add", map[string]any{"name": "wt-stale-3"})
	if c, ok := asConflict(err); !ok || c.SHA256 != s2.SHA256 {
		t.Fatalf("after a hand edit: got %#v, want Conflict %s", err, s2.SHA256)
	}
}

func refusedKeepsSHA(t *testing.T, h Harness) {
	b := h.New(t)
	cases := []struct {
		op   string
		args map[string]any
	}{
		{"wt.no-such-op", map[string]any{}},
		{"profile.add", map[string]any{"name": "Not A Name"}},
		{"schedule.remove", map[string]any{"name": "wt-missing"}},
		{"settings.default_cap", map[string]any{"value": 101}},
		{"account.add", map[string]any{"name": "wt-x", "vendor": "no-such-vendor"}},
	}
	for _, c := range cases {
		before := b.State().SHA256
		_, err := apply(t, b, before, c.op, c.args)
		mustRefuse(t, err, c.op)
		if after := b.State().SHA256; after != before {
			t.Errorf("%s: refused, but the sha moved %s -> %s", c.op, before, after)
		}
	}
}

type opCall struct {
	op   string
	args map[string]any
}

// everyOp is one call of each operation the page sends, built on s.
func everyOp(t *testing.T, s web.State) []opCall {
	t.Helper()
	p, i, c := plainChoice(t, s)
	sched := "wt-x"
	if len(s.Schedules) > 0 {
		sched = s.Schedules[0].Name
	}
	at := map[string]any{"profile": p, "index": i, "expect": c}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range at {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	return []opCall{
		{"settings.default_cap", map[string]any{"value": 90}},
		{"profile.add", map[string]any{"name": "wt-new"}},
		{"choice.add", map[string]any{"profile": p}},
		{"choice.set", with(map[string]any{"field": "cap", "value": []any{80}})},
		{"choice.move", map[string]any{"profile": p, "from": i, "to": 0, "expect": c}},
		{"schedule.add", map[string]any{"name": "wt-new"}},
		{"schedule.paint", map[string]any{"name": sched, "cells": [][2]int{{1, 2}}, "on": true}},
		{"account.add", map[string]any{"name": "wt-new", "vendor": "claude",
			"fields": map[string]any{"config_dir": "~/.wt"}}},
		{"choice.remove", with(nil)},
		{"schedule.remove", map[string]any{"name": sched, "force": true}},
		{"profile.remove", map[string]any{"name": p}},
		{"account.remove", map[string]any{"name": s.Accounts[0].Name}},
	}
}

func readOnly(t *testing.T, h Harness) {
	b := h.New(t)
	ops := everyOp(t, b.State())
	h.Break(t)
	ro := b.State()
	if ro.ReadOnly == "" || ro.SHA256 == "" {
		t.Fatalf("broken config: read_only %q sha %q", ro.ReadOnly, ro.SHA256)
	}
	if p := nullPaths(t, ro); len(p) > 0 {
		t.Errorf("read-only state: null at %v", p)
	}
	for _, o := range ops {
		_, err := apply(t, b, ro.SHA256, o.op, o.args)
		mustRefuse(t, err, "read-only "+o.op)
		if now := b.State().SHA256; now != ro.SHA256 {
			t.Fatalf("read-only %s moved the sha", o.op)
		}
	}
	h.Fix(t)
	if s := b.State(); s.ReadOnly != "" {
		t.Errorf("after Fix: read_only %q", s.ReadOnly)
	}
}

func scheduleUses(t *testing.T, h Harness) {
	b := h.New(t)
	mustApply(t, b, "schedule.add", map[string]any{"name": "wt-used"})
	p, i, c := plainChoice(t, b.State())
	mustApply(t, b, "choice.set", map[string]any{"profile": p, "index": i, "expect": c,
		"field": "skip_during", "value": []string{"wt-used"}})
	before := b.State().SHA256
	_, err := apply(t, b, before, "schedule.remove", map[string]any{"name": "wt-used"})
	r := mustRefuse(t, err, "schedule.remove in use")
	if r.Uses != 1 || !strings.Contains(r.Reason, "wt-used") {
		t.Errorf("refusal %+v, want uses 1 and a reason naming wt-used", r)
	}
	if b.State().SHA256 != before {
		t.Error("refused removal moved the sha")
	}
	mustApply(t, b, "schedule.remove", map[string]any{"name": "wt-used", "force": true})
	s := b.State()
	if got := choiceAt(t, s, p, i); len(got.SkipDuring) != 0 {
		t.Errorf("forced removal left skip_during %v", got.SkipDuring)
	}
	for _, x := range s.Schedules {
		if x.Name == "wt-used" {
			t.Error("forced removal kept the schedule")
		}
	}
}

func emptyScheduleList(t *testing.T, h Harness) {
	b := h.New(t)
	p, i, _ := plainChoice(t, b.State())
	for _, field := range []string{"skip_during", "allow_during"} {
		c := choiceAt(t, b.State(), p, i)
		mustApply(t, b, "choice.set", map[string]any{"profile": p, "index": i, "expect": c,
			"field": field, "value": []string{}})
		got := choiceAt(t, b.State(), p, i)
		if got.SkipDuring == nil || got.AllowDuring == nil || len(got.SkipDuring)+len(got.AllowDuring) != 0 {
			t.Errorf("%s []: choice now %+v", field, got)
		}
	}
}

func expectMismatch(t *testing.T, h Harness) {
	b := h.New(t)
	before := b.State()
	p, i, c := plainChoice(t, before)
	c.Model = "wt-changed-by-hand"
	_, err := apply(t, b, before.SHA256, "choice.set", map[string]any{"profile": p, "index": i,
		"expect": c, "field": "cap", "value": nil})
	r := mustRefuse(t, err, "expect mismatch")
	if !strings.Contains(r.Reason, "changed outside the UI") {
		t.Errorf("reason %q, want it to say the choice was changed outside the UI", r.Reason)
	}
	if b.State().SHA256 != before.SHA256 {
		t.Error("refused set moved the sha")
	}
}

// Contract 7: expect compares after normalising null vs [] lists.
func expectNullLists(t *testing.T, h Harness) {
	b := h.New(t)
	p, i, c := plainChoice(t, b.State())
	c.SkipDuring, c.AllowDuring = nil, nil
	mustApply(t, b, "choice.set", map[string]any{"profile": p, "index": i, "expect": c,
		"field": "cap", "value": c.Cap})
}

func refreshNames(t *testing.T, h Harness) {
	b := h.New(t)
	_, err := b.Refresh("wt-no-such-account")
	r := mustRefuse(t, err, "refresh of an unknown account")
	if !strings.HasPrefix(r.Reason, "wt-no-such-account") {
		t.Errorf("reason %q does not lead with the account", r.Reason)
	}
}

// refreshRow: a refresh of a known account answers its full row, whatever
// the call's outcome (the state carries it; controller ruling, task 11
// review), and that row is the one Usage reports next.
func refreshRow(t *testing.T, h Harness) {
	if h.Refreshable == "" {
		t.Fatal("the harness names no refreshable account")
	}
	b := h.New(t)
	for _, a := range b.State().Accounts {
		row, err := b.Refresh(a.Name)
		if err != nil {
			t.Errorf("%s: refresh answered %#v, want its row", a.Name, err)
			continue
		}
		checkRow(t, row)
		if row.Account != a.Name || row.Vendor != a.Vendor {
			t.Errorf("%s: row for %s/%s", a.Name, row.Account, row.Vendor)
		}
		if u := usageRow(t, b, a.Name); u.State != row.State || u.CanRefresh != row.CanRefresh {
			t.Errorf("%s: refresh row %q/%v, usage then says %q/%v", a.Name, row.State, row.CanRefresh, u.State, u.CanRefresh)
		}
		if a.Name == h.Refreshable {
			refreshed(t, b, row)
		}
	}
}

func refreshed(t *testing.T, b web.Backend, row web.UsageRow) {
	t.Helper()
	if row.State != "ok" || row.ReadingAgeSeconds == nil || len(row.Windows) == 0 {
		t.Errorf("%s: refreshed row %+v, want ok with a reading and windows", row.Account, row)
	}
	if len(b.State().Windows[row.Account]) == 0 {
		t.Errorf("%s: refreshed, but state has no windows for it", row.Account)
	}
}

func usageRow(t *testing.T, b web.Backend, account string) web.UsageRow {
	t.Helper()
	for _, r := range b.Usage() {
		if r.Account == account {
			return r
		}
	}
	t.Fatalf("usage has no row for %s", account)
	return web.UsageRow{}
}
