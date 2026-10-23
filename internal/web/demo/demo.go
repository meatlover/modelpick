// Package demo is an in-memory web.Backend with a fixed fixture, used to
// design and test the page without touching any real config or vendor.
package demo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/meatlover/modelpick/internal/web"
)

const configPath = "~/.config/modelpick/config.json"

// reading is one account's stored usage reading.
type reading struct {
	state      string
	canRefresh bool
	hasReading bool
	readAt     time.Time
	windows    []window
}

type window struct {
	label   string
	length  time.Duration
	used    float64
	resetAt time.Time
	scope   *string
}

// Demo is the in-memory backend. Create it with New.
type Demo struct {
	mu         sync.Mutex
	now        func() time.Time
	counter    int
	readOnly   string
	hasError   bool
	defaultCap float64
	accounts   []web.Account
	models     map[string][]web.Model
	windows    map[string]map[string][]string
	schedules  []web.Schedule
	profiles   []web.Profile
	readings   map[string]*reading
}

// New returns a Demo whose readings and resets are relative to now().
func New(now func() time.Time) *Demo {
	d := &Demo{now: now, counter: 1, defaultCap: 95}
	d.buildAccounts()
	d.buildReadings(now())
	d.buildModels()
	d.buildSchedules()
	d.buildProfiles()
	return d
}

func str(s string) *string { return &s }

func (d *Demo) buildAccounts() {
	acct := func(name, vendor, loc, src string, f map[string]any) web.Account {
		return web.Account{Name: name, Vendor: vendor, Fields: f, Location: loc, Source: src}
	}
	goAcct := func(n int) web.Account {
		return acct("go-"+strconv.Itoa(n), "opencode-go", "", "",
			map[string]any{"key_file": "~/.ssh/opencode-go-api", "key_line": float64(n)})
	}
	d.accounts = []web.Account{
		acct("claude-main", "claude", "~/.claude", "Keychain: Claude Code-credentials",
			map[string]any{"config_dir": "~/.claude"}),
		acct("claude-rec", "claude", "~/.claude-rec", "Keychain: Claude Code-credentials-6a4e8c1e",
			map[string]any{"config_dir": "~/.claude-rec"}),
		acct("codex-main", "codex", "~/.codex", "file: ~/.codex/auth.json",
			map[string]any{"codex_home": "~/.codex"}),
		goAcct(1), goAcct(2), goAcct(3),
	}
	d.relabel()
}

func vendorModels(vendor string) []web.Model {
	mk := func(think []string, ids ...string) []web.Model {
		out := []web.Model{}
		for _, id := range ids {
			out = append(out, web.Model{ID: id, Thinking: append([]string{}, think...)})
		}
		return out
	}
	switch vendor {
	case "claude":
		return mk([]string{"low", "medium", "high", "xhigh", "max"},
			"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1", "claude-haiku-5-5")
	case "codex":
		return mk([]string{"low", "medium", "high", "xhigh"}, "gpt-6.1-sol", "gpt-6.1-sol-mini")
	}
	return mk(nil, "opencode-go/deepseek-v4.1-flash", "opencode-go/deepseek-v4.1")
}

func vendorWindows(vendor, model string) []string {
	switch {
	case vendor == "claude" && model == "claude-fable-5-1":
		return []string{"5h", "7d", "7d Fable"}
	case vendor == "claude":
		return []string{"5h", "7d"}
	case vendor == "codex":
		return []string{"7d"}
	}
	return []string{"5h", "7d", "30d"}
}

func (d *Demo) buildModels() {
	d.models = map[string][]web.Model{}
	d.windows = map[string]map[string][]string{}
	for _, a := range d.accounts {
		d.addAccountModels(a)
	}
}

func (d *Demo) addAccountModels(a web.Account) {
	d.models[a.Name] = vendorModels(a.Vendor)
	for _, m := range d.models[a.Name] {
		// windows[account] exists only for accounts with a stored reading.
		if r := d.readings[a.Name]; r == nil || !r.hasReading {
			continue
		}
		if w := vendorWindows(a.Vendor, m.ID); w != nil {
			if d.windows[a.Name] == nil {
				d.windows[a.Name] = map[string][]string{}
			}
			d.windows[a.Name][m.ID] = w
		}
	}
}

