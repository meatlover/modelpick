package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Doc is the config file as written: unexpanded paths, schedules as rules,
// settings keys present. A nil map means the file had no such section.
type Doc struct {
	Version   int
	Settings  map[string]float64 // only keys the file had (+ default_cap once set)
	Accounts  map[string]DocAccount
	Schedules map[string][]DocRule
	Profiles  map[string][]DocChoice
}

// DocAccount is one account as written.
type DocAccount struct {
	Vendor    string `json:"vendor"`
	ConfigDir string `json:"config_dir"`
	CodexHome string `json:"codex_home"`
	KeyFile   string `json:"key_file"`
	KeyLine   int    `json:"key_line"`
}

// DocChoice is one choice as written. Cap is written only when Capped.
type DocChoice struct {
	Account, Model string
	Thinking       *string   // nil = JSON null
	Cap            []CapElem // nil with Capped=false: no "cap" key
	Capped         bool
	SkipDuring     []string
	AllowDuring    []string
	// Group is 0 for a plain choice. Members of one load balancing group
	// share an id > 0 and are contiguous; ids run 1, 2, … in file order.
	Group       int
	LoadBalance string // the group's algorithm as written; "" when Group is 0
}

// ParseDoc decodes data structurally: strict keys and types, no cross-checks
// (versions, vendors, references and ranges are Parse's business). Errors are
// *ConfigError with an empty Path.
func ParseDoc(data []byte) (*Doc, error) { return parser{}.decodeDoc(data) }

type rawSettings struct {
	MinCallIntervalMinutes *float64 `json:"min_call_interval_minutes"`
	DefaultCap             *float64 `json:"default_cap"`
}

type rawChoice struct {
	Account     *string         `json:"account"`
	Model       *string         `json:"model"`
	Thinking    json.RawMessage `json:"thinking"`
	Cap         json.RawMessage `json:"cap"`
	SkipDuring  []string        `json:"skip_during"`
	AllowDuring []string        `json:"allow_during"`
}

type rawTop struct {
	Version   *int                       `json:"version"`
	Settings  *rawSettings               `json:"settings"`
	Accounts  map[string]json.RawMessage `json:"accounts"`
	Schedules map[string]json.RawMessage `json:"schedules"`
	Profiles  map[string]json.RawMessage `json:"profiles"`
}

func (p parser) decodeDoc(data []byte) (*Doc, error) {
	var top rawTop
	if err := p.strict(data, &top, ""); err != nil {
		return nil, err
	}
	d := &Doc{}
	if top.Version != nil {
		d.Version = *top.Version
	}
	if top.Settings != nil {
		d.Settings = map[string]float64{}
		if v := top.Settings.MinCallIntervalMinutes; v != nil {
			d.Settings["min_call_interval_minutes"] = *v
		}
		if v := top.Settings.DefaultCap; v != nil {
			d.Settings["default_cap"] = *v
		}
	}
	var err error
	if d.Accounts, err = p.decodeAccounts(top.Accounts); err != nil {
		return nil, err
	}
	if d.Schedules, err = p.decodeSchedules(top.Schedules); err != nil {
		return nil, err
	}
	if d.Profiles, err = p.decodeProfiles(top.Profiles); err != nil {
		return nil, err
	}
	return d, nil
}

func (p parser) decodeAccounts(raws map[string]json.RawMessage) (map[string]DocAccount, error) {
	if raws == nil {
		return nil, nil
	}
	out := map[string]DocAccount{}
	for _, name := range sortedKeys(raws) {
		var a DocAccount
		if err := p.strict(raws[name], &a, "accounts."+name); err != nil {
			return nil, err
		}
		out[name] = a
	}
	return out, nil
}

func (p parser) decodeSchedules(raws map[string]json.RawMessage) (map[string][]DocRule, error) {
	if raws == nil {
		return nil, nil
	}
	out := map[string][]DocRule{}
	for _, name := range sortedKeys(raws) {
		prefix := "schedules." + name
		raw := strings.TrimSpace(string(raws[name]))
		rules, err := p.decodeRules(raw, prefix)
		if err != nil {
			return nil, err
		}
		out[name] = rules
	}
	return out, nil
}

