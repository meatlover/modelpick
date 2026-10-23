package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
)

// configChoice is one profile entry. Cap is as written; Uncapped marks a
// choice without a cap (per-window positions need readings, which `config`
// never fetches).
type configChoice struct {
	Account    string          `json:"account"`
	Model      string          `json:"model"`
	Thinking   *string         `json:"thinking"`
	Cap        *[]core.CapElem `json:"cap,omitempty"`
	Uncapped   bool            `json:"uncapped,omitempty"`
	SkipDuring []string        `json:"skip_during,omitempty"`
}

type configSettings struct {
	MinCallIntervalMinutes float64 `json:"min_call_interval_minutes"`
	DefaultCap             float64 `json:"default_cap"`
}

type configOutput struct {
	Settings  configSettings             `json:"settings"`
	Accounts  map[string]adapter.Account `json:"accounts"`
	Schedules json.RawMessage            `json:"schedules"`
	Profiles  map[string][]configChoice  `json:"profiles"`
}

// runConfig is `modelpick config`: the effective config as JSON. No network
// and no store access.
func runConfig(args []string, stdout, stderr io.Writer, d Deps) int {
	if len(args) > 0 {
		return badUsage(stderr, fmt.Sprintf("config: unexpected argument %q", args[0]))
	}
	if d.Home == "" {
		fmt.Fprintf(stderr, "modelpick: %v\n", errNoHome)
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
		Profiles:  map[string][]configChoice{},
	}
	for name, choices := range cfg.Profiles {
		cs := make([]configChoice, 0, len(choices))
		for _, c := range choices {
			cc := configChoice{Account: c.Account, Model: c.Model, Thinking: c.Thinking,
				Uncapped: !c.Capped, SkipDuring: c.SkipDuring}
			if c.Capped {
				capCopy := append([]core.CapElem{}, c.Cap...)
				cc.Cap = &capCopy
			}
			cs = append(cs, cc)
		}
		out.Profiles[name] = cs
	}
	return writeJSON(stdout, stderr, out)
}
