package live

import (
	"fmt"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/web"
)

// This file maps app and core values to the wire types (api.go, contract
// 1–17). Every list is non-nil; names are sorted.

func emptyState(path, sha, readOnly string) web.State {
	return web.State{SHA256: sha, Path: path, ReadOnly: readOnly,
		Accounts: []web.Account{}, Models: map[string][]web.Model{},
		Windows: map[string]map[string][]string{}, Schedules: []web.Schedule{}, Profiles: []web.Profile{}}
}

func list(in []string) []string { return append([]string{}, in...) }

// wireAccounts gives each account its locator fields as written (contract
// 15: "~" unexpanded, key_line a number) and display strings: the locator
// as location, and as source the credential's description or, for
// opencode-go, "key line N of M" with M the accounts on that key file
// (owner, 2026-10-23, N10). sources may be nil (no valid config).
func wireAccounts(doc *core.Doc, sources map[string]string) []web.Account {
	onFile := map[string]int{}
	for _, a := range doc.Accounts {
		if a.Vendor == "opencode-go" {
			onFile[a.KeyFile]++
		}
	}
	out := []web.Account{}
	for _, n := range sortedNames(doc.Accounts) {
		a := doc.Accounts[n]
		w := web.Account{Name: n, Vendor: a.Vendor, Fields: map[string]any{}, Source: sources[n]}
		switch a.Vendor {
		case "claude":
			w.Fields["config_dir"], w.Location = a.ConfigDir, a.ConfigDir
		case "codex":
			w.Fields["codex_home"], w.Location = a.CodexHome, a.CodexHome
		case "opencode-go":
			w.Fields["key_file"], w.Fields["key_line"], w.Location = a.KeyFile, a.KeyLine, a.KeyFile
			w.Source = fmt.Sprintf("key line %d of %d", a.KeyLine, onFile[a.KeyFile])
		}
		out = append(out, w)
	}
	return out
}

// wireSchedules gives each schedule's covered fraction per UTC day and
// hour; a schedule whose rules do not parse shows empty.
func wireSchedules(doc *core.Doc) []web.Schedule {
	out := []web.Schedule{}
	for _, n := range sortedNames(doc.Schedules) {
		s := web.Schedule{Name: n}
		if w, err := core.RulesWeek(doc.Schedules[n]); err == nil {
			s.Cells = w.Cells()
		}
		out = append(out, s)
	}
	return out
}

func wireProfiles(doc *core.Doc) []web.Profile {
	out := []web.Profile{}
	for _, n := range sortedNames(doc.Profiles) {
		p := web.Profile{Name: n, Choices: []web.Choice{}}
		for _, c := range doc.Profiles[n] {
			p.Choices = append(p.Choices, wireChoice(c))
		}
		out = append(out, p)
	}
	return out
}

func wireChoice(c core.DocChoice) web.Choice {
	w := web.Choice{Account: c.Account, Model: c.Model, Cap: wireCap(c),
		SkipDuring: list(c.SkipDuring), AllowDuring: list(c.AllowDuring)}
	if c.Thinking != nil {
		t := *c.Thinking
		w.Thinking = &t
	}
	return w
}

// wireCap is null for an uncapped choice, else its elements as 90,
// "pace+10" or "default", with trailing "default" elements dropped as the
// file writes them (owner, round 5).
func wireCap(c core.DocChoice) []any {
	if !c.Capped {
		return nil
	}
	n := len(c.Cap)
	for n > 0 && c.Cap[n-1].Kind == core.CapDefault {
		n--
	}
	out := []any{}
	for _, e := range c.Cap[:n] {
		if e.Kind == core.CapFixed {
			out = append(out, e.N)
		} else {
			out = append(out, e.String())
		}
	}
	return out
}

func wireModels(ms []adapter.Model) []web.Model {
	out := []web.Model{}
	for _, m := range ms {
		out = append(out, web.Model{ID: m.ID, Thinking: list(m.Thinking)})
	}
	return out
}

// windowLabels are the labels of the windows that apply to model, in the
// order its cap positions map to them.
func windowLabels(ws []adapter.Window, model string) []string {
	out := []string{}
	for _, w := range core.OrderWindows(ws, model) {
		out = append(out, w.Label)
	}
	return out
}

// usageRow is the ls --json row (contract 1–3, ruling 2): the v1 state
// text, or "no reading yet" for an account with nothing saved and nothing
// blocking a first reading; can_refresh false only while rate-limited or
// the token is expired; windows after their resets.
func usageRow(name, vendor string, st core.AccountState, now time.Time) web.UsageRow {
	row := web.UsageRow{Account: name, Vendor: vendor, State: app.StateText(st), Windows: []web.UsageWindow{}}
	row.CanRefresh = !st.TokenExpired && st.RetryUntil.IsZero()
	if st.Reading == nil {
		if row.CanRefresh && st.Err == nil {
			row.State = "no reading yet"
		}
		return row
	}
	age := 0
	if d := now.Sub(st.Reading.TakenAt); d > 0 {
		age = int(d / time.Second)
	}
	row.ReadingAgeSeconds = &age
	for _, w := range core.ApplyResets(*st.Reading, now).Windows {
		uw := web.UsageWindow{Label: w.Label, Length: core.FormatLength(w.Length), UsedPct: w.UsedPct}
		if !w.ResetsAt.IsZero() {
			uw.ResetsAt = w.ResetsAt.UTC().Format(time.RFC3339)
		}
		if scope := w.ScopeModelID; scope != "" || w.ScopeModelName != "" {
			if scope == "" {
				scope = w.ScopeModelName
			}
			uw.Scope = &scope
		}
		row.Windows = append(row.Windows, uw)
	}
	return row
}

func wireFindings(fs []app.Finding) []web.Finding {
	out := []web.Finding{}
	for _, f := range fs {
		out = append(out, web.Finding{Severity: f.Severity, Subject: f.Subject, Message: f.Message})
	}
	return out
}

// wireDoctor counts the findings it lists (contract 6).
func wireDoctor(fs []app.Finding) web.Doctor {
	d := web.Doctor{Findings: wireFindings(fs)}
	for _, f := range d.Findings {
		if f.Severity == "error" {
			d.Errors++
		} else {
			d.Warnings++
		}
	}
	return d
}
