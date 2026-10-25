package demo

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/meatlover/modelpick/internal/web"
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var paceRE = regexp.MustCompile(`^pace\+(\d+)$`)

type opArgs struct {
	Name    string          `json:"name"`
	Vendor  string          `json:"vendor"`
	Fields  map[string]any  `json:"fields"`
	Profile string          `json:"profile"`
	Index   int             `json:"index"`
	From    int             `json:"from"`
	To      int             `json:"to"`
	Expect  json.RawMessage `json:"expect"`
	Field   string          `json:"field"`
	Value   json.RawMessage `json:"value"`
	Force   bool            `json:"force"`
	Cells   [][2]int        `json:"cells"`
	On      bool            `json:"on"`
	// WithChoices lets account.remove take the account's choices with it.
	WithChoices bool `json:"with_choices"`
	// Indexes and Expects are group.create's chosen choices.
	Indexes []int             `json:"indexes"`
	Expects []json.RawMessage `json:"expects"`
	// Group is the flat index of any member of the group choice.add adds
	// to or choice.move joins (nil: plain); GroupExpect is that group as
	// the page saw it.
	Group       *int            `json:"group"`
	GroupExpect json.RawMessage `json:"group_expect"`
}

type opFunc func(d *Demo, a opArgs) error

var ops = map[string]opFunc{
	"settings.default_cap": (*Demo).opDefaultCap,
	"profile.add":          (*Demo).opProfileAdd,
	"profile.remove":       (*Demo).opProfileRemove,
	"choice.add":           (*Demo).opChoiceAdd,
	"choice.remove":        (*Demo).opChoiceRemove,
	"choice.move":          (*Demo).opChoiceMove,
	"choice.set":           (*Demo).opChoiceSet,
	"group.create":         (*Demo).opGroupCreate,
	"group.dissolve":       (*Demo).opGroupDissolve,
	"group.move":           (*Demo).opGroupMove,
	"schedule.add":         (*Demo).opScheduleAdd,
	"schedule.remove":      (*Demo).opScheduleRemove,
	"schedule.paint":       (*Demo).opSchedulePaint,
	"account.add":          (*Demo).opAccountAdd,
	"account.remove":       (*Demo).opAccountRemove,
}

// capital upper-cases the first letter of a kind word that opens a refusal.
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func refuse(format string, args ...any) error {
	return web.Refused{Reason: fmt.Sprintf(format, args...)}
}

// Apply runs one operation against the stored config.
func (d *Demo) Apply(req web.OpRequest) (web.OpResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if req.Base != d.sha() {
		return web.OpResult{}, web.Conflict{SHA256: d.sha()}
	}
	if d.readOnly != "" {
		return web.OpResult{}, refuse("%s", d.readOnly)
	}
	fn, ok := ops[req.Op]
	if !ok {
		return web.OpResult{}, refuse("Unknown operation %s", req.Op)
	}
	var a opArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &a); err != nil {
			return web.OpResult{}, refuse("Bad arguments for %s", req.Op)
		}
	}
	if err := fn(d, a); err != nil {
		return web.OpResult{}, err
	}
	d.sortAll()
	d.bump()
	return web.OpResult{SHA256: d.sha(), Findings: d.findings()}, nil
}

func (d *Demo) checkNew(kind, name string, taken func(string) bool) error {
	if !nameRE.MatchString(name) {
		return refuse("%s name %q: use a–z, 0–9 and -, starting with a letter or digit", capital(kind), name)
	}
	if kind == "account" && name == "models" {
		return refuse("models is a reserved account name")
	}
	if taken(name) {
		return refuse("%s %s already exists", capital(kind), name)
	}
	return nil
}

func (d *Demo) opDefaultCap(a opArgs) error {
	var v float64
	if err := json.Unmarshal(a.Value, &v); err != nil || v < 0 || v > 100 {
		return refuse("Global default cap: %s is outside 0–100%%", strings.TrimSpace(string(a.Value)))
	}
	d.defaultCap = v
	return nil
}

func (d *Demo) profile(name string) (*web.Profile, error) {
	for i := range d.profiles {
		if d.profiles[i].Name == name {
			return &d.profiles[i], nil
		}
	}
	return nil, refuse("Unknown profile %s", name)
}

// firstThinking is the model's first thinking level, or nil.
func (d *Demo) modelThinking(account, model string) []string {
	for _, m := range d.models[account] {
		if m.ID == model {
			return m.Thinking
		}
	}
	return nil
}

// newChoice is the §8.4 default for account: first model, first thinking.
func (d *Demo) newChoice(account string) web.Choice {
	m := d.models[account][0]
	c := web.Choice{Account: account, Model: m.ID, Cap: []any{},
		SkipDuring: []string{}, AllowDuring: []string{}}
	if len(m.Thinking) > 0 {
		c.Thinking = str(m.Thinking[0])
	}
	return c
}

