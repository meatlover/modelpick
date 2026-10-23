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
	// AllowDuring: usable only while any listed schedule matches. Never set
	// together with SkipDuring.
	AllowDuring []string
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
	if e.Path == "" { // from ParseDoc, which has no file name
		return fmt.Sprintf("config: %s: %s", e.Field, e.Msg)
	}
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
	doc, err := p.decodeDoc(data)
	if err != nil {
		return nil, err
	}
	return p.validate(doc, home)
}

// validate runs the cross-checks and expansions over a decoded Doc.
func (p parser) validate(doc *Doc, home string) (*Config, error) {
	if doc.Version != 1 {
		return nil, p.fail("version", "must be 1")
	}
	cfg := &Config{Path: p.path}
	var err error
	if cfg.Settings, err = p.settings(doc.Settings); err != nil {
		return nil, err
	}
	if cfg.Accounts, err = p.accounts(doc.Accounts, home); err != nil {
		return nil, err
	}
	if cfg.Schedules, err = p.schedules(doc.Schedules); err != nil {
		return nil, err
	}
	if cfg.Profiles, err = p.profiles(doc.Profiles, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (p parser) settings(set map[string]float64) (Settings, error) {
	var d rawSettings
	if err := json.Unmarshal(defaultsJSON, &d); err != nil || d.MinCallIntervalMinutes == nil || d.DefaultCap == nil {
		panic("core: embedded defaults.json is malformed")
	}
	minutes, capPct := *d.MinCallIntervalMinutes, *d.DefaultCap
	if v, ok := set["min_call_interval_minutes"]; ok {
		if v < 0 {
			return Settings{}, p.fail("settings.min_call_interval_minutes", "must not be negative")
		}
		minutes = v
	}
	if v, ok := set["default_cap"]; ok {
		if v < 0 || v > 100 {
			return Settings{}, p.fail("settings.default_cap", "must be between 0 and 100")
		}
		capPct = v
	}
	return Settings{MinCallInterval: time.Duration(minutes * float64(time.Minute)), DefaultCap: capPct}, nil
}

func (p parser) accounts(docs map[string]DocAccount, home string) (map[string]adapter.Account, error) {
	out := map[string]adapter.Account{}
	for _, name := range sortedKeys(docs) {
		prefix := "accounts." + name
		if name == ReservedAccountName {
			return nil, p.fail(prefix, "reserved name")
		}
		d := docs[name]
		a := adapter.Account{Name: name, Vendor: d.Vendor, ConfigDir: d.ConfigDir, CodexHome: d.CodexHome, KeyFile: d.KeyFile, KeyLine: d.KeyLine}
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

func (p parser) schedules(docs map[string][]DocRule) (map[string]Schedule, error) {
	out := map[string]Schedule{}
	for _, name := range sortedKeys(docs) {
		s := Schedule{}
		for i, r := range docs[name] {
			rule, err := p.scheduleRule(r, fmt.Sprintf("schedules.%s[%d]", name, i))
			if err != nil {
				return nil, err
			}
			s.Rules = append(s.Rules, rule)
		}
		out[name] = s
	}
	return out, nil
}

// scheduleRule checks one {days, utc} rule. Both lists present and empty is
// the valid empty rule; one empty list alone is refused, as is a missing one.
func (p parser) scheduleRule(rs DocRule, prefix string) (ScheduleRule, error) {
	bothListed := rs.Days != nil && rs.UTC != nil
	if !(bothListed && len(rs.Days) == 0 && len(rs.UTC) == 0) {
		if len(rs.Days) == 0 {
			return ScheduleRule{}, p.fail(join(prefix, "days"), "must not be empty")
		}
		if len(rs.UTC) == 0 {
			return ScheduleRule{}, p.fail(join(prefix, "utc"), "must not be empty")
		}
	}
	// ParseSchedule reports no index, so locate the bad element one by one.
	for i, d := range rs.Days {
		if _, err := ParseSchedule([]string{d}, nil); err != nil {
			return ScheduleRule{}, p.fail(fmt.Sprintf("%s.days[%d]", prefix, i), "%v", err)
		}
	}
	for i, r := range rs.UTC {
		if _, err := ParseSchedule(nil, []string{r}); err != nil {
			return ScheduleRule{}, p.fail(fmt.Sprintf("%s.utc[%d]", prefix, i), "%v", err)
		}
	}
	s, err := ParseSchedule(rs.Days, rs.UTC)
	if err != nil {
		return ScheduleRule{}, p.fail(prefix, "%v", err)
	}
	return s.Rules[0], nil
}

func (p parser) profiles(docs map[string][]DocChoice, cfg *Config) (map[string][]Choice, error) {
	out := map[string][]Choice{}
	for _, name := range sortedKeys(docs) {
		choices := make([]Choice, 0, len(docs[name]))
		for i, d := range docs[name] {
			c, err := p.choice(d, fmt.Sprintf("profiles.%s[%d]", name, i), cfg)
			if err != nil {
				return nil, err
			}
			choices = append(choices, c)
		}
		out[name] = choices
	}
	return out, nil
}

func (p parser) choice(d DocChoice, prefix string, cfg *Config) (Choice, error) {
	var c Choice
	if d.Account == "" {
		return c, p.fail(join(prefix, "account"), "required")
	}
	if _, ok := cfg.Accounts[d.Account]; !ok {
		return c, p.fail(join(prefix, "account"), "unknown account %q", d.Account)
	}
	if d.Model == "" {
		return c, p.fail(join(prefix, "model"), "required")
	}
	c = Choice{Account: d.Account, Model: d.Model, Thinking: d.Thinking, Cap: d.Cap, Capped: d.Capped}
	return c, p.choiceSchedules(&c, d, prefix, cfg)
}

// choiceSchedules checks skip_during / allow_during and stores them on c.
func (p parser) choiceSchedules(c *Choice, d DocChoice, prefix string, cfg *Config) error {
	if d.AllowDuring != nil && len(d.AllowDuring) == 0 {
		return p.fail(join(prefix, "allow_during"), "must not be empty")
	}
	if len(d.SkipDuring) > 0 && len(d.AllowDuring) > 0 {
		return p.fail(prefix, "use skip_during or allow_during, not both")
	}
	for _, kind := range []struct {
		key  string
		list []string
	}{{"skip_during", d.SkipDuring}, {"allow_during", d.AllowDuring}} {
		for _, s := range kind.list {
			if _, ok := cfg.Schedules[s]; !ok {
				return p.fail(join(prefix, kind.key), "unknown schedule %q", s)
			}
		}
	}
	c.SkipDuring, c.AllowDuring = d.SkipDuring, d.AllowDuring
	return nil
}