// decodeRules reads the object form or the list form of one schedule. Each
// rule is checked on its own (days, ranges), so a bad rule is reported with
// the form it was written in.
func (p parser) decodeRules(raw, prefix string) ([]DocRule, error) {
	if !strings.HasPrefix(raw, "[") {
		var r DocRule
		if err := p.strict([]byte(raw), &r, prefix); err != nil {
			return nil, err
		}
		_, err := p.scheduleRule(r, prefix)
		return []DocRule{r}, err
	}
	var items []json.RawMessage
	if err := p.strict([]byte(raw), &items, prefix); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, p.fail(prefix, "must not be empty")
	}
	rules := make([]DocRule, len(items))
	for i, item := range items {
		at := fmt.Sprintf("%s[%d]", prefix, i)
		if err := p.strict(item, &rules[i], at); err != nil {
			return nil, err
		}
		if _, err := p.scheduleRule(rules[i], at); err != nil {
			return nil, err
		}
	}
	return rules, nil
}

func (p parser) decodeProfiles(raws map[string]json.RawMessage) (map[string][]DocChoice, error) {
	if raws == nil {
		return nil, nil
	}
	out := map[string][]DocChoice{}
	for _, name := range sortedKeys(raws) {
		prefix := "profiles." + name
		var items []json.RawMessage
		if err := p.strict(raws[name], &items, prefix); err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, p.fail(prefix, "must not be empty")
		}
		choices, err := p.decodeItems(items, prefix)
		if err != nil {
			return nil, err
		}
		out[name] = choices
	}
	return out, nil
}

// decodeItems flattens a profile's items into choices: a group becomes its
// members, tagged with the group's id (1, 2, … in file order) and algorithm.
func (p parser) decodeItems(items []json.RawMessage, prefix string) ([]DocChoice, error) {
	choices := make([]DocChoice, 0, len(items))
	group := 0
	for i, item := range items {
		at := fmt.Sprintf("%s[%d]", prefix, i)
		account, isGroup, err := p.itemKeys(item, at)
		switch {
		case err != nil:
			return nil, err
		case account && isGroup:
			return nil, p.fail(at, "a choice has account, a group has load_balance and choices: not both")
		case isGroup:
			group++
			members, err := p.decodeGroup(item, at, group)
			if err != nil {
				return nil, err
			}
			choices = append(choices, members...)
		case account:
			c, err := p.decodeChoice(item, at)
			if err != nil {
				return nil, err
			}
			choices = append(choices, c)
		default:
			return nil, p.neitherItem(item, at)
		}
	}
	return choices, nil
}

// itemKeys tells a choice (it has account) from a group (it has load_balance
// or choices) by the keys present.
func (p parser) itemKeys(raw json.RawMessage, prefix string) (account, group bool, err error) {
	var keys map[string]json.RawMessage
	if err := p.strict(raw, &keys, prefix); err != nil {
		return false, false, err
	}
	_, account = keys["account"]
	_, lb := keys["load_balance"]
	_, cs := keys["choices"]
	return account, lb || cs, nil
}

// neitherItem is the error for an item with no account and no group key.
// A key that neither a choice nor a group has is most likely a typo of one
// of them, so it is named, as it was before groups existed.
func (p parser) neitherItem(raw json.RawMessage, prefix string) error {
	var either struct {
		rawChoice
		rawGroup
	}
	var ce *ConfigError
	if err := p.strict(raw, &either, prefix); errors.As(err, &ce) && ce.Msg == "unknown key" {
		return err
	}
	return p.fail(prefix, "needs account (a choice) or load_balance (a group)")
}

type rawGroup struct {
	LoadBalance *string           `json:"load_balance"`
	Choices     []json.RawMessage `json:"choices"`
}

