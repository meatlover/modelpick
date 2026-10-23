package core

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/pathx"
)

// ReservedAccountName cannot name an account: it is the key under which the
// config API lists models.
const ReservedAccountName = "models"

//go:embed defaults.json
var defaultsJSON []byte

// Settings are the global knobs; omitted values come from the embedded defaults.
type Settings struct {
	MinCallInterval time.Duration
	DefaultCap      float64
}

// Choice is one ranked entry of a profile.
type Choice struct {
	Account    string
	Model      string
	Thinking   *string   // nil = JSON null
	Cap        []CapElem // meaningful only when Capped
	Capped     bool      // false = "cap" key absent → no limit
	SkipDuring []string
}

// Config is a validated modelpick configuration.
type Config struct {
	Path      string
	Settings  Settings
	Accounts  map[string]adapter.Account // Name filled, all paths home-expanded
	Schedules map[string]Schedule
	Profiles  map[string][]Choice
}

// ConfigError is a validation failure located by a JSON-path Field.
type ConfigError struct{ Path, Field, Msg string }

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config %s: %s: %s", e.Path, e.Field, e.Msg)
}

// ProfileNames returns the profile names, sorted.
func (c *Config) ProfileNames() []string { return sortedKeys(c.Profiles) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Load reads and parses the config file at path.
func Load(path, home string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data, path, home)
}

type rawSettings struct {
	MinCallIntervalMinutes *float64 `json:"min_call_interval_minutes"`
	DefaultCap             *float64 `json:"default_cap"`
}

type rawSchedule struct {
	Days []string `json:"days"`
	UTC  []string `json:"utc"`
}

type rawChoice struct {
	Account    *string         `json:"account"`
	Model      *string         `json:"model"`
	Thinking   json.RawMessage `json:"thinking"`
	Cap        json.RawMessage `json:"cap"`
	SkipDuring []string        `json:"skip_during"`
}

type rawTop struct {
	Version   *int                       `json:"version"`
	Settings  *rawSettings               `json:"settings"`
	Accounts  map[string]json.RawMessage `json:"accounts"`
	Schedules map[string]json.RawMessage `json:"schedules"`
	Profiles  map[string]json.RawMessage `json:"profiles"`
}

// parser carries the path so every failure becomes a *ConfigError.
type parser struct{ path string }

func (p parser) fail(field, format string, args ...any) error {
	return &ConfigError{Path: p.path, Field: field, Msg: fmt.Sprintf(format, args...)}
}

// strict decodes raw into v rejecting unknown keys; prefix locates the object.
func (p parser) strict(raw []byte, v any, prefix string) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if dec.More() {
			return p.fail(prefix, "unexpected data after JSON value")
		}
		return nil
	}
	if key, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return p.fail(join(prefix, strings.Trim(key, `"`)), "unknown key")
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return p.fail(join(prefix, te.Field), "must be %s", jsonKind(te.Type.Kind()))
	}
	return p.fail(prefix, "invalid JSON: %v", err)
}

func jsonKind(k reflect.Kind) string {
	switch k {
	case reflect.Slice, reflect.Array:
		return "an array"
	case reflect.Map, reflect.Struct, reflect.Pointer:
		return "an object"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	}
	return "a number"
}

func join(prefix, field string) string {
	switch {
	case prefix == "":
		return field
	case field == "":
		return prefix
	}
	return prefix + "." + field
}

