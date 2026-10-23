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
	for _, a := range args {
		switch a {
		case "--refresh":
			refresh = true
		case "--json":
			asJSON = true
		default:
			return badUsage(stderr, fmt.Sprintf("ls: unexpected argument %q", a))
		}
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: %v\n", err)
		return 1
	}
	rows := lsRows(cfg, app.NewEnv(d, cfg, refresh), d.Now())
	if asJSON {
		return writeJSON(stdout, stderr, rows)
	}
	writeLsTable(stdout, rows)
	return 0
}

// lsRows reads every account, sorted by name, through the usage store.
func lsRows(cfg *core.Config, e *app.Env, now time.Time) []lsAccount {
	names := make([]string, 0, len(cfg.Accounts))
	for n := range cfg.Accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	rows := make([]lsAccount, 0, len(names))
	for _, n := range names {
		st := e.Usage(n)
		row := lsAccount{Account: n, Vendor: cfg.Accounts[n].Vendor, State: app.StateText(st), Windows: []lsWindow{}}
		if st.Reading != nil {
			age := int64(usage.Age(st.Reading.TakenAt, now) / time.Second)
			row.AgeSeconds = &age
			for _, w := range core.ApplyResets(*st.Reading, now).Windows {
				lw := lsWindow{Label: w.Label, Length: core.FormatLength(w.Length), UsedPct: w.UsedPct}
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

func writeLsTable(w io.Writer, rows []lsAccount) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACCOUNT\tVENDOR\tWINDOW\tUSED\tRESETS\tAGE\tSTATE")
	for _, r := range rows {
		age := "-"
		if r.AgeSeconds != nil {
			age = formatAge(time.Duration(*r.AgeSeconds) * time.Second)
		}
		if len(r.Windows) == 0 {
			fmt.Fprintf(tw, "%s\t%s\t-\t-\t-\t%s\t%s\n", r.Account, r.Vendor, age, r.State)
		}
		for _, win := range r.Windows {
			label := fmt.Sprintf("%s (%s)", win.Label, win.Length)
			if win.Scope != nil {
				label += " " + *win.Scope
			}
			resets := "-"
			if win.ResetsAt != nil {
				resets = *win.ResetsAt
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%g%%\t%s\t%s\t%s\n", r.Account, r.Vendor, label, win.UsedPct, resets, age, r.State)
		}
	}
	_ = tw.Flush()
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
		fmt.Fprintf(stderr, "modelpick: %v\n", err)
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
