package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExampleConfigParses(t *testing.T) {
	cfg, err := Load("../../examples/config.json", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Accounts["claude-rec"].ConfigDir; got != "/home/u/.claude-rec" {
		t.Errorf("ConfigDir = %q", got)
	}
	if cfg.Accounts["go-2"].Name != "go-2" || cfg.Accounts["go-2"].KeyLine != 2 {
		t.Errorf("go-2 = %+v", cfg.Accounts["go-2"])
	}
	cd := cfg.Profiles["coder-difficult"]
	if cd[3].Capped {
		t.Error("coder-difficult[3] must be uncapped")
	}
	if !cd[1].Capped || !reflect.DeepEqual(cd[1].Cap, []float64{90, 98, 95}) {
		t.Errorf("coder-difficult[1].Cap = %v capped=%v", cd[1].Cap, cd[1].Capped)
	}
	if th := cfg.Profiles["tester"][0].Thinking; th != nil {
		t.Errorf("tester[0].Thinking = %q, want nil", *th)
	}
	if got := cfg.Profiles["tester"][0].SkipDuring; !reflect.DeepEqual(got, []string{"deepseek-peak"}) {
		t.Errorf("SkipDuring = %v", got)
	}
	if cfg.Settings.MinCallInterval != 15*time.Minute || cfg.Settings.DefaultCap != 95 {
		t.Errorf("Settings = %+v", cfg.Settings)
	}
	if got := cfg.ProfileNames(); !reflect.DeepEqual(got, []string{"coder-difficult", "tester"}) {
		t.Errorf("ProfileNames = %v", got)
	}
	if len(cfg.Schedules["deepseek-peak"].Ranges) != 2 {
		t.Errorf("schedule = %+v", cfg.Schedules["deepseek-peak"])
	}
}

const minimal = `{"version":1,"accounts":{"a":{"vendor":"claude","config_dir":"~/.c"}},
"profiles":{"p":[{"account":"a","model":"m","thinking":null}]}}`

func TestDefaultsApplyWhenSettingsOmitted(t *testing.T) {
	cfg, err := Parse([]byte(minimal), "c.json", "/h")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.MinCallInterval != 15*time.Minute || cfg.Settings.DefaultCap != 95 {
		t.Errorf("Settings = %+v", cfg.Settings)
	}
}

func TestEmptyCapArrayIsCapped(t *testing.T) {
	doc := strings.Replace(minimal, `"thinking":null`, `"thinking":null,"cap":[]`, 1)
	cfg, err := Parse([]byte(doc), "c.json", "/h")
	if err != nil {
		t.Fatal(err)
	}
	if c := cfg.Profiles["p"][0]; !c.Capped || len(c.Cap) != 0 {
		t.Errorf("choice = %+v", c)
	}
}

func TestSettingsOverride(t *testing.T) {
	doc := strings.Replace(minimal, `"accounts"`, `"settings":{"min_call_interval_minutes":5,"default_cap":80},"accounts"`, 1)
	cfg, err := Parse([]byte(doc), "c.json", "/h")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.MinCallInterval != 5*time.Minute || cfg.Settings.DefaultCap != 80 {
		t.Errorf("Settings = %+v", cfg.Settings)
	}
}

func TestValidationErrors(t *testing.T) {
	const acc = `"accounts":{"a":{"vendor":"claude","config_dir":"/c"}}`
	const sch = `"schedules":{"peak":{"days":["mon"],"utc":["01:00-04:00"]}}`
	prof := func(choice string) string {
		return `{"version":1,` + acc + `,` + sch + `,"profiles":{"tester":[` + choice + `]}}`
	}
	ok := `{"account":"a","model":"m","thinking":null}`
	cases := []struct {
		name, doc, field, msg string
	}{
		{"unknown top key", `{"version":1,"bogus":1}`, "bogus", "unknown key"},
		{"unknown choice key", prof(ok + `,{"account":"a","model":"m","thinking":null,"bogus":1}`), "profiles.tester[1].bogus", "unknown key"},
		{"unknown account key", `{"version":1,"accounts":{"a":{"vendor":"claude","config_dir":"/c","x":1}}}`, "accounts.a.x", "unknown key"},
		{"version missing", `{}`, "version", "must be 1"},
		{"version 2", `{"version":2}`, "version", "must be 1"},
		{"unknown vendor", `{"version":1,"accounts":{"a":{"vendor":"gpt"}}}`, "accounts.a.vendor", `unknown vendor "gpt"`},
		{"claude missing dir", `{"version":1,"accounts":{"a":{"vendor":"claude"}}}`, "accounts.a.config_dir", `required for vendor "claude"`},
		{"claude with codex_home", `{"version":1,"accounts":{"a":{"vendor":"claude","config_dir":"/c","codex_home":"/x"}}}`, "accounts.a.codex_home", `not valid for vendor "claude"`},
		{"codex with config_dir", `{"version":1,"accounts":{"a":{"vendor":"codex","codex_home":"/x","config_dir":"/c"}}}`, "accounts.a.config_dir", `not valid for vendor "codex"`},
		{"go missing key_line", `{"version":1,"accounts":{"a":{"vendor":"opencode-go","key_file":"/k"}}}`, "accounts.a.key_line", `required for vendor "opencode-go"`},
		{"go key_line 0", `{"version":1,"accounts":{"a":{"vendor":"opencode-go","key_file":"/k","key_line":0}}}`, "accounts.a.key_line", `required for vendor "opencode-go"`},
		{"go key_line negative", `{"version":1,"accounts":{"a":{"vendor":"opencode-go","key_file":"/k","key_line":-1}}}`, "accounts.a.key_line", "must be >= 1"},
		{"go with config_dir", `{"version":1,"accounts":{"a":{"vendor":"opencode-go","key_file":"/k","key_line":1,"config_dir":"/c"}}}`, "accounts.a.config_dir", `not valid for vendor "opencode-go"`},
		{"unknown account", prof(`{"account":"zz","model":"m","thinking":null}`), "profiles.tester[0].account", `unknown account "zz"`},
		{"unknown schedule", prof(ok + `,{"account":"a","model":"m","thinking":null,"skip_during":["deepseek-peek"]}`), "profiles.tester[1].skip_during", `unknown schedule "deepseek-peek"`},
		{"cap above 100", prof(`{"account":"a","model":"m","thinking":null,"cap":[90,101]}`), "profiles.tester[0].cap[1]", "must be between 0 and 100"},
		{"cap negative", prof(`{"account":"a","model":"m","thinking":null,"cap":[-1]}`), "profiles.tester[0].cap[0]", "must be between 0 and 100"},
		{"cap null", prof(`{"account":"a","model":"m","thinking":null,"cap":null}`), "profiles.tester[0].cap", "cap must not be null: omit cap for no limit"},
		{"cap null element", prof(`{"account":"a","model":"m","thinking":null,"cap":[90,null]}`), "profiles.tester[0].cap[1]", "must be a number"},
		{"settings wrong type", `{"version":1,"settings":"x"}`, "settings", "must be an object"},
		{"account wrong type", `{"version":1,"accounts":{"a":"x"}}`, "accounts.a", "must be an object"},
		{"choice wrong type", prof(ok + `,[1]`), "profiles.tester[1]", "must be an object"},
		{"empty profile", `{"version":1,` + acc + `,"profiles":{"tester":[]}}`, "profiles.tester", "must not be empty"},
		{"thinking missing", prof(`{"account":"a","model":"m"}`), "profiles.tester[0].thinking", "required (use null for none)"},
		{"bad day", `{"version":1,"schedules":{"peak":{"days":["mon","funday"],"utc":["01:00-04:00"]}}}`, "schedules.peak.days[1]", `unknown day "funday" (want mon..sun)`},
		{"bad utc", `{"version":1,"schedules":{"peak":{"days":["mon"],"utc":["01:00-04:00","25:00-26:00"]}}}`, "schedules.peak.utc[1]", `range "25:00-26:00": invalid time "25:00" (want HH:MM, 00:00-23:59)`},
		{"empty days", `{"version":1,"schedules":{"peak":{"days":[],"utc":["01:00-04:00"]}}}`, "schedules.peak.days", "must not be empty"},
		{"empty utc", `{"version":1,"schedules":{"peak":{"days":["mon"],"utc":[]}}}`, "schedules.peak.utc", "must not be empty"},
		{"default_cap out of range", `{"version":1,"settings":{"default_cap":101}}`, "settings.default_cap", "must be between 0 and 100"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.doc), "c.json", "/h")
			var ce *ConfigError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want *ConfigError", err)
			}
			if ce.Field != c.field {
				t.Errorf("Field = %q, want %q (msg %q)", ce.Field, c.field, ce.Msg)
			}
			if c.msg != "" && ce.Msg != c.msg {
				t.Errorf("Msg = %q, want %q", ce.Msg, c.msg)
			}
			if ce.Path != "c.json" {
				t.Errorf("Path = %q", ce.Path)
			}
		})
	}
}

func TestConfigErrorString(t *testing.T) {
	e := &ConfigError{Path: "p.json", Field: "a.b", Msg: "bad"}
	if e.Error() != "config p.json: a.b: bad" {
		t.Errorf("Error() = %q", e.Error())
	}
}