func (d *Demo) opProfileAdd(a opArgs) error {
	err := d.checkNew("profile", a.Name, func(n string) bool { _, e := d.profile(n); return e == nil })
	if err != nil {
		return err
	}
	if len(d.accounts) == 0 {
		return refuse("Profile %s needs an account", a.Name)
	}
	d.profiles = append(d.profiles, web.Profile{Name: a.Name,
		Choices: []web.Choice{d.newChoice(d.accounts[0].Name)}})
	return nil
}

func (d *Demo) opProfileRemove(a opArgs) error {
	for i := range d.profiles {
		if d.profiles[i].Name == a.Name {
			d.profiles = append(d.profiles[:i], d.profiles[i+1:]...)
			return nil
		}
	}
	return refuse("Unknown profile %s", a.Name)
}

// opChoiceAdd copies the last choice's account, before a trailing plain
// uncapped choice (after a trailing group, as a plain choice); with a.Group
// it adds a member to that group instead.
func (d *Demo) opChoiceAdd(a opArgs) error {
	p, err := d.profile(a.Profile)
	if err != nil {
		return err
	}
	if a.Group != nil {
		return d.opMemberAdd(p, a)
	}
	last := len(p.Choices) - 1
	c := d.newChoice(p.Choices[last].Account)
	at := len(p.Choices)
	if p.Choices[last].Group == 0 && p.Choices[last].Cap == nil {
		at = last
	}
	p.Choices = append(p.Choices[:at], append([]web.Choice{c}, p.Choices[at:]...)...)
	return nil
}

// normalized makes nil list fields empty so a page's copy compares equal.
func normalized(c web.Choice) web.Choice {
	if c.SkipDuring == nil {
		c.SkipDuring = []string{}
	}
	if c.AllowDuring == nil {
		c.AllowDuring = []string{}
	}
	return c
}

func (d *Demo) opChoiceRemove(a opArgs) error {
	p, i, err := d.findChoice(a.Profile, a.Index, a.Expect)
	if err != nil {
		return err
	}
	if len(p.Choices) == 1 {
		return refuse("Profile %s needs at least one choice", p.Name)
	}
	p.Choices = normalizeGroups(append(append([]web.Choice{}, p.Choices[:i]...), p.Choices[i+1:]...))
	return nil
}

// opChoiceMove moves one choice to a.To, an index into the list without
// it. With a.Group it ends a member of that group and must land inside it
// or at either edge; without, it ends plain and must not land inside one.
func (d *Demo) opChoiceMove(a opArgs) error {
	p, i, err := d.findChoice(a.Profile, a.From, a.Expect)
	if err != nil {
		return err
	}
	cs := p.Choices
	if a.To < 0 || a.To >= len(cs) {
		return refuse("Profile %s has no choice %d", p.Name, a.To+1)
	}
	card := cs[i]
	card.Group, card.LoadBalance = 0, nil
	if a.Group != nil {
		g, err := targetGroup(p, a, "choice.move")
		if err != nil {
			return err
		}
		card.Group, card.LoadBalance = cs[g.start].Group, str(g.lb)
	}
	rest := append(append([]web.Choice{}, cs[:i]...), cs[i+1:]...)
	if err := checkLanding(rest, a.To, card.Group, "Choice "+labels(cs)[i]+" of "+p.Name); err != nil {
		return err
	}
	p.Choices = normalizeGroups(append(append(append([]web.Choice{}, rest[:a.To]...), card), rest[a.To:]...))
	return nil
}

func (d *Demo) opScheduleAdd(a opArgs) error {
	err := d.checkNew("schedule", a.Name, func(n string) bool { return d.schedule(n) != nil })
	if err != nil {
		return err
	}
	d.schedules = append(d.schedules, web.Schedule{Name: a.Name})
	return nil
}

func (d *Demo) schedule(name string) *web.Schedule {
	for i := range d.schedules {
		if d.schedules[i].Name == name {
			return &d.schedules[i]
		}
	}
	return nil
}

