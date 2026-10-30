package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/usage"
)

// lsWindow and lsAccount are the `ls --json` shapes.
type lsWindow struct {
	Label    string  `json:"label"`
	Length   string  `json:"length"`
	UsedPct  float64 `json:"used_pct"`
	ResetsAt *string `json:"resets_at"`
	Scope    *string `json:"scope"`

	resetsAt time.Time `json:"-"` // for the table; zero when unknown
}

type lsAccount struct {
	Account    string     `json:"account"`
	Vendor     string     `json:"vendor"`
	State      string     `json:"state"`
	AgeSeconds *int64     `json:"reading_age_seconds"`
	Windows    []lsWindow `json:"windows"`
}

// runLs dispatches `ls` and `ls models`.
func runLs(args []string, stdout, stderr io.Writer, d app.Deps) int {
	if len(args) > 0 && args[0] == "models" {
		return runLsModels(args[1:], stdout, stderr, d)
	}
	var refresh, asJSON bool
	var named []string
	for _, a := range args {
		switch {
		case a == "--refresh":
			refresh = true
		case a == "--json":
			asJSON = true
		case strings.HasPrefix(a, "-"):
			return badUsage(stderr, fmt.Sprintf("ls: unexpected argument %q", a))
		default:
			named = append(named, a)
		}
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: %s\n", configErrText(err, d))
		return 1
	}
	names, err := lsNames(cfg, named)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: %v\n", err)
		return 1
	}
	rows := lsRows(cfg, app.NewEnv(d, cfg, refresh), names, d.Now())
	if asJSON {
		return writeJSON(stdout, stderr, rows)
	}
	writeLsTable(stdout, rows, d.Now())
	return 0
}

