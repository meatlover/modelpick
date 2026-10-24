package core

import (
	"errors"
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
