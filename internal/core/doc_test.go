package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const canonicalWant = `{
  "version": 1,
  "settings": {
    "default_cap": 90
  },
  "accounts": {
    "a": {
      "vendor": "claude",
      "config_dir": "~/.claude"
    },
    "b": {
      "vendor": "opencode-go",
      "key_file": "~/.ssh/keys",
      "key_line": 2
    }
  },
  "schedules": {
    "many": [
      {
        "days": ["sat"],
        "utc": ["08:00-12:00"]
      },
      {
        "days": ["sun"],
        "utc": ["09:00-11:00"]
      }
    ],
    "none": {"days": [], "utc": []},
    "one": {
      "days": ["mon", "tue"],
      "utc": ["01:00-04:00"]
    }
  },
  "profiles": {
    "p": [
      {
        "account": "a",
        "model": "m",
        "thinking": null,
        "cap": [90, "pace+10"],
        "skip_during": ["one"]
      },
      {
        "account": "b",
        "model": "n",
        "thinking": "high",
        "cap": [],
        "allow_during": ["many"]
      },
      {
        "account": "a",
        "model": "m",
        "thinking": null
      }
    ]
  }
}
`

const canonicalInput = `{"profiles":{"p":[
 {"cap":[90,"pace+10","default","default"],"thinking":null,"model":"m","account":"a","skip_during":["one"]},
 {"account":"b","model":"n","thinking":"high","cap":["default"],"allow_during":["many"]},
 {"account":"a","model":"m","thinking":null,"skip_during":[]}]},
"schedules":{"one":{"days":["mon","tue"],"utc":["01:00-04:00"]},
 "none":{"days":[],"utc":[]},
 "many":[{"days":["sat"],"utc":["08:00-12:00"]},{"days":["sun"],"utc":["09:00-11:00"]}]},
"accounts":{"b":{"key_line":2,"key_file":"~/.ssh/keys","vendor":"opencode-go"},"a":{"config_dir":"~/.claude","vendor":"claude"}},
"settings":{"default_cap":90},"version":1}`