func (d *Demo) buildSchedules() {
	peak := web.Schedule{Name: "deepseek-peak"}
	for day := 1; day <= 5; day++ {
		for _, h := range []int{1, 2, 3, 6, 7, 8, 9} {
			peak.Cells[day][h] = 1
		}
	}
	d.schedules = []web.Schedule{peak}
}

func (d *Demo) buildProfiles() {
	ch := func(acct, model, think string, capv []any, skip ...string) web.Choice {
		c := web.Choice{Account: acct, Model: model, Cap: capv,
			SkipDuring: append([]string{}, skip...), AllowDuring: []string{}}
		if think != "" {
			c.Thinking = str(think)
		}
		return c
	}
	flash := "opencode-go/deepseek-v4.1-flash"
	peak := "deepseek-peak"
	d.profiles = []web.Profile{
		{Name: "coder-difficult", Choices: []web.Choice{
			ch("claude-main", "claude-opus-5-5", "high", []any{90.0, "pace+10"}),
			ch("codex-main", "gpt-6.1-sol", "xhigh", []any{"pace+5"}),
			ch("claude-main", "claude-fable-5-1", "high", []any{90.0, 98.0, 95.0}, peak),
			ch("claude-rec", "claude-sonnet-5-5", "medium", nil),
		}},
		{Name: "tester", Choices: []web.Choice{
			ch("go-1", flash, "", []any{}, peak),
			ch("go-2", flash, "", []any{}, peak),
			ch("go-3", flash, "", []any{}, peak),
			ch("claude-main", "claude-sonnet-5-5", "medium", nil),
		}},
	}
}

func (d *Demo) buildReadings(t0 time.Time) {
	at := func(dd time.Duration) time.Time { return t0.Add(dd) }
	day := 24 * time.Hour
	mk := func(age time.Duration, state string, refresh bool, w ...window) *reading {
		return &reading{state: state, canRefresh: refresh, hasReading: true, readAt: t0.Add(-age), windows: w}
	}
	w := func(label string, l time.Duration, used float64, reset time.Time) window {
		return window{label: label, length: l, used: used, resetAt: reset}
	}
	goWin := func(u5, u7, u30 float64) []window {
		return []window{w("5h", 5*time.Hour, u5, at(4*time.Hour)),
			w("7d", 7*day, u7, at(3*day+6*time.Hour+50*time.Minute)),
			w("30d", 30*day, u30, at(5*day+16*time.Hour+50*time.Minute))}
	}
	fable := w("7d Fable", 7*day, 12, at(2*day+11*time.Hour+50*time.Minute))
	fable.scope = str("claude-fable-5-1")
	d.readings = map[string]*reading{
		"claude-main": mk(4*time.Minute, "ok", true,
			w("5h", 5*time.Hour, 31, at(time.Hour+50*time.Minute)),
			w("7d", 7*day, 58, at(2*day+11*time.Hour+50*time.Minute)), fable),
		"claude-rec": {state: "expired token"},
		"codex-main": mk(12*time.Minute, "ok", true, w("7d", 7*day, 41, at(2*day+50*time.Minute))),
		"go-1":       mk(9*time.Minute, "ok", true, goWin(0, 62, 95)...),
		"go-2":       mk(9*time.Minute, "ok", true, goWin(3, 70, 96)...),
		"go-3": mk(38*time.Minute, "rate-limited until "+at(32*time.Minute).UTC().Format(time.RFC3339),
			false, goWin(0, 100, 81)...),
	}
}

// freshWindows is a first reading for vendor taken at t0: the fixture's
// windows for that vendor, lightly used, each resetting a full length on.
func freshWindows(vendor string, t0 time.Time) []window {
	day := 24 * time.Hour
	w := func(label string, l time.Duration, used float64) window {
		return window{label: label, length: l, used: used, resetAt: t0.Add(l)}
	}
	switch vendor {
	case "claude":
		fable := w("7d Fable", 7*day, 0)
		fable.scope = str("claude-fable-5-1")
		return []window{w("5h", 5*time.Hour, 2), w("7d", 7*day, 1), fable}
	case "codex":
		return []window{w("7d", 7*day, 1)}
	}
	return []window{w("5h", 5*time.Hour, 0), w("7d", 7*day, 1), w("30d", 30*day, 1)}
}

func (d *Demo) sha() string {
	sum := sha256.Sum256([]byte(strconv.Itoa(d.counter)))
	return hex.EncodeToString(sum[:])
}

// bump gives the config a new identity, as any write to the file would.
func (d *Demo) bump() { d.counter++ }