func (p parser) decodeGroup(raw json.RawMessage, prefix string, id int) ([]DocChoice, error) {
	var rg rawGroup
	if err := p.strict(raw, &rg, prefix); err != nil {
		return nil, err
	}
	if rg.LoadBalance == nil {
		return nil, p.fail(join(prefix, "load_balance"), "required")
	}
	if len(rg.Choices) < 2 {
		return nil, p.fail(join(prefix, "choices"), "needs at least 2 members")
	}
	members := make([]DocChoice, len(rg.Choices))
	for j, item := range rg.Choices {
		at := fmt.Sprintf("%s.choices[%d]", prefix, j)
		_, isGroup, err := p.itemKeys(item, at)
		if err != nil {
			return nil, err
		}
		if isGroup {
			return nil, p.fail(at, "a group cannot be a member of a group")
		}
		c, err := p.decodeChoice(item, at)
		if err != nil {
			return nil, err
		}
		c.Group, c.LoadBalance = id, *rg.LoadBalance
		members[j] = c
	}
	return members, nil
}

func (p parser) decodeChoice(raw json.RawMessage, prefix string) (DocChoice, error) {
	var rc rawChoice
	if err := p.strict(raw, &rc, prefix); err != nil {
		return DocChoice{}, err
	}
	c := DocChoice{SkipDuring: rc.SkipDuring, AllowDuring: rc.AllowDuring}
	if rc.Account != nil {
		c.Account = *rc.Account
	}
	if rc.Model != nil {
		c.Model = *rc.Model
	}
	if rc.Thinking == nil {
		return c, p.fail(join(prefix, "thinking"), "required (use null for none)")
	}
	if string(rc.Thinking) != "null" {
		var s string
		if err := json.Unmarshal(rc.Thinking, &s); err != nil {
			return c, p.fail(join(prefix, "thinking"), "must be a string or null")
		}
		c.Thinking = &s
	}
	return c, p.decodeCap(&c, rc.Cap, prefix)
}

func (p parser) decodeCap(c *DocChoice, raw json.RawMessage, prefix string) error {
	if raw == nil {
		return nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return p.fail(join(prefix, "cap"), "cap must not be null: omit cap for no limit")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return p.fail(join(prefix, "cap"), "must be an array")
	}
	c.Capped = true
	c.Cap = make([]CapElem, 0, len(items))
	for j, item := range items {
		e, err := ParseCapElem(item)
		if err != nil {
			return p.fail(fmt.Sprintf("%s.cap[%d]", prefix, j), "%s", err.Error())
		}
		c.Cap = append(c.Cap, e)
	}
	return nil
}

// member is one key of an object whose value is already rendered.
type member struct{ key, val string }

func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func pad(lv int) string { return strings.Repeat("  ", lv) }

// obj renders members one per line inside braces at nesting level lv.
func obj(lv int, ms []member) string {
	if len(ms) == 0 {
		return "{}"
	}
	lines := make([]string, len(ms))
	for i, m := range ms {
		lines[i] = pad(lv+1) + quote(m.key) + ": " + m.val
	}
	return "{\n" + strings.Join(lines, ",\n") + "\n" + pad(lv) + "}"
}

// arrayLines renders already-rendered items one per line inside brackets.
func arrayLines(lv int, items []string) string {
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = pad(lv+1) + it
	}
	return "[\n" + strings.Join(lines, ",\n") + "\n" + pad(lv) + "]"
}

// scalars renders scalars on one line: ["a", "b"].
func scalars[T any](items []T, render func(T) string) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = render(it)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func strs(items []string) string { return scalars(items, quote) }

func mapObj[V any](lv int, m map[string]V, render func(lv int, v V) string) string {
	ms := make([]member, 0, len(m))
	for _, k := range sortedKeys(m) {
		ms = append(ms, member{k, render(lv+1, m[k])})
	}
	return obj(lv, ms)
}