func (d *Demo) scheduleUses(name string) int {
	n := 0
	for _, p := range d.profiles {
		for _, c := range p.Choices {
			if contains(c.SkipDuring, name) || contains(c.AllowDuring, name) {
				n++
			}
		}
	}
	return n
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func without(list []string, s string) []string {
	out := []string{}
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func (d *Demo) opScheduleRemove(a opArgs) error {
	if d.schedule(a.Name) == nil {
		return refuse("Unknown schedule %s", a.Name)
	}
	if n := d.scheduleUses(a.Name); n > 0 && !a.Force {
		return web.Refused{Reason: fmt.Sprintf("Schedule %s is used by %s", a.Name, plural(n, "choice")), Uses: n}
	}
	for i := range d.profiles {
		for j := range d.profiles[i].Choices {
			c := &d.profiles[i].Choices[j]
			c.SkipDuring, c.AllowDuring = without(c.SkipDuring, a.Name), without(c.AllowDuring, a.Name)
		}
	}
	for i := range d.schedules {
		if d.schedules[i].Name == a.Name {
			d.schedules = append(d.schedules[:i], d.schedules[i+1:]...)
			break
		}
	}
	return nil
}

func (d *Demo) opSchedulePaint(a opArgs) error {
	s := d.schedule(a.Name)
	if s == nil {
		return refuse("Unknown schedule %s", a.Name)
	}
	for _, c := range a.Cells {
		if c[0] < 0 || c[0] > 6 || c[1] < 0 || c[1] > 23 {
			return refuse("Cell [%d, %d] is outside day 0–6, hour 0–23", c[0], c[1])
		}
	}
	v := 0.0
	if a.On {
		v = 1
	}
	for _, c := range a.Cells {
		s.Cells[c[0]][c[1]] = v
	}
	return nil
}

func (d *Demo) hasAccount(name string) bool {
	for _, a := range d.accounts {
		if a.Name == name {
			return true
		}
	}
	return false
}

// accountLocation builds the display strings; onFile is how many accounts
// read the same key file (opencode-go only).
func accountLocation(vendor string, f map[string]any, onFile int) (loc, src string) {
	dir, _ := f["config_dir"].(string)
	switch vendor {
	case "claude":
		return dir, "Keychain: Claude Code-credentials"
	case "codex":
		home, _ := f["codex_home"].(string)
		return home, "file: " + home + "/auth.json"
	}
	file, _ := f["key_file"].(string)
	line := "1"
	if n, ok := f["key_line"].(float64); ok {
		line = strconv.Itoa(int(n))
	}
	return file, "key line " + line + " of " + strconv.Itoa(onFile)
}

// relabel rebuilds every account's location and source, since an
// opencode-go source counts the accounts on its key file.
func (d *Demo) relabel() {
	onFile := map[string]int{}
	for _, a := range d.accounts {
		if f, ok := a.Fields["key_file"].(string); ok && a.Vendor == "opencode-go" {
			onFile[f]++
		}
	}
	for i := range d.accounts {
		a := &d.accounts[i]
		file, _ := a.Fields["key_file"].(string)
		a.Location, a.Source = accountLocation(a.Vendor, a.Fields, onFile[file])
	}
}

func (d *Demo) opAccountAdd(a opArgs) error {
	if err := d.checkNew("account", a.Name, d.hasAccount); err != nil {
		return err
	}
	if a.Vendor != "claude" && a.Vendor != "codex" && a.Vendor != "opencode-go" {
		return refuse("Unknown vendor %q", a.Vendor)
	}
	if a.Fields == nil {
		a.Fields = map[string]any{}
	}
	acct := web.Account{Name: a.Name, Vendor: a.Vendor, Fields: a.Fields}
	d.accounts = append(d.accounts, acct)
	d.relabel()
	d.addAccountModels(acct)
	return nil
}

func (d *Demo) opAccountRemove(a opArgs) error {
	if !d.hasAccount(a.Name) {
		return refuse("Unknown account %s", a.Name)
	}
	if a.WithChoices {
		d.dropChoicesOf(a.Name)
	}
	for _, p := range d.profiles {
		for i, c := range p.Choices {
			if c.Account == a.Name {
				return refuse("Account %s is used by profile %s, choice %s", a.Name, p.Name, labels(p.Choices)[i])
			}
		}
	}
	for i := range d.accounts {
		if d.accounts[i].Name == a.Name {
			d.accounts = append(d.accounts[:i], d.accounts[i+1:]...)
			break
		}
	}
	d.relabel()
	delete(d.models, a.Name)
	delete(d.windows, a.Name)
	delete(d.readings, a.Name)
	return nil
}

// dropChoicesOf removes the choices that use account and the profiles left
// with none, as the real back end does; a group left with one member
// becomes plain.
func (d *Demo) dropChoicesOf(account string) {
	var profiles []web.Profile
	for _, p := range d.profiles {
		var kept []web.Choice
		for _, c := range p.Choices {
			if c.Account != account {
				kept = append(kept, c)
			}
		}
		if len(kept) == len(p.Choices) {
			profiles = append(profiles, p)
		} else if len(kept) > 0 {
			p.Choices = normalizeGroups(kept)
			profiles = append(profiles, p)
		}
	}
	d.profiles = profiles
}