// Parse validates data and returns the Config. home expands "~" in paths.
func Parse(data []byte, path, home string) (*Config, error) {
	p := parser{path: path}
	var top rawTop
	if err := p.strict(data, &top, ""); err != nil {
		return nil, err
	}
	if top.Version == nil || *top.Version != 1 {
		return nil, p.fail("version", "must be 1")
	}
	cfg := &Config{Path: path}
	var err error
	if cfg.Settings, err = p.settings(top.Settings); err != nil {
		return nil, err
	}
	if cfg.Accounts, err = p.accounts(top.Accounts, home); err != nil {
		return nil, err
	}
	if cfg.Schedules, err = p.schedules(top.Schedules); err != nil {
		return nil, err
	}
	if cfg.Profiles, err = p.profiles(top.Profiles, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (p parser) settings(raw *rawSettings) (Settings, error) {
	var d rawSettings
	if err := json.Unmarshal(defaultsJSON, &d); err != nil || d.MinCallIntervalMinutes == nil || d.DefaultCap == nil {
		panic("core: embedded defaults.json is malformed")
	}
	minutes, capPct := *d.MinCallIntervalMinutes, *d.DefaultCap
	if raw != nil {
		if raw.MinCallIntervalMinutes != nil {
			if *raw.MinCallIntervalMinutes < 0 {
				return Settings{}, p.fail("settings.min_call_interval_minutes", "must not be negative")
			}
			minutes = *raw.MinCallIntervalMinutes
		}
		if raw.DefaultCap != nil {
			if *raw.DefaultCap < 0 || *raw.DefaultCap > 100 {
				return Settings{}, p.fail("settings.default_cap", "must be between 0 and 100")
			}
			capPct = *raw.DefaultCap
		}
	}
	return Settings{MinCallInterval: time.Duration(minutes * float64(time.Minute)), DefaultCap: capPct}, nil
}

func (p parser) accounts(raws map[string]json.RawMessage, home string) (map[string]adapter.Account, error) {
	out := map[string]adapter.Account{}
	for _, name := range sortedKeys(raws) {
		prefix := "accounts." + name
		if name == ReservedAccountName {
			return nil, p.fail(prefix, "reserved name")
		}
		var a adapter.Account
		if err := p.strict(raws[name], &a, prefix); err != nil {
			return nil, err
		}
		a.Name = name
		if err := p.checkVendorFields(a, prefix); err != nil {
			return nil, err
		}
		a.ConfigDir = pathx.ExpandHome(a.ConfigDir, home)
		a.CodexHome = pathx.ExpandHome(a.CodexHome, home)
		a.KeyFile = pathx.ExpandHome(a.KeyFile, home)
		out[name] = a
	}
	return out, nil
}

func (p parser) checkVendorFields(a adapter.Account, prefix string) error {
	type field struct {
		key string
		set bool
	}
	fields := []field{
		{"config_dir", a.ConfigDir != ""},
		{"codex_home", a.CodexHome != ""},
		{"key_file", a.KeyFile != ""},
		{"key_line", a.KeyLine != 0},
	}
	var need []string
	switch a.Vendor {
	case "claude":
		need = []string{"config_dir"}
	case "codex":
		need = []string{"codex_home"}
	case "opencode-go":
		need = []string{"key_file", "key_line"}
	default:
		return p.fail(join(prefix, "vendor"), "unknown vendor %q", a.Vendor)
	}
	for _, f := range fields {
		wanted := false
		for _, n := range need {
			wanted = wanted || n == f.key
		}
		switch {
		case wanted && !f.set:
			return p.fail(join(prefix, f.key), "required for vendor %q", a.Vendor)
		case !wanted && f.set:
			return p.fail(join(prefix, f.key), "not valid for vendor %q", a.Vendor)
		}
	}
	if a.Vendor == "opencode-go" && a.KeyLine < 1 {
		return p.fail(join(prefix, "key_line"), "must be >= 1")
	}
	// Ruling 26: a relative path would be relative to whatever directory
	// modelpick runs in; refuse it rather than guess.
	for _, f := range []struct{ key, val string }{
		{"config_dir", a.ConfigDir}, {"codex_home", a.CodexHome}, {"key_file", a.KeyFile},
	} {
		if f.val != "" && !filepath.IsAbs(f.val) && f.val != "~" && !strings.HasPrefix(f.val, "~/") {
			return p.fail(join(prefix, f.key), "must be an absolute path or start with ~")
		}
	}
	return nil
}

func (p parser) schedules(raws map[string]json.RawMessage) (map[string]Schedule, error) {
	out := map[string]Schedule{}
	for _, name := range sortedKeys(raws) {
		prefix := "schedules." + name
		var rs rawSchedule
		if err := p.strict(raws[name], &rs, prefix); err != nil {
			return nil, err
		}
		if len(rs.Days) == 0 {
			return nil, p.fail(join(prefix, "days"), "must not be empty")
		}
		if len(rs.UTC) == 0 {
			return nil, p.fail(join(prefix, "utc"), "must not be empty")
		}
		// ParseSchedule reports no index, so locate the bad element one by one.
		for i, d := range rs.Days {
			if _, err := ParseSchedule([]string{d}, nil); err != nil {
				return nil, p.fail(fmt.Sprintf("%s.days[%d]", prefix, i), "%v", err)
			}
		}
		for i, r := range rs.UTC {
			if _, err := ParseSchedule(nil, []string{r}); err != nil {
				return nil, p.fail(fmt.Sprintf("%s.utc[%d]", prefix, i), "%v", err)
			}
		}
		s, err := ParseSchedule(rs.Days, rs.UTC)
		if err != nil {
			return nil, p.fail(prefix, "%v", err)
		}
		out[name] = s
	}
	return out, nil
}

func (p parser) profiles(raws map[string]json.RawMessage, cfg *Config) (map[string][]Choice, error) {
	out := map[string][]Choice{}
	for _, name := range sortedKeys(raws) {
		prefix := "profiles." + name
		var items []json.RawMessage
		if err := p.strict(raws[name], &items, prefix); err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, p.fail(prefix, "must not be empty")
		}
		choices := make([]Choice, 0, len(items))
		for i, item := range items {
			c, err := p.choice(item, fmt.Sprintf("%s[%d]", prefix, i), cfg)
			if err != nil {
				return nil, err
			}
			choices = append(choices, c)
		}
		out[name] = choices
	}
	return out, nil
}

func (p parser) choice(raw json.RawMessage, prefix string, cfg *Config) (Choice, error) {
	var rc rawChoice
	if err := p.strict(raw, &rc, prefix); err != nil {
		return Choice{}, err
	}
	var c Choice
	if rc.Account == nil || *rc.Account == "" {
		return c, p.fail(join(prefix, "account"), "required")
	}
	if _, ok := cfg.Accounts[*rc.Account]; !ok {
		return c, p.fail(join(prefix, "account"), "unknown account %q", *rc.Account)
	}
	c.Account = *rc.Account
	if rc.Model == nil || *rc.Model == "" {
		return c, p.fail(join(prefix, "model"), "required")
	}
	c.Model = *rc.Model
	if rc.Thinking == nil {
		return c, p.fail(join(prefix, "thinking"), "required (use null for none)")
	}
	if string(bytes.TrimSpace(rc.Thinking)) != "null" {
		var s string
		if err := json.Unmarshal(rc.Thinking, &s); err != nil {
			return c, p.fail(join(prefix, "thinking"), "must be a string or null")
		}
		c.Thinking = &s
	}
	if rc.Cap != nil {
		if string(bytes.TrimSpace(rc.Cap)) == "null" {
			return c, p.fail(join(prefix, "cap"), "cap must not be null: omit cap for no limit")
		}
		c.Capped = true
		var items []json.RawMessage
		if err := json.Unmarshal(rc.Cap, &items); err != nil {
			return c, p.fail(join(prefix, "cap"), "must be an array")
		}
		c.Cap = make([]CapElem, 0, len(items))
		for j, item := range items {
			e, err := ParseCapElem(item)
			if err != nil {
				return c, p.fail(fmt.Sprintf("%s.cap[%d]", prefix, j), "%s", err.Error())
			}
			c.Cap = append(c.Cap, e)
		}
	}
	for _, s := range rc.SkipDuring {
		if _, ok := cfg.Schedules[s]; !ok {
			return c, p.fail(join(prefix, "skip_during"), "unknown schedule %q", s)
		}
	}
	c.SkipDuring = rc.SkipDuring
	return c, nil
}