func mustDoc(t *testing.T, src string) *Doc {
	t.Helper()
	d, err := ParseDoc([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEncodeCanonical(t *testing.T) {
	got := string(mustDoc(t, canonicalInput).Encode())
	if got != canonicalWant {
		t.Errorf("Encode:\n%s\nwant:\n%s", got, canonicalWant)
	}
	if _, err := Parse([]byte(got), "c.json", "/h"); err != nil {
		t.Errorf("canonical output does not parse: %v", err)
	}
}

func TestEncodeSettingsKeys(t *testing.T) {
	d := mustDoc(t, minimalConfig(t))
	if out := string(d.Encode()); strings.Contains(out, "settings") {
		t.Errorf("a file without settings encoded settings:\n%s", out)
	}
	d.Settings = map[string]float64{"default_cap": 90}
	if out := string(d.Encode()); !strings.Contains(out, "  \"settings\": {\n    \"default_cap\": 90\n  },\n") ||
		strings.Contains(out, "min_call") {
		t.Errorf("default_cap only expected:\n%s", out)
	}
}

func TestEncodeKeepsTilde(t *testing.T) {
	out := string(mustDoc(t, minimalConfig(t)).Encode())
	if !strings.Contains(out, `"config_dir": "~/.c"`) {
		t.Errorf("tilde expanded or lost:\n%s", out)
	}
}

func TestEncodeNumbers(t *testing.T) {
	d := mustDoc(t, minimalConfig(t))
	d.Settings = map[string]float64{"default_cap": 92.5, "min_call_interval_minutes": 15}
	d.Profiles["p"][0].Cap, d.Profiles["p"][0].Capped = []CapElem{{CapFixed, 90}, {CapFixed, 92.5}, {CapPace, 7}}, true
	out := string(d.Encode())
	for _, want := range []string{`"default_cap": 92.5`, `"min_call_interval_minutes": 15`, `"cap": [90, 92.5, "pace+7"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "90.0") || strings.Contains(out, "15.0") {
		t.Errorf("float noise in:\n%s", out)
	}
}

func minimalConfig(t *testing.T) string {
	t.Helper()
	return readFixture(t, "testdata/configs/minimal.json")
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEncodeRoundTrip(t *testing.T) {
	files, err := filepath.Glob("testdata/configs/*.json")
	if err != nil || len(files) < 6 {
		t.Fatalf("fixtures: %v %v", files, err)
	}
	files = append(files, "../../examples/config.json")
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src := readFixture(t, f)
			want, err := Parse([]byte(src), "c.json", "/home/u")
			if err != nil {
				t.Fatal(err)
			}
			doc := mustDoc(t, src)
			enc := doc.Encode()
			got, err := Parse(enc, "c.json", "/home/u")
			if err != nil {
				t.Fatalf("re-parse: %v\n%s", err, enc)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Parse(Encode(x)) != Parse(x)\n%s", enc)
			}
			if again := mustDoc(t, string(enc)).Encode(); string(again) != string(enc) {
				t.Errorf("Encode is not idempotent:\n%s\nvs\n%s", enc, again)
			}
		})
	}
}

func TestParseDocErrors(t *testing.T) {
	cases := []struct{ name, src, field, msg string }{
		{"thinking absent", `{"version":1,"profiles":{"p":[{"account":"a","model":"m"}]}}`, "profiles.p[0].thinking", "required (use null for none)"},
		{"cap null", `{"version":1,"profiles":{"p":[{"account":"a","model":"m","thinking":null,"cap":null}]}}`, "profiles.p[0].cap", "cap must not be null: omit cap for no limit"},
		{"unknown key", `{"version":1,"x":1}`, "x", "unknown key"},
	}
	for _, c := range cases {
		_, err := ParseDoc([]byte(c.src))
		var ce *ConfigError
		if !errors.As(err, &ce) || ce.Field != c.field || ce.Msg != c.msg {
			t.Errorf("%s: err = %v, want %s: %s", c.name, err, c.field, c.msg)
		}
	}
}

// specGroupExample is the profile of spec §4 (load balancing design), as written there.
const specGroupExample = `{"version":1,"profiles":{"tester": [
  {
    "load_balance": "random",
    "choices": [
      { "account": "go-1", "model": "opencode-go/deepseek-v4.1-flash", "thinking": null, "cap": [95, 95, 95], "skip_during": ["deepseek-peak"] },
      { "account": "go-2", "model": "opencode-go/deepseek-v4.1-flash", "thinking": null, "cap": [95, 95, 95], "skip_during": ["deepseek-peak"] },
      { "account": "go-3", "model": "opencode-go/deepseek-v4.1-flash", "thinking": null, "cap": [],           "skip_during": ["deepseek-peak"] }
    ]
  },
  { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" }
]}}`

func TestDecodeGroup(t *testing.T) {
	cs := mustDoc(t, specGroupExample).Profiles["tester"]
	if len(cs) != 4 {
		t.Fatalf("decoded %d choices, want 4 flat", len(cs))
	}
	for i, c := range cs[:3] {
		if c.Group != 1 || c.LoadBalance != "random" || c.Account != fmt.Sprintf("go-%d", i+1) {
			t.Errorf("member %d = %+v", i, c)
		}
	}
	if c := cs[3]; c.Group != 0 || c.LoadBalance != "" || c.Account != "claude-main" {
		t.Errorf("plain choice = %+v", c)
	}
	if !cs[2].Capped || len(cs[2].Cap) != 0 || cs[2].SkipDuring[0] != "deepseek-peak" {
		t.Errorf("member fields lost: %+v", cs[2])
	}
}

const adjacentGroups = `{
  "version": 1,
  "profiles": {
    "p": [
      {
        "load_balance": "random",
        "choices": [
          {
            "account": "a",
            "model": "m",
            "thinking": null
          },
          {
            "account": "b",
            "model": "m",
            "thinking": null
          }
        ]
      },
      {
        "load_balance": "random",
        "choices": [
          {
            "account": "c",
            "model": "m",
            "thinking": null
          },
          {
            "account": "d",
            "model": "m",
            "thinking": null
          }
        ]
      }
    ]
  }
}
`

func TestDecodeAdjacentGroups(t *testing.T) {
	d := mustDoc(t, adjacentGroups)
	var ids []int
	for _, c := range d.Profiles["p"] {
		ids = append(ids, c.Group)
	}
	if !reflect.DeepEqual(ids, []int{1, 1, 2, 2}) {
		t.Errorf("group ids = %v, want [1 1 2 2]", ids)
	}
	if got := string(d.Encode()); got != adjacentGroups {
		t.Errorf("Encode merged or changed the groups:\n%s", got)
	}
}

const specGroupCanonical = `{
  "version": 1,
  "profiles": {
    "tester": [
      {
        "load_balance": "random",
        "choices": [
          {
            "account": "go-1",
            "model": "opencode-go/deepseek-v4.1-flash",
            "thinking": null,
            "cap": [95, 95, 95],
            "skip_during": ["deepseek-peak"]
          },
          {
            "account": "go-2",
            "model": "opencode-go/deepseek-v4.1-flash",
            "thinking": null,
            "cap": [95, 95, 95],
            "skip_during": ["deepseek-peak"]
          },
          {
            "account": "go-3",
            "model": "opencode-go/deepseek-v4.1-flash",
            "thinking": null,
            "cap": [],
            "skip_during": ["deepseek-peak"]
          }
        ]
      },
      {
        "account": "claude-main",
        "model": "claude-sonnet-5-5",
        "thinking": "medium"
      }
    ]
  }
}
`

func TestEncodeGroupCanonical(t *testing.T) {
	if got := string(mustDoc(t, specGroupExample).Encode()); got != specGroupCanonical {
		t.Errorf("Encode:\n%s\nwant:\n%s", got, specGroupCanonical)
	}
}

func TestParseDocGroupErrors(t *testing.T) {
	prof := func(items string) string { return `{"version":1,"profiles":{"t":[` + items + `]}}` }
	const m = `{"account":"a","model":"m","thinking":null}`
	cases := []struct{ name, src, field, msg string }{
		{"account and load_balance", prof(`{"account":"a","model":"m","thinking":null,"load_balance":"random"}`), "profiles.t[0]", "a choice has account, a group has load_balance and choices: not both"},
		{"account and choices", prof(`{"account":"a","choices":[` + m + `,` + m + `]}`), "profiles.t[0]", "a choice has account, a group has load_balance and choices: not both"},
		{"neither", prof(`{"model":"m","thinking":null}`), "profiles.t[0]", "needs account (a choice) or load_balance (a group)"},
		{"neither, second item", prof(m + `,{}`), "profiles.t[1]", "needs account (a choice) or load_balance (a group)"},
		{"typo'd account", prof(`{"acount":"a","model":"m","thinking":null}`), "profiles.t[0].acount", "unknown key"},
		{"typo'd load_balance, no choices", prof(`{"load_balanse":"random"}`), "profiles.t[0].load_balanse", "unknown key"},
		{"neither, wrong type", prof(`{"model":5}`), "profiles.t[0]", "needs account (a choice) or load_balance (a group)"},
		{"one member", prof(`{"load_balance":"random","choices":[` + m + `]}`), "profiles.t[0].choices", "needs at least 2 members"},
		{"no choices", prof(`{"load_balance":"random"}`), "profiles.t[0].choices", "needs at least 2 members"},
		{"choices not an array", prof(`{"load_balance":"random","choices":{}}`), "profiles.t[0].choices", "must be an array"},
		{"nested group", prof(`{"load_balance":"random","choices":[{"load_balance":"random","choices":[` + m + `,` + m + `]},` + m + `]}`), "profiles.t[0].choices[0]", "a group cannot be a member of a group"},
		{"cap on a group", prof(`{"load_balance":"random","cap":[90],"choices":[` + m + `,` + m + `]}`), "profiles.t[0].cap", "unknown key"},
		{"no load_balance", prof(`{"choices":[` + m + `,` + m + `]}`), "profiles.t[0].load_balance", "required"},
		{"member thinking", prof(m + `,{"load_balance":"random","choices":[` + m + `,{"account":"a","model":"m"}]}`), "profiles.t[1].choices[1].thinking", "required (use null for none)"},
		{"member not an object", prof(`{"load_balance":"random","choices":[` + m + `,1]}`), "profiles.t[0].choices[1]", "must be an object"},
	}
	for _, c := range cases {
		_, err := ParseDoc([]byte(c.src))
		var ce *ConfigError
		if !errors.As(err, &ce) || ce.Field != c.field || ce.Msg != c.msg {
			t.Errorf("%s: err = %v, want %s: %s", c.name, err, c.field, c.msg)
		}
	}
}
