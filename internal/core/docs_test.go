package core

import (
	"encoding/json"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// docSection returns the lines of a markdown file after the line heading,
// up to the next heading of the same or a higher level.
func docSection(t *testing.T, path, heading string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	level := strings.Index(heading, " ")
	var out []string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		if hashes := len(line) - len(strings.TrimLeft(line, "#")); in && hashes > 0 && hashes <= level &&
			strings.HasPrefix(line[hashes:], " ") {
			break
		}
		if in {
			out = append(out, line)
		}
		in = in || line == heading
	}
	if !in {
		t.Fatalf("%s has no heading %q", path, heading)
	}
	return strings.Join(out, "\n")
}

var (
	fencedJSON  = regexp.MustCompile("(?s)```json\n(.*?)\n```")
	backtickNow = regexp.MustCompile("`(\\d{4}-\\d\\d-\\d\\dT\\d\\d:\\d\\d:\\d\\dZ)`")
)

const readingsHeader = "| Account | Window | Used | Resets (UTC) |"

// docReadings parses the worked example's readings table: one window per
// row, its length written as FormatLength writes it.
func docReadings(t *testing.T, section string, taken time.Time) map[string]AccountState {
	t.Helper()
	i := strings.Index(section, readingsHeader)
	if i < 0 {
		t.Fatalf("no readings table headed %q", readingsHeader)
	}
	lengths := map[string]time.Duration{"5h": 5 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
	usage := map[string]AccountState{}
	for _, line := range strings.Split(section[i:], "\n")[2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for k := range cells {
			cells[k] = strings.TrimSpace(cells[k])
		}
		used, err := strconv.ParseFloat(strings.TrimSuffix(cells[2], "%"), 64)
		resets, err2 := time.Parse(time.RFC3339, cells[3])
		length, ok := lengths[cells[1]]
		if len(cells) != 4 || err != nil || err2 != nil || !ok {
			t.Fatalf("readings row %q: want | account | 5h/7d/30d | N%% | RFC 3339 |", line)
		}
		st := usage[cells[0]]
		if st.Reading == nil {
			st.Reading = &adapter.Reading{TakenAt: taken}
		}
		st.Reading.Windows = append(st.Reading.Windows,
			adapter.Window{Label: cells[1], Length: length, UsedPct: used, ResetsAt: resets})
		usage[cells[0]] = st
	}
	if len(usage) == 0 {
		t.Fatal("the readings table has no rows")
	}
	return usage
}

// TestConceptsWorkedExample runs the worked example of docs/concepts.md
// through the real Parse and Pick: its config block, at its stated time,
// with its readings table and every login present, must give its stated
// result byte for byte.
func TestConceptsWorkedExample(t *testing.T) {
	section := docSection(t, "../../docs/concepts.md", "## A worked example")
	blocks := fencedJSON.FindAllStringSubmatch(section, -1)
	if len(blocks) != 2 {
		t.Fatalf("the worked example has %d json blocks, want 2 (config, result)", len(blocks))
	}
	cfg, err := Parse([]byte(blocks[0][1]), "concepts.md", "/home/you")
	if err != nil {
		t.Fatalf("config block: %v", err)
	}
	m := backtickNow.FindStringSubmatch(section)
	if m == nil {
		t.Fatal("the worked example states no `YYYY-MM-DDTHH:MM:SSZ` time")
	}
	now, _ := time.Parse(time.RFC3339, m[1])
	var want struct{ Profile string }
	if err := json.Unmarshal([]byte(blocks[1][1]), &want); err != nil {
		t.Fatalf("result block: %v", err)
	}
	env := &fakeEnv{usage: docReadings(t, section, now.Add(-time.Minute))}
	got := Pick(cfg, want.Profile, now, env, func(int, func(i, j int)) {})
	if string(got.JSON()) != blocks[1][1]+"\n" {
		t.Errorf("docs/concepts.md worked example: pick gives\n%s\nthe doc says\n%s", got.JSON(), blocks[1][1])
	}
}

// configKeys returns every json key of the structs the config decoder reads:
// all struct tags in doc.go and week.go, the files that define them.
func configKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	fset := token.NewFileSet()
	for _, file := range []string{"doc.go", "week.go"} {
		f, err := goparser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if field, ok := n.(*ast.Field); ok && field.Tag != nil {
				tag, _ := strconv.Unquote(field.Tag.Value)
				name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
				if name != "" && name != "-" {
					keys = append(keys, name)
				}
			}
			return true
		})
	}
	if len(keys) < 15 {
		t.Fatalf("found only %d config keys in doc.go and week.go: %v", len(keys), keys)
	}
	return keys
}

// TestConfigGuideCoversKeysAndDefaults: every key the decoder accepts is
// named in docs/configuration.md, and the settings table's defaults are the
// embedded defaults.json.
func TestConfigGuideCoversKeysAndDefaults(t *testing.T) {
	data, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)
	for _, k := range configKeys(t) {
		if !strings.Contains(guide, "`"+k+"`") {
			t.Errorf("config key %q is not documented in docs/configuration.md", k)
		}
	}
	var defaults map[string]float64
	if err := json.Unmarshal(defaultsJSON, &defaults); err != nil {
		t.Fatal(err)
	}
	settings := docSection(t, "../../docs/configuration.md", "## `settings`")
	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\| `([^`]*)` \\|")
	documented := map[string]string{}
	for _, m := range row.FindAllStringSubmatch(settings, -1) {
		documented[m[1]] = m[2]
	}
	for k, v := range defaults {
		if got, ok := documented[k]; !ok || got != strconv.FormatFloat(v, 'f', -1, 64) {
			t.Errorf("settings.%s: docs/configuration.md says default %q, defaults.json has %v", k, got, v)
		}
	}
	if len(documented) != len(defaults) {
		t.Errorf("the settings table has %d rows, defaults.json %d keys", len(documented), len(defaults))
	}
}