// Encode is the canonical form (spec §8.4 "Writing"): 2-space indent, keys in a
// fixed order, names sorted, trailing newline. Sections the Doc has (non-nil)
// are written; schedules are written as they stand.
func (d *Doc) Encode() []byte {
	ms := []member{{"version", strconv.Itoa(d.Version)}}
	if d.Settings != nil {
		ms = append(ms, member{"settings", mapObj(1, d.Settings, func(_ int, v float64) string { return num(v) })})
	}
	if d.Accounts != nil {
		ms = append(ms, member{"accounts", mapObj(1, d.Accounts, encodeAccount)})
	}
	if d.Schedules != nil {
		ms = append(ms, member{"schedules", mapObj(1, d.Schedules, encodeSchedule)})
	}
	if d.Profiles != nil {
		ms = append(ms, member{"profiles", mapObj(1, d.Profiles, encodeProfile)})
	}
	return []byte(obj(0, ms) + "\n")
}

func encodeAccount(lv int, a DocAccount) string {
	ms := []member{{"vendor", quote(a.Vendor)}}
	for _, f := range []struct{ key, val string }{
		{"config_dir", a.ConfigDir}, {"codex_home", a.CodexHome}, {"key_file", a.KeyFile},
	} {
		if f.val != "" {
			ms = append(ms, member{f.key, quote(f.val)})
		}
	}
	if a.KeyLine != 0 {
		ms = append(ms, member{"key_line", strconv.Itoa(a.KeyLine)})
	}
	return obj(lv, ms)
}

func encodeSchedule(lv int, rules []DocRule) string {
	switch len(rules) {
	case 0:
		return `{"days": [], "utc": []}`
	case 1:
		return encodeRule(lv, rules[0])
	}
	items := make([]string, len(rules))
	for i, r := range rules {
		items[i] = encodeRule(lv+1, r)
	}
	return arrayLines(lv, items)
}

func encodeRule(lv int, r DocRule) string {
	if len(r.Days) == 0 && len(r.UTC) == 0 {
		return `{"days": [], "utc": []}`
	}
	return obj(lv, []member{{"days", strs(r.Days)}, {"utc", strs(r.UTC)}})
}

// encodeProfile writes each group as one item, its members in the choice
// layout one level deeper.
func encodeProfile(lv int, choices []DocChoice) string {
	groups, lbs := DocGroups(choices)
	var items []string
	for _, pos := range Positions(groups, lbs) {
		if groups[pos.Start] == 0 {
			items = append(items, encodeChoice(lv+1, choices[pos.Start]))
			continue
		}
		members := make([]string, 0, pos.End-pos.Start)
		for _, c := range choices[pos.Start:pos.End] {
			members = append(members, encodeChoice(lv+3, c))
		}
		items = append(items, obj(lv+1, []member{{"load_balance", quote(pos.LoadBalance)}, {"choices", arrayLines(lv+2, members)}}))
	}
	return arrayLines(lv, items)
}

func encodeChoice(lv int, c DocChoice) string {
	thinking := "null"
	if c.Thinking != nil {
		thinking = quote(*c.Thinking)
	}
	ms := []member{{"account", quote(c.Account)}, {"model", quote(c.Model)}, {"thinking", thinking}}
	if c.Capped {
		ms = append(ms, member{"cap", scalars(trimDefaults(c.Cap), encodeCapElem)})
	}
	if len(c.SkipDuring) > 0 {
		ms = append(ms, member{"skip_during", strs(c.SkipDuring)})
	}
	if c.AllowDuring != nil {
		ms = append(ms, member{"allow_during", strs(c.AllowDuring)})
	}
	return obj(lv, ms)
}

// trimDefaults drops trailing "default" elements: they mean what an absent
// position means.
func trimDefaults(cap []CapElem) []CapElem {
	n := len(cap)
	for n > 0 && cap[n-1].Kind == CapDefault {
		n--
	}
	return cap[:n]
}

func encodeCapElem(e CapElem) string {
	if e.Kind == CapFixed {
		return e.String()
	}
	return quote(e.String())
}
