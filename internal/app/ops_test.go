package app_test

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
)

// fx is a config fixture as JSON fragments keyed by name, so a case states
// only what an operation changes.
type fx struct {
	settings  map[string]string // nil = no settings section
	accounts  map[string]string
	schedules map[string]string // nil = no schedules section
	profiles  map[string][]string
}

func clone[V any](m map[string]V) map[string]V {
	if m == nil {
		return nil
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (f fx) copy() fx {
	return fx{clone(f.settings), clone(f.accounts), clone(f.schedules), clone(f.profiles)}
}

// prof sets profile name to choices; no choices removes it.
func (f fx) prof(name string, choices ...string) fx {
	g := f.copy()
	if len(choices) == 0 {
		delete(g.profiles, name)
	} else {
		g.profiles[name] = choices
	}
	return g
}

// sched sets schedule name to body; "" removes it.
func (f fx) sched(name, body string) fx {
	g := f.copy()
	if g.schedules == nil {
		g.schedules = map[string]string{}
	}
	if body == "" {
		delete(g.schedules, name)
	} else {
		g.schedules[name] = body
	}
	return g
}

// acct sets account name to body; "" removes it.
func (f fx) acct(name, body string) fx {
	g := f.copy()
	if body == "" {
		delete(g.accounts, name)
	} else {
		g.accounts[name] = body
	}
	return g
}

func (f fx) setting(key, val string) fx {
	g := f.copy()
	if g.settings == nil {
		g.settings = map[string]string{}
	}
	g.settings[key] = val
	return g
}

func (f fx) noSettings() fx  { g := f.copy(); g.settings = nil; return g }
func (f fx) noSchedules() fx { g := f.copy(); g.schedules = nil; return g }

func members[V any](m map[string]V, render func(V) string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = `"` + k + `":` + render(m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (f fx) String() string {
	id := func(s string) string { return s }
	out := `{"version":1`
	if f.settings != nil {
		out += `,"settings":` + members(f.settings, id)
	}
	out += `,"accounts":` + members(f.accounts, id)
	if f.schedules != nil {
		out += `,"schedules":` + members(f.schedules, id)
	}
	list := func(cs []string) string { return "[" + strings.Join(cs, ",") + "]" }
	return out + `,"profiles":` + members(f.profiles, list) + "}"
}

// Choices as written in the file, and as the page sends them in expect.
const (
	c0 = `{"account":"claude-main","model":"claude-opus-5-5","thinking":"high","cap":[90,"pace+10"]}`
	c1 = `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"xhigh","cap":[],"skip_during":["peak"]}`
	c2 = `{"account":"go-1","model":"opencode-go/deepseek-v4.1-flash","thinking":null,"allow_during":["peak","night"]}`
	s0 = `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"low","cap":[80],"allow_during":["night"]}`

	e0 = `{"account":"claude-main","model":"claude-opus-5-5","thinking":"high","cap":[90,"pace+10"],"skip_during":[],"allow_during":[]}`
	e1 = `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"xhigh","cap":[],"skip_during":["peak"],"allow_during":[]}`
	e2 = `{"account":"go-1","model":"opencode-go/deepseek-v4.1-flash","thinking":null,"cap":null,"skip_during":[],"allow_during":["peak","night"]}`
	es = `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"low","cap":[80],"skip_during":[],"allow_during":["night"]}`

	peak = `{"days":["mon","tue"],"utc":["01:30-03:00"]}`
)

var base = fx{
	settings: map[string]string{"default_cap": "95"},
	accounts: map[string]string{
		"claude-main": `{"vendor":"claude","config_dir":"~/.claude"}`,
		"claude-two":  `{"vendor":"claude","config_dir":"~/.claude-two"}`,
		"codex-main":  `{"vendor":"codex","codex_home":"~/.codex"}`,
		"go-1":        `{"vendor":"opencode-go","key_file":"~/.ssh/opencode-go-api","key_line":1}`,
		"go-9":        `{"vendor":"opencode-go","key_file":"~/.ssh/opencode-go-api","key_line":9}`,
	},
	schedules: map[string]string{
		"peak":  peak,
		"night": `{"days":["sun"],"utc":["22:00-24:00"]}`,
		"spare": `{"days":[],"utc":[]}`,
	},
	profiles: map[string][]string{"coder": {c0, c1, c2}, "solo": {s0}},
}

// testOpEnv lists models per account; an account missing here fails to list.
func testOpEnv() app.OpEnv {
	models := map[string][]adapter.Model{
		"claude-main": {{ID: "claude-opus-5-5", Thinking: []string{"low", "medium", "high"}},
			{ID: "claude-sonnet-5", Thinking: []string{"medium", "high"}}},
		"claude-two": {{ID: "claude-haiku-5"}, {ID: "claude-opus-5-5", Thinking: []string{"low", "medium"}}},
		"codex-main": {{ID: "gpt-6.1-sol", Thinking: []string{"low", "medium", "high", "xhigh"}}, {ID: "gpt-5-mini"}},
		"go-1":       {{ID: "opencode-go/deepseek-v4.1-flash"}, {ID: "opencode-go/kimi-k3", Thinking: []string{"max"}}},
		"go-0":       {},
	}
	return app.OpEnv{
		Models: func(account string) ([]adapter.Model, error) {
			ms, ok := models[account]
			if !ok {
				return nil, errors.New("opencode not found on PATH")
			}
			return ms, nil
		},
		Windows: func(account, model string) []string {
			if account == "claude-main" && model == "claude-opus-5-5" {
				return []string{"5h", "7d"}
			}
			return nil
		},
	}
}

type opCase struct {
	name    string
	doc     fx
	op      string
	args    string
	want    fx     // the config after the operation (unused when refused)
	refused string // the whole Refused.Reason; "" = applied
	uses    int
}

func mustDoc(t *testing.T, text string) *core.Doc {
	t.Helper()
	d, err := core.ParseDoc([]byte(text))
	if err != nil {
		t.Fatalf("fixture: %v\n%s", err, text)
	}
	return d
}

// runOps applies each case to a fresh Doc. A refusal must leave the Doc as
// it was; an applied operation must give the wanted file and a config that
// core.Parse accepts.
func runOps(t *testing.T, cases []opCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := mustDoc(t, c.doc.String())
			before := string(d.Encode())
			err := app.ApplyOp(d, c.op, json.RawMessage(c.args), testOpEnv())
			got := string(d.Encode())
			if c.refused != "" {
				var r *app.Refused
				if !errors.As(err, &r) || r.Reason != c.refused || r.Uses != c.uses {
					t.Fatalf("want Refused{%q, %d}, got %#v", c.refused, c.uses, err)
				}
				if got != before {
					t.Fatalf("refused op changed the doc:\n%s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if want := string(mustDoc(t, c.want.String()).Encode()); got != want {
				t.Fatalf("got\n%s\nwant\n%s", got, want)
			}
			if _, err := core.Parse([]byte(got), "config.json", "/home/u"); err != nil {
				t.Fatalf("result does not parse: %v", err)
			}
		})
	}
}

func set(profile string, index int, expect, field, value string) string {
	return `{"profile":"` + profile + `","index":` + itoa(index) + `,"expect":` + expect +
		`,"field":"` + field + `","value":` + value + `}`
}

func at(profile string, index int, expect string) string {
	return `{"profile":"` + profile + `","index":` + itoa(index) + `,"expect":` + expect + `}`
}

func move(from, to int, expect string) string {
	return `{"profile":"coder","from":` + itoa(from) + `,"to":` + itoa(to) + `,"expect":` + expect + `}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

var generalCases = []opCase{
	{name: "default cap", doc: base, op: "settings.default_cap", args: `{"value":80}`,
		want: base.setting("default_cap", "80")},
	{name: "default cap 0", doc: base, op: "settings.default_cap", args: `{"value":0}`,
		want: base.setting("default_cap", "0")},
	{name: "default cap creates settings", doc: base.noSettings(), op: "settings.default_cap", args: `{"value":100}`,
		want: base.setting("default_cap", "100")},
	{name: "default cap over", doc: base, op: "settings.default_cap", args: `{"value":150}`,
		refused: "Global default cap: 150 is outside 0–100%"},
	{name: "default cap negative", doc: base, op: "settings.default_cap", args: `{"value":-1}`,
		refused: "Global default cap: -1 is outside 0–100%"},
	{name: "default cap null", doc: base, op: "settings.default_cap", args: `{"value":null}`,
		refused: "Global default cap: null is not a number"},
	{name: "default cap text", doc: base, op: "settings.default_cap", args: `{"value":"abc"}`,
		refused: `Global default cap: "abc" is not a number`},
	{name: "unknown op", doc: base, op: "profile.rename", args: `{}`,
		refused: "Unknown operation profile.rename"},
	{name: "bad args", doc: base, op: "profile.add", args: `{"name":5}`,
		refused: "Bad arguments for profile.add"},
}

func TestOpGeneral(t *testing.T) { runOps(t, generalCases) }

const newClaude = `{"account":"claude-main","model":"claude-opus-5-5","thinking":"low","cap":[]}`

var profileCases = []opCase{
	{name: "add from first account", doc: base, op: "profile.add", args: `{"name":"new"}`,
		want: base.prof("new", newClaude)},
	{name: "add bad name", doc: base, op: "profile.add", args: `{"name":"My Profile"}`,
		refused: `Profile name "My Profile": use a–z, 0–9 and -, starting with a letter or digit`},
	{name: "add leading dash", doc: base, op: "profile.add", args: `{"name":"-x"}`,
		refused: `Profile name "-x": use a–z, 0–9 and -, starting with a letter or digit`},
	{name: "add taken", doc: base, op: "profile.add", args: `{"name":"coder"}`,
		refused: "Profile coder already exists"},
	{name: "add without accounts", doc: fx{accounts: map[string]string{}, profiles: map[string][]string{}},
		op: "profile.add", args: `{"name":"new"}`, refused: "Profile new needs an account"},
	{name: "add models not listed", doc: fx{accounts: map[string]string{"go-9": base.accounts["go-9"]},
		profiles: map[string][]string{}}, op: "profile.add", args: `{"name":"new"}`,
		refused: "Cannot list the models of go-9: opencode not found on PATH"},
	{name: "add no models", doc: fx{accounts: map[string]string{"go-0": base.accounts["go-9"]},
		profiles: map[string][]string{}}, op: "profile.add", args: `{"name":"new"}`,
		refused: "Account go-0 offers no models"},
	{name: "remove", doc: base, op: "profile.remove", args: `{"name":"solo"}`, want: base.prof("solo")},
	{name: "remove unknown", doc: base, op: "profile.remove", args: `{"name":"nope"}`,
		refused: "Unknown profile nope"},
	{name: "choice.add before uncapped", doc: base, op: "choice.add", args: `{"profile":"coder"}`,
		want: base.prof("coder", c0, c1,
			`{"account":"go-1","model":"opencode-go/deepseek-v4.1-flash","thinking":null,"cap":[]}`, c2)},
	{name: "choice.add appends", doc: base, op: "choice.add", args: `{"profile":"solo"}`,
		want: base.prof("solo", s0, `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"low","cap":[]}`)},
	{name: "choice.add unknown", doc: base, op: "choice.add", args: `{"profile":"nope"}`,
		refused: "Unknown profile nope"},
	{name: "choice.add models not listed", doc: base.prof("p", strings.Replace(s0, "codex-main", "go-9", 1)),
		op: "choice.add", args: `{"profile":"p"}`, refused: "Cannot list the models of go-9: opencode not found on PATH"},
	{name: "choice.remove", doc: base, op: "choice.remove", args: at("coder", 1, e1), want: base.prof("coder", c0, c2)},
	{name: "choice.remove last", doc: base, op: "choice.remove", args: at("solo", 0, es),
		refused: "Profile solo needs at least one choice"},
	{name: "choice.remove stale", doc: base, op: "choice.remove", args: at("coder", 1, es),
		refused: "Choice 2 of coder was changed outside the UI"},
	{name: "choice.remove no expect", doc: base, op: "choice.remove", args: `{"profile":"coder","index":1}`,
		refused: "Choice 2 of coder was changed outside the UI"},
	{name: "choice.remove follows the one identical", doc: base, op: "choice.remove", args: at("coder", 1, e2),
		want: base.prof("coder", c0, c1)},
	{name: "choice.remove out of range follows", doc: base, op: "choice.remove", args: at("coder", 7, e0),
		want: base.prof("coder", c1, c2)},
	{name: "choice.remove out of range", doc: base, op: "choice.remove", args: at("coder", 7, es),
		refused: "Profile coder has no choice 8"},
	{name: "choice.remove two identical", doc: base.prof("dup", c0, c1, c1), op: "choice.remove",
		args: at("dup", 0, e1), refused: "Choice 1 of dup was changed outside the UI"},
	{name: "choice.remove exact among identical", doc: base.prof("dup", c0, c1, c1), op: "choice.remove",
		args: at("dup", 2, e1), want: base.prof("dup", c0, c1)},
	{name: "choice.remove normalised expect", doc: base, op: "choice.remove",
		args: at("coder", 0, `{"account":"claude-main","model":"claude-opus-5-5","thinking":"high","cap":[90.0,"pace+10","default"]}`),
		want: base.prof("coder", c1, c2)},
	{name: "choice.remove uncapped is not cap []", doc: base, op: "choice.remove",
		args:    at("coder", 1, strings.Replace(e1, `"cap":[]`, `"cap":null`, 1)),
		refused: "Choice 2 of coder was changed outside the UI"},
	{name: "choice.move", doc: base, op: "choice.move", args: move(0, 1, e0), want: base.prof("coder", c1, c0, c2)},
	{name: "choice.move to end", doc: base, op: "choice.move", args: move(0, 2, e0), want: base.prof("coder", c1, c2, c0)},
	{name: "choice.move follows", doc: base, op: "choice.move", args: move(0, 0, e1), want: base.prof("coder", c1, c0, c2)},
	{name: "choice.move no position", doc: base, op: "choice.move", args: move(0, 3, e0),
		refused: "Profile coder has no position 4"},
	{name: "choice.move stale", doc: base, op: "choice.move", args: move(0, 1, es),
		refused: "Choice 1 of coder was changed outside the UI"},
}

func TestOpProfile(t *testing.T) { runOps(t, profileCases) }

// with replaces one key's value inside a choice fragment.
func with(choice, from, to string) string { return strings.Replace(choice, from, to, 1) }

var choiceSetCases = []opCase{
	{name: "account keeps model, first thinking", doc: base, op: "choice.set",
		args: set("coder", 0, e0, "account", `"claude-two"`),
		want: base.prof("coder", with(with(c0, "claude-main", "claude-two"), `"high"`, `"low"`), c1, c2)},
	{name: "account takes first model, keeps thinking", doc: base, op: "choice.set",
		args: set("coder", 0, e0, "account", `"codex-main"`),
		want: base.prof("coder", with(with(c0, "claude-main", "codex-main"), "claude-opus-5-5", "gpt-6.1-sol"), c1, c2)},
	{name: "account without thinking", doc: base, op: "choice.set",
		args: set("coder", 0, e0, "account", `"go-1"`),
		want: base.prof("coder", `{"account":"go-1","model":"opencode-go/deepseek-v4.1-flash","thinking":null,"cap":[90,"pace+10"]}`, c1, c2)},
	{name: "account unknown", doc: base, op: "choice.set", args: set("coder", 0, e0, "account", `"nope"`),
		refused: "Profile coder, choice 1: unknown account nope"},
	{name: "account models not listed", doc: base, op: "choice.set", args: set("coder", 0, e0, "account", `"go-9"`),
		refused: "Profile coder, choice 1: cannot list the models of go-9: opencode not found on PATH"},
	{name: "model keeps thinking", doc: base, op: "choice.set", args: set("coder", 0, e0, "model", `"claude-sonnet-5"`),
		want: base.prof("coder", with(c0, "claude-opus-5-5", "claude-sonnet-5"), c1, c2)},
	{name: "model without thinking", doc: base, op: "choice.set", args: set("coder", 1, e1, "model", `"gpt-5-mini"`),
		want: base.prof("coder", c0, with(with(c1, "gpt-6.1-sol", "gpt-5-mini"), `"xhigh"`, "null"), c2)},
	{name: "model first thinking", doc: base, op: "choice.set", args: set("coder", 2, e2, "model", `"opencode-go/kimi-k3"`),
		want: base.prof("coder", c0, c1, with(with(c2, "deepseek-v4.1-flash", "kimi-k3"), "null", `"max"`))},
	{name: "model not offered", doc: base, op: "choice.set", args: set("coder", 0, e0, "model", `"nope"`),
		refused: "Profile coder, choice 1: claude-main does not offer nope"},
	{name: "model not text", doc: base, op: "choice.set", args: set("coder", 0, e0, "model", `5`),
		refused: "Profile coder, choice 1: model must be text"},
	{name: "thinking", doc: base, op: "choice.set", args: set("coder", 0, e0, "thinking", `"low"`),
		want: base.prof("coder", with(c0, `"high"`, `"low"`), c1, c2)},
	{name: "thinking not offered", doc: base, op: "choice.set", args: set("coder", 0, e0, "thinking", `"max"`),
		refused: `Profile coder, choice 1: thinking "max" is not offered by claude-opus-5-5`},
	{name: "thinking null with levels", doc: base, op: "choice.set", args: set("coder", 0, e0, "thinking", `null`),
		refused: "Profile coder, choice 1: thinking null is not offered by claude-opus-5-5"},
	{name: "thinking on a model without", doc: base, op: "choice.set", args: set("coder", 2, e2, "thinking", `"low"`),
		refused: `Profile coder, choice 3: thinking "low" is not offered by opencode-go/deepseek-v4.1-flash`},
	{name: "thinking models not listed", doc: base.prof("p", with(s0, "codex-main", "go-9")), op: "choice.set",
		args:    set("p", 0, with(es, "codex-main", "go-9"), "thinking", `"low"`),
		refused: "Profile p, choice 1: cannot list the models of go-9: opencode not found on PATH"},
	{name: "unknown field", doc: base, op: "choice.set", args: set("coder", 0, e0, "colour", `"red"`),
		refused: "Profile coder, choice 1: unknown field colour"},
	{name: "stale", doc: base, op: "choice.set", args: set("coder", 0, es, "thinking", `"low"`),
		refused: "Choice 1 of coder was changed outside the UI"},
	{name: "follows the one identical", doc: base, op: "choice.set", args: set("coder", 0, e1, "thinking", `"low"`),
		want: base.prof("coder", c0, with(c1, `"xhigh"`, `"low"`), c2)},
	{name: "skip", doc: base, op: "choice.set", args: set("coder", 0, e0, "skip_during", `["peak","night"]`),
		want: base.prof("coder", with(c0, `]}`, `],"skip_during":["peak","night"]}`), c1, c2)},
	{name: "skip emptied", doc: base, op: "choice.set", args: set("coder", 1, e1, "skip_during", `[]`),
		want: base.prof("coder", c0, `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"xhigh","cap":[]}`, c2)},
	{name: "allow emptied", doc: base, op: "choice.set", args: set("coder", 2, e2, "allow_during", `[]`),
		want: base.prof("coder", c0, c1, `{"account":"go-1","model":"opencode-go/deepseek-v4.1-flash","thinking":null}`)},
	{name: "skip and allow", doc: base, op: "choice.set", args: set("coder", 2, e2, "skip_during", `["spare"]`),
		refused: "Use Skip or Allow, not both"},
	{name: "unknown schedule", doc: base, op: "choice.set", args: set("coder", 0, e0, "allow_during", `["nope"]`),
		refused: "Profile coder, choice 1: unknown schedule nope"},
	{name: "schedules not a list", doc: base, op: "choice.set", args: set("coder", 0, e0, "skip_during", `null`),
		refused: "Profile coder, choice 1: schedules must be a list"},
}

func TestOpChoiceSet(t *testing.T) { runOps(t, choiceSetCases) }

func capSet(index int, expect, value string) string { return set("coder", index, expect, "cap", value) }

func withCap(cap string) fx { return base.prof("coder", with(c0, `[90,"pace+10"]`, cap), c1, c2) }

var capCases = []opCase{
	{name: "fixed", doc: base, op: "choice.set", args: capSet(0, e0, `[80]`), want: withCap(`[80]`)},
	{name: "pace ends", doc: base, op: "choice.set", args: capSet(0, e0, `["pace+5","pace+50"]`),
		want: withCap(`["pace+5","pace+50"]`)},
	{name: "default kept inside", doc: base, op: "choice.set", args: capSet(0, e0, `["default","pace+20"]`),
		want: withCap(`["default","pace+20"]`)},
	{name: "trailing default not written", doc: base, op: "choice.set", args: capSet(0, e0, `[90,"default"]`),
		want: withCap(`[90]`)},
	{name: "all default", doc: base, op: "choice.set", args: capSet(0, e0, `[]`), want: withCap(`[]`)},
	{name: "uncapped", doc: base, op: "choice.set", args: capSet(0, e0, `null`),
		want: base.prof("coder", `{"account":"claude-main","model":"claude-opus-5-5","thinking":"high"}`, c1, c2)},
	{name: "capping an uncapped", doc: base, op: "choice.set", args: capSet(2, e2, `[50]`),
		want: base.prof("coder", c0, c1, with(c2, `"thinking":null`, `"thinking":null,"cap":[50]`))},
	{name: "fixed over", doc: base, op: "choice.set", args: capSet(0, e0, `[120]`),
		refused: "Profile coder, choice 1, 5h cap: 120 is outside 0–100%"},
	{name: "pace over in 7d", doc: base, op: "choice.set", args: capSet(0, e0, `[90,"pace+70"]`),
		refused: "Profile coder, choice 1, 7d cap: pace+70 is outside pace+5–pace+50"},
	{name: "past the labels", doc: base, op: "choice.set", args: capSet(0, e0, `[90,90,"pace+51"]`),
		refused: "Profile coder, choice 1, cap 3: pace+51 is outside pace+5–pace+50"},
	{name: "no labels", doc: base, op: "choice.set", args: capSet(1, e1, `["pace+4"]`),
		refused: "Profile coder, choice 2, cap 1: pace+4 is outside pace+5–pace+50"},
	{name: "pace+0", doc: base, op: "choice.set", args: capSet(0, e0, `["pace+0"]`),
		refused: "Profile coder, choice 1, 5h cap: pace+0 is outside pace+5–pace+50"},
	{name: "not a number", doc: base, op: "choice.set", args: capSet(0, e0, `["fast"]`),
		refused: "Profile coder, choice 1, 5h cap: fast is not a number, pace+N or default"},
	{name: "null element", doc: base, op: "choice.set", args: capSet(0, e0, `[null]`),
		refused: "Profile coder, choice 1, 5h cap: null is not a number, pace+N or default"},
	{name: "Default", doc: base, op: "choice.set", args: capSet(0, e0, `["Default"]`),
		refused: "Profile coder, choice 1, 5h cap: Default is not a number, pace+N or default"},
	{name: "fractional pace", doc: base, op: "choice.set", args: capSet(0, e0, `["pace+7.5"]`),
		refused: "Profile coder, choice 1, 5h cap: pace+7.5 is not a number, pace+N or default"},
	{name: "not a list", doc: base, op: "choice.set", args: capSet(0, e0, `"x"`),
		refused: "Profile coder, choice 1: cap must be a list or null"},
}

func TestOpChoiceCap(t *testing.T) { runOps(t, capCases) }

const night = `{"days":["sun"],"utc":["22:00-24:00"]}`

var scheduleCases = []opCase{
	{name: "add", doc: base, op: "schedule.add", args: `{"name":"late"}`, want: base.sched("late", `{"days":[],"utc":[]}`)},
	{name: "add creates section", doc: base.noSchedules().prof("coder", c0).prof("solo"), op: "schedule.add",
		args: `{"name":"late"}`, want: base.noSchedules().sched("late", `{"days":[],"utc":[]}`).prof("coder", c0).prof("solo")},
	{name: "add taken", doc: base, op: "schedule.add", args: `{"name":"peak"}`, refused: "Schedule peak already exists"},
	{name: "add bad name", doc: base, op: "schedule.add", args: `{"name":"Late Night"}`,
		refused: `Schedule name "Late Night": use a–z, 0–9 and -, starting with a letter or digit`},
	{name: "remove unused", doc: base, op: "schedule.remove", args: `{"name":"spare"}`, want: base.sched("spare", "")},
	{name: "remove used", doc: base, op: "schedule.remove", args: `{"name":"peak","force":false}`,
		refused: "Schedule peak is used by 2 choices", uses: 2},
	{name: "remove used once", doc: base.prof("solo"), op: "schedule.remove", args: `{"name":"night"}`,
		refused: "Schedule night is used by 1 choice", uses: 1},
	{name: "remove forced", doc: base, op: "schedule.remove", args: `{"name":"night","force":true}`,
		want: base.sched("night", "").prof("coder", c0, c1, with(c2, `["peak","night"]`, `["peak"]`)).
			prof("solo", `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"low","cap":[80]}`)},
	{name: "remove forced drops skip", doc: base, op: "schedule.remove", args: `{"name":"peak","force":true}`,
		want: base.sched("peak", "").prof("coder", c0,
			`{"account":"codex-main","model":"gpt-6.1-sol","thinking":"xhigh","cap":[]}`,
			with(c2, `["peak","night"]`, `["night"]`))},
	{name: "remove unknown", doc: base, op: "schedule.remove", args: `{"name":"nope"}`, refused: "Unknown schedule nope"},
	{name: "paint off keeps other minutes", doc: base, op: "schedule.paint",
		args: `{"name":"peak","cells":[[1,1]],"on":false}`,
		want: base.sched("peak", `[{"days":["mon"],"utc":["02:00-03:00"]},{"days":["tue"],"utc":["01:30-03:00"]}]`)},
	{name: "paint on", doc: base, op: "schedule.paint", args: `{"name":"peak","cells":[[1,1],[2,1]],"on":true}`,
		want: base.sched("peak", `{"days":["mon","tue"],"utc":["01:00-03:00"]}`)},
	{name: "paint empty schedule", doc: base, op: "schedule.paint", args: `{"name":"spare","cells":[[0,23],[1,0]],"on":true}`,
		want: base.sched("spare", `[{"days":["mon"],"utc":["00:00-01:00"]},{"days":["sun"],"utc":["23:00-24:00"]}]`)},
	{name: "paint everything off", doc: base, op: "schedule.paint", args: `{"name":"night","cells":[[0,22],[0,23]],"on":false}`,
		want: base.sched("night", `{"days":[],"utc":[]}`)},
	{name: "paint across midnight", doc: base.sched("spare", `{"days":["sun"],"utc":["22:00-02:00"]}`),
		op: "schedule.paint", args: `{"name":"spare","cells":[[1,1]],"on":false}`,
		want: base.sched("spare", `[{"days":["mon"],"utc":["00:00-01:00"]},{"days":["sun"],"utc":["22:00-24:00"]}]`)},
	{name: "paint outside the grid", doc: base, op: "schedule.paint", args: `{"name":"peak","cells":[[1,1],[7,0]],"on":true}`,
		refused: "Cell [7, 0] is outside day 0–6, hour 0–23"},
	{name: "paint hour 24", doc: base, op: "schedule.paint", args: `{"name":"peak","cells":[[0,24]],"on":true}`,
		refused: "Cell [0, 24] is outside day 0–6, hour 0–23"},
	{name: "paint unknown", doc: base, op: "schedule.paint", args: `{"name":"nope","cells":[],"on":true}`,
		refused: "Unknown schedule nope"},
}

func TestOpSchedule(t *testing.T) { runOps(t, scheduleCases) }

func addAccount(name, vendor, fields string) string {
	return `{"name":"` + name + `","vendor":"` + vendor + `","fields":` + fields + `}`
}

var accountCases = []opCase{
	{name: "add claude keeps ~", doc: base, op: "account.add", args: addAccount("claude-3", "claude", `{"config_dir":"~/.claude-3"}`),
		want: base.acct("claude-3", `{"vendor":"claude","config_dir":"~/.claude-3"}`)},
	{name: "add codex", doc: base, op: "account.add", args: addAccount("codex-2", "codex", `{"codex_home":"/opt/codex"}`),
		want: base.acct("codex-2", `{"vendor":"codex","codex_home":"/opt/codex"}`)},
	{name: "add opencode-go", doc: base, op: "account.add",
		args: addAccount("go-3", "opencode-go", `{"key_file":"~/.ssh/opencode-go-api","key_line":3}`),
		want: base.acct("go-3", `{"vendor":"opencode-go","key_file":"~/.ssh/opencode-go-api","key_line":3}`)},
	{name: "add ignores other vendors' fields", doc: base, op: "account.add",
		args: addAccount("claude-3", "claude", `{"config_dir":"~/.claude-3","key_line":2}`),
		want: base.acct("claude-3", `{"vendor":"claude","config_dir":"~/.claude-3"}`)},
	{name: "add missing dir", doc: base, op: "account.add", args: addAccount("claude-3", "claude", `{}`),
		refused: "Account claude-3 needs a config dir"},
	{name: "add missing home", doc: base, op: "account.add", args: addAccount("codex-2", "codex", `{"codex_home":""}`),
		refused: "Account codex-2 needs a Codex home"},
	{name: "add missing key file", doc: base, op: "account.add", args: addAccount("go-3", "opencode-go", `{"key_line":3}`),
		refused: "Account go-3 needs a key file"},
	{name: "add blank key line", doc: base, op: "account.add",
		args: addAccount("go-3", "opencode-go", `{"key_file":"~/k","key_line":""}`), refused: "Account go-3 needs a key line"},
	{name: "add fractional key line", doc: base, op: "account.add",
		args:    addAccount("go-3", "opencode-go", `{"key_file":"~/k","key_line":2.5}`),
		refused: "Account go-3: key line must be a whole number, 1 or more"},
	{name: "add key line 0", doc: base, op: "account.add",
		args:    addAccount("go-3", "opencode-go", `{"key_file":"~/k","key_line":0}`),
		refused: "Account go-3: key line must be a whole number, 1 or more"},
	{name: "add relative dir", doc: base, op: "account.add", args: addAccount("claude-3", "claude", `{"config_dir":"claude-3"}`),
		refused: "Account claude-3: config dir must be an absolute path or start with ~"},
	{name: "add ~user dir", doc: base, op: "account.add", args: addAccount("claude-3", "claude", `{"config_dir":"~bob/.claude"}`),
		refused: "Account claude-3: config dir must be an absolute path or start with ~"},
	{name: "add relative home", doc: base, op: "account.add", args: addAccount("codex-2", "codex", `{"codex_home":"./codex"}`),
		refused: "Account codex-2: Codex home must be an absolute path or start with ~"},
	{name: "add relative key file", doc: base, op: "account.add",
		args:    addAccount("go-3", "opencode-go", `{"key_file":"keys/go","key_line":1}`),
		refused: "Account go-3: key file must be an absolute path or start with ~"},
	{name: "add bare ~", doc: base, op: "account.add", args: addAccount("claude-3", "claude", `{"config_dir":"~"}`),
		want: base.acct("claude-3", `{"vendor":"claude","config_dir":"~"}`)},
	{name: "add unknown vendor", doc: base, op: "account.add", args: addAccount("g", "gemini", `{}`),
		refused: `Unknown vendor "gemini"`},
	{name: "add reserved", doc: base, op: "account.add", args: addAccount("models", "claude", `{"config_dir":"~/.c"}`),
		refused: "models is a reserved account name"},
	{name: "add bad name", doc: base, op: "account.add", args: addAccount("My Account", "claude", `{"config_dir":"~/.c"}`),
		refused: `Account name "My Account": use a–z, 0–9 and -, starting with a letter or digit`},
	{name: "add taken", doc: base, op: "account.add", args: addAccount("go-1", "opencode-go", `{"key_file":"~/k","key_line":4}`),
		refused: "Account go-1 already exists"},
	{name: "remove", doc: base, op: "account.remove", args: `{"name":"go-9"}`, want: base.acct("go-9", "")},
	{name: "remove referenced", doc: base, op: "account.remove", args: `{"name":"claude-main"}`,
		refused: "Account claude-main is used by profile coder, choice 1"},
	{name: "remove referenced first by profile", doc: base, op: "account.remove", args: `{"name":"codex-main"}`,
		refused: "Account codex-main is used by profile coder, choice 2"},
	{name: "remove unknown", doc: base, op: "account.remove", args: `{"name":"nope"}`, refused: "Unknown account nope"},
}

func TestOpAccount(t *testing.T) { runOps(t, accountCases) }

// TestOpRefusedIsError pins that a refusal is a *Refused whose Error is the
// reason, as Task 10's gate and the HTTP layer read it.
func TestOpRefusedIsError(t *testing.T) {
	err := app.ApplyOp(mustDoc(t, base.String()), "profile.remove", json.RawMessage(`{"name":"nope"}`), testOpEnv())
	if err == nil || err.Error() != "Unknown profile nope" {
		t.Fatalf("got %v", err)
	}
}

// TestOpChoiceAddEmptyProfile covers a Doc the file cannot hold (ParseDoc
// refuses an empty profile): choice.add fills it as profile.add would.
func TestOpChoiceAddEmptyProfile(t *testing.T) {
	d := mustDoc(t, base.String())
	d.Profiles["empty"] = nil
	if err := app.ApplyOp(d, "choice.add", json.RawMessage(`{"profile":"empty"}`), testOpEnv()); err != nil {
		t.Fatal(err)
	}
	want := mustDoc(t, base.prof("empty", newClaude).String()).Encode()
	if got := d.Encode(); string(got) != string(want) {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