func (d *Demo) sortAll() {
	sort.Slice(d.accounts, func(i, j int) bool { return d.accounts[i].Name < d.accounts[j].Name })
	sort.Slice(d.schedules, func(i, j int) bool { return d.schedules[i].Name < d.schedules[j].Name })
	sort.Slice(d.profiles, func(i, j int) bool { return d.profiles[i].Name < d.profiles[j].Name })
}

// State returns a deep copy of the current config.
func (d *Demo) State() web.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stateLocked()
}

func (d *Demo) stateLocked() web.State {
	s := web.State{SHA256: d.sha(), Path: configPath, ReadOnly: d.readOnly,
		DefaultCap: d.defaultCap, Accounts: d.accounts, Models: d.models,
		Windows: d.windows, Schedules: d.schedules, Profiles: d.profiles}
	raw, err := json.Marshal(s)
	if err != nil {
		panic(err) // the wire types always marshal
	}
	var out web.State
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func (d *Demo) row(a web.Account) web.UsageRow {
	r := d.readings[a.Name]
	row := web.UsageRow{Account: a.Name, Vendor: a.Vendor, State: "no reading yet",
		Windows: []web.UsageWindow{}}
	if r == nil {
		row.CanRefresh = true // nothing blocks a first reading
		return row
	}
	row.State, row.CanRefresh = r.state, r.canRefresh
	if r.hasReading {
		age := int(d.now().Sub(r.readAt).Seconds())
		row.ReadingAgeSeconds = &age
	}
	for _, w := range r.windows {
		row.Windows = append(row.Windows, web.UsageWindow{Label: w.label,
			Length: w.length.String(), UsedPct: w.used,
			ResetsAt: w.resetAt.UTC().Format(time.RFC3339), Scope: w.scope})
	}
	return row
}

// Usage returns one row per account.
func (d *Demo) Usage() []web.UsageRow {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows := []web.UsageRow{}
	for _, a := range d.accounts {
		rows = append(rows, d.row(a))
	}
	return rows
}

// Refresh pretends to call the vendor: 600 ms, then age 0 when allowed.
func (d *Demo) Refresh(account string) (web.UsageRow, error) {
	time.Sleep(600 * time.Millisecond)
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, a := range d.accounts {
		if a.Name != account {
			continue
		}
		r := d.readings[account]
		if r == nil {
			// A first reading: fixture-like windows, and windows[account]
			// appears as it does for an account with a stored reading.
			d.readings[account] = &reading{state: "ok", canRefresh: true, hasReading: true,
				readAt: d.now(), windows: freshWindows(a.Vendor, d.now())}
			d.addAccountModels(a)
			return d.row(a), nil
		}
		if !r.canRefresh {
			return web.UsageRow{}, web.Refused{Reason: account + ": " + r.state}
		}
		r.readAt = d.now()
		return d.row(a), nil
	}
	return web.UsageRow{}, web.Refused{Reason: "unknown account " + account}
}

func (d *Demo) findings() []web.Finding {
	out := []web.Finding{}
	if d.hasError {
		out = append(out, web.Finding{Severity: "error", Subject: "go-3",
			Message: "key line 3 missing in ~/.ssh/opencode-go-api"})
	}
	return append(out, web.Finding{Severity: "warning", Subject: "claude-rec",
		Message: "token expired; open Claude Code with this account to refresh its login"})
}

// Doctor returns the current findings.
func (d *Demo) Doctor() web.Doctor {
	d.mu.Lock()
	defer d.mu.Unlock()
	doc := web.Doctor{Findings: d.findings()}
	for _, f := range doc.Findings {
		if f.Severity == "error" {
			doc.Errors++
		} else {
			doc.Warnings++
		}
	}
	return doc
}

// HandEdit changes the state as an edit of the file by hand would.
func (d *Demo) HandEdit() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.profiles {
		if d.profiles[i].Name != "tester" || len(d.profiles[i].Choices) < 4 {
			continue
		}
		c := &d.profiles[i].Choices[3]
		// Only a level the model offers keeps the state valid.
		if contains(d.modelThinking(c.Account, c.Model), "low") {
			c.Thinking = str("low")
		}
	}
	d.bump()
}

// SetBroken marks the file unparseable with msg; "" restores it.
func (d *Demo) SetBroken(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readOnly = msg
	d.bump()
}

// AddError adds the go-3 error finding.
func (d *Demo) AddError() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hasError = true
}