// lsNames is the accounts to show: the named ones in order without
// duplicates, or every account sorted when none were named. An unknown name
// is an error before any account is read.
func lsNames(cfg *core.Config, named []string) ([]string, error) {
	all := make([]string, 0, len(cfg.Accounts))
	for n := range cfg.Accounts {
		all = append(all, n)
	}
	sort.Strings(all)
	if len(named) == 0 {
		return all, nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(named))
	for _, n := range named {
		if _, ok := cfg.Accounts[n]; !ok {
			return nil, fmt.Errorf("ls: unknown account %q; configured: %s", n, strings.Join(all, ", "))
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// lsRows reads the given accounts, in order, through the usage store.
func lsRows(cfg *core.Config, e *app.Env, names []string, now time.Time) []lsAccount {
	rows := make([]lsAccount, 0, len(names))
	for _, n := range names {
		st := e.Usage(n)
		row := lsAccount{Account: n, Vendor: cfg.Accounts[n].Vendor, State: app.StateText(st), Windows: []lsWindow{}}
		if st.Reading != nil {
			age := int64(usage.Age(st.Reading.TakenAt, now) / time.Second)
			row.AgeSeconds = &age
			for _, w := range core.ApplyResets(*st.Reading, now).Windows {
				lw := lsWindow{Label: w.Label, Length: core.FormatLength(w.Length), UsedPct: w.UsedPct, resetsAt: w.ResetsAt}
				if !w.ResetsAt.IsZero() {
					s := w.ResetsAt.UTC().Format(time.RFC3339)
					lw.ResetsAt = &s
				}
				if scope := cmpOr(w.ScopeModelID, w.ScopeModelName); scope != "" {
					lw.Scope = &scope
				}
				row.Windows = append(row.Windows, lw)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// writeLsTable prints the v2 table: STATE second, one line per window with
// the account, state and age on the account's first line only, RESETS as time
// left and time. An errored account shows "error" in STATE and its message
// goes after the table, one "<account>: <message>" line each.
func writeLsTable(w io.Writer, rows []lsAccount, now time.Time) {
	leftW, resetW := resetWidths(rows, now)
	table := [][]string{{"ACCOUNT", "STATE", "WINDOW", "USED", "RESETS", "AGE"}}
	var errLines []string
	for _, r := range rows {
		state := r.State
		if m, ok := strings.CutPrefix(r.State, "error: "); ok {
			state = "error"
			errLines = append(errLines, r.Account+": "+m)
		}
		age := "-"
		if r.AgeSeconds != nil {
			age = formatAge(time.Duration(*r.AgeSeconds) * time.Second)
		}
		if len(r.Windows) == 0 {
			table = append(table, []string{r.Account, state, "-", "-", "-", age})
			continue
		}
		for i, win := range r.Windows {
			// A continuation line keeps an empty AGE cell, so its RESETS cell is
			// never the line's last and always counts toward the column width.
			first, last := []string{r.Account, state}, age
			if i > 0 {
				first, last = []string{"", ""}, ""
			}
			cells := append(first, windowCell(win), fmt.Sprintf("%g%%", win.UsedPct), resetsCell(win, now, leftW, resetW))
			table = append(table, append(cells, last))
		}
	}
	writeColumns(w, table, map[int]bool{3: true})
	if len(errLines) > 0 {
		fmt.Fprintln(w)
		for _, l := range errLines {
			fmt.Fprintln(w, l)
		}
	}
}

// windowCell is the length (5h, 7d), then the scope when the window is
// model-scoped; the vendor's label when the length is unknown.
func windowCell(win lsWindow) string {
	c := win.Length
	if c == "?" {
		c = win.Label
	}
	if win.Scope != nil {
		c += " " + *win.Scope
	}
	return c
}

// resetWidths is the widest fmtLeft and fmtReset over every window.
func resetWidths(rows []lsAccount, now time.Time) (left, reset int) {
	for _, r := range rows {
		for _, win := range r.Windows {
			if win.resetsAt.IsZero() {
				continue
			}
			left = max(left, len(fmtLeft(win.resetsAt, now)))
			reset = max(reset, len(fmtReset(win.resetsAt, now)))
		}
	}
	return left, reset
}

// resetsCell is time left padded to leftW, two spaces, then the time right-
// aligned to resetW; "-" for a window with no reset time.
func resetsCell(win lsWindow, now time.Time, leftW, resetW int) string {
	if win.resetsAt.IsZero() {
		return "-"
	}
	return fmt.Sprintf("%-*s  %*s", leftW, fmtLeft(win.resetsAt, now), resetW, fmtReset(win.resetsAt, now))
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	return core.FormatLength(d.Truncate(time.Minute))
}

// writeJSON prints v indented; a marshal failure is a bug, reported as exit 1.
func writeJSON(stdout, stderr io.Writer, v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", b)
	return 0
}

type lsModel struct {
	ID       string   `json:"id"`
	Thinking []string `json:"thinking"`
}

type lsModels struct {
	Account string    `json:"account"`
	Models  []lsModel `json:"models"`
	Error   string    `json:"error,omitempty"`
}

func runLsModels(args []string, stdout, stderr io.Writer, d app.Deps) int {
	var asJSON bool
	for _, a := range args {
		if a != "--json" {
			return badUsage(stderr, fmt.Sprintf("ls models: unexpected argument %q", a))
		}
		asJSON = true
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: %s\n", configErrText(err, d))
		return 1
	}
	e := app.NewEnv(d, cfg, false)
	names := make([]string, 0, len(cfg.Accounts))
	for n := range cfg.Accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]lsModels, 0, len(names))
	for _, n := range names {
		out = append(out, accountModels(e, n))
	}
	if asJSON {
		return writeJSON(stdout, stderr, out)
	}
	writeModelsTable(stdout, out)
	return 0
}

// accountModels lists one account's models; a failure becomes the entry's
// Error, with no models.
func accountModels(e *app.Env, name string) lsModels {
	entry := lsModels{Account: name, Models: []lsModel{}}
	ms, err := app.AccountModels(e, name)
	if err != nil {
		entry.Error = err.Error()
	}
	for _, m := range ms {
		th := m.Thinking
		if th == nil {
			th = []string{}
		}
		entry.Models = append(entry.Models, lsModel{ID: m.ID, Thinking: th})
	}
	return entry
}

func writeModelsTable(w io.Writer, out []lsModels) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACCOUNT\tMODEL\tTHINKING")
	for _, o := range out {
		if o.Error != "" {
			fmt.Fprintf(tw, "%s\terror: %s\t-\n", o.Account, o.Error)
		}
		for _, m := range o.Models {
			think := "-"
			if len(m.Thinking) > 0 {
				think = strings.Join(m.Thinking, ",")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", o.Account, m.ID, think)
		}
	}
	_ = tw.Flush()
}
