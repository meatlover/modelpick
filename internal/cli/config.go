package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
)

// configChoice is one profile entry. Cap is as written; Uncapped marks a
// choice without a cap (per-window positions need readings, which `config`
// never fetches).
type configChoice struct {
	Account     string          `json:"account"`
	Model       string          `json:"model"`
	Thinking    *string         `json:"thinking"`
	Cap         *[]core.CapElem `json:"cap,omitempty"`
	Uncapped    bool            `json:"uncapped,omitempty"`
	SkipDuring  []string        `json:"skip_during,omitempty"`
	AllowDuring []string        `json:"allow_during,omitempty"`
}

// configGroup is a load balancing group in the file's shape.
type configGroup struct {
	LoadBalance string         `json:"load_balance"`
	Choices     []configChoice `json:"choices"`
}

type configSettings struct {
	MinCallIntervalMinutes float64 `json:"min_call_interval_minutes"`
	DefaultCap             float64 `json:"default_cap"`
}

type configOutput struct {
	Settings  configSettings             `json:"settings"`
	Accounts  map[string]adapter.Account `json:"accounts"`
	Schedules json.RawMessage            `json:"schedules"`
	Profiles  map[string][]any           `json:"profiles"`
}

// runConfig is `modelpick config`: the effective config as JSON. No network
// and no store access.
func runConfig(args []string, stdout, stderr io.Writer, d app.Deps) int {
	if len(args) > 0 {
		return badUsage(stderr, fmt.Sprintf("config: unexpected argument %q", args[0]))
	}
	if d.Home == "" {
		fmt.Fprintf(stderr, "modelpick: %v\n", app.ErrNoHome)
		return 1
	}
	path := platform.ConfigPath(d.Getenv, d.Home)
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: read config: %v\n", err)
		return 1
	}
	cfg, err := core.Parse(data, path, d.Home)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: %v\n", err)
		return 1
	}
	// Schedules are shown as written; the parsed form is not invertible.
	var written struct {
		Schedules json.RawMessage `json:"schedules"`
	}
	_ = json.Unmarshal(data, &written) // Parse already validated data
	if written.Schedules == nil {
		written.Schedules = json.RawMessage("{}")
	}
	out := configOutput{
		Settings: configSettings{
			MinCallIntervalMinutes: cfg.Settings.MinCallInterval.Minutes(),
			DefaultCap:             cfg.Settings.DefaultCap,
		},
		Accounts:  cfg.Accounts,
		Schedules: written.Schedules,
		Profiles:  map[string][]any{},
	}
	for name, choices := range cfg.Profiles {
		out.Profiles[name] = configItems(choices)
	}
	return writeJSON(stdout, stderr, out)
}

// configItems is a profile's list in the file's shape: a plain choice, or a
// group holding its members.
func configItems(choices []core.Choice) []any {
	groups, lbs := make([]int, len(choices)), make([]string, len(choices))
	for i, c := range choices {
		groups[i], lbs[i] = c.Group, c.LoadBalance
	}
	var items []any
	for _, pos := range core.Positions(groups, lbs) {
		members := make([]configChoice, 0, pos.End-pos.Start)
		for _, c := range choices[pos.Start:pos.End] {
			members = append(members, newConfigChoice(c))
		}
		if groups[pos.Start] == 0 {
			items = append(items, members[0])
			continue
		}
		items = append(items, configGroup{LoadBalance: pos.LoadBalance, Choices: members})
	}
	return items
}

func newConfigChoice(c core.Choice) configChoice {
	cc := configChoice{Account: c.Account, Model: c.Model, Thinking: c.Thinking,
		Uncapped: !c.Capped, SkipDuring: c.SkipDuring, AllowDuring: c.AllowDuring}
	if c.Capped {
		capCopy := append([]core.CapElem{}, c.Cap...)
		cc.Cap = &capCopy
	}
	return cc
}
