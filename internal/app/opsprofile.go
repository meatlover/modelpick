package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

// listModels is the account's model list, or why there is none as a
// lower-case fragment for the caller to place.
func listModels(env OpEnv, account string) ([]adapter.Model, string) {
	if env.Models == nil {
		return nil, "cannot list the models of " + account
	}
	ms, err := env.Models(account)
	if err != nil {
		return nil, "cannot list the models of " + account + ": " + err.Error()
	}
	if len(ms) == 0 {
		return nil, "account " + account + " offers no models"
	}
	return ms, ""
}

func modelOf(ms []adapter.Model, id string) *adapter.Model {
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i]
		}
	}
	return nil
}

// fixThinking keeps c's thinking if its model offers it, else takes the
// model's first level, else null (spec §8.4).
func fixThinking(c *core.DocChoice, ms []adapter.Model) {
	var levels []string
	if m := modelOf(ms, c.Model); m != nil {
		levels = m.Thinking
	}
	if c.Thinking != nil && contains(levels, *c.Thinking) {
		return
	}
	c.Thinking = nil
	if len(levels) > 0 {
		first := levels[0]
		c.Thinking = &first
	}
}

// newChoice is the §8.4 default for account: its first model, a valid
// thinking and cap [] (every window at the default cap).
func newChoice(env OpEnv, account string) (core.DocChoice, error) {
	ms, why := listModels(env, account)
	if why != "" {
		return core.DocChoice{}, refuse("%s", capital(why))
	}
	c := core.DocChoice{Account: account, Model: ms[0].ID, Capped: true, Cap: []core.CapElem{}}
	fixThinking(&c, ms)
	return c, nil
}

func opProfileAdd(doc *core.Doc, a opArgs, env OpEnv) error {
	_, taken := doc.Profiles[a.Name]
	if err := checkNew("profile", a.Name, taken); err != nil {
		return err
	}
	if len(doc.Accounts) == 0 {
		return refuse("Profile %s needs an account", a.Name)
	}
	c, err := newChoice(env, sortedNames(doc.Accounts)[0])
	if err != nil {
		return err
	}
	if doc.Profiles == nil {
		doc.Profiles = map[string][]core.DocChoice{}
	}
	doc.Profiles[a.Name] = []core.DocChoice{c}
	return nil
}

func opProfileRemove(doc *core.Doc, a opArgs, _ OpEnv) error {
	if _, ok := doc.Profiles[a.Name]; !ok {
		return refuse("Unknown profile %s", a.Name)
	}
	delete(doc.Profiles, a.Name)
	return nil
}

// opChoiceAdd copies the last choice's account. Nothing after a position
// that always wins can be picked, so the new plain choice goes before a
// trailing uncapped choice or a trailing group with an uncapped member,
// else last. With a.Group it adds a member to that group instead.
func opChoiceAdd(doc *core.Doc, a opArgs, env OpEnv) error {
	cs, ok := doc.Profiles[a.Profile]
	if !ok {
		return refuse("Unknown profile %s", a.Profile)
	}
	if len(cs) == 0 {
		return opProfileFill(doc, a.Profile, env)
	}
	if a.Group != nil {
		return opMemberAdd(doc, a, env, cs)
	}
	last := len(cs) - 1
	c, err := newChoice(env, cs[last].Account)
	if err != nil {
		return err
	}
	at := len(cs)
	ps := positionsOf(cs)
	if trailing := ps[len(ps)-1]; alwaysWins(cs[trailing.Start:trailing.End]) {
		at = trailing.Start
	}
	out := append(append(append([]core.DocChoice{}, cs[:at]...), c), cs[at:]...)
	doc.Profiles[a.Profile] = out
	return nil
}

// alwaysWins reports whether a position (its choices) has an uncapped
// choice, so that no later position is ever picked.
func alwaysWins(position []core.DocChoice) bool {
	for _, c := range position {
		if !c.Capped {
			return true
		}
	}
	return false
}

// opProfileFill gives a profile with no choices (not a state the file can
// hold, but a Doc can) its first choice, as profile.add would.
func opProfileFill(doc *core.Doc, profile string, env OpEnv) error {
	if len(doc.Accounts) == 0 {
		return refuse("Profile %s needs an account", profile)
	}
	c, err := newChoice(env, sortedNames(doc.Accounts)[0])
	if err != nil {
		return err
	}
	doc.Profiles[profile] = []core.DocChoice{c}
	return nil
}

func opChoiceRemove(doc *core.Doc, a opArgs, _ OpEnv) error {
	cs, i, err := findChoice(doc, a.Profile, a.Index, a.Expect, a.MemberOf)
	if err != nil {
		return err
	}
	if len(cs) == 1 {
		return refuse("Profile %s needs at least one choice", a.Profile)
	}
	doc.Profiles[a.Profile] = normalizeGroups(append(append([]core.DocChoice{}, cs[:i]...), cs[i+1:]...))
	return nil
}

// opChoiceMove moves one choice to a.To, an index into the list without
// it. With a.Group it ends a member of that group and must land inside the
// group or at either edge; without, it ends plain and must not land inside
// a group.
func opChoiceMove(doc *core.Doc, a opArgs, _ OpEnv) error {
	cs, i, err := findChoice(doc, a.Profile, a.From, a.Expect, a.MemberOf)
	if err != nil {
		return err
	}
	if a.To < 0 || a.To >= len(cs) {
		return refuse("Profile %s has no choice %d", a.Profile, a.To+1)
	}
	card := cs[i]
	card.Group, card.LoadBalance = 0, ""
	if a.Group != nil {
		p, err := targetGroup(doc, a, "choice.move")
		if err != nil {
			return err
		}
		card.Group, card.LoadBalance = cs[p.Start].Group, p.LoadBalance
	}
	rest := append(append([]core.DocChoice{}, cs[:i]...), cs[i+1:]...)
	what := "Choice " + label(cs, i) + " of " + a.Profile
	if err := checkLanding(rest, a.To, card.Group, what); err != nil {
		return err
	}
	out := append(append(append([]core.DocChoice{}, rest[:a.To]...), card), rest[a.To:]...)
	doc.Profiles[a.Profile] = normalizeGroups(out)
	return nil
}

// checkLanding refuses a plain choice (group 0) landing strictly inside a
// group of rest, and a choice joining group landing outside that group's
// run; either edge of the run is inside.
func checkLanding(rest []core.DocChoice, to, group int, what string) error {
	if group == 0 {
		if to > 0 && to < len(rest) && rest[to-1].Group != 0 && rest[to-1].Group == rest[to].Group {
			return refuse("%s cannot be placed inside a load balancing group", what)
		}
		return nil
	}
	start, end := -1, -1
	for k, c := range rest {
		if c.Group == group {
			if start < 0 {
				start = k
			}
			end = k + 1
		}
	}
	if start < 0 || to < start || to > end {
		return refuse("%s must be placed inside the load balancing group it joins", what)
	}
	return nil
}

func opChoiceSet(doc *core.Doc, a opArgs, env OpEnv) error {
	cs, i, err := findChoice(doc, a.Profile, a.Index, a.Expect, a.MemberOf)
	if err != nil {
		return err
	}
	where := "Profile " + a.Profile + ", choice " + label(cs, i)
	c := cs[i]
	if err := setField(doc, env, &c, where, a); err != nil {
		return err
	}
	if len(c.SkipDuring) > 0 && len(c.AllowDuring) > 0 {
		return refuse("Use Skip or Allow, not both")
	}
	cs[i] = c
	return nil
}

func setField(doc *core.Doc, env OpEnv, c *core.DocChoice, where string, a opArgs) error {
	switch a.Field {
	case "account", "model":
		var s string
		if json.Unmarshal(a.Value, &s) != nil {
			return refuse("%s: %s must be text", where, a.Field)
		}
		if a.Field == "account" {
			return setAccount(doc, env, c, where, s)
		}
		return setModel(env, c, where, s)
	case "thinking":
		return setThinking(env, c, where, a.Value)
	case "cap":
		var err error
		c.Cap, c.Capped, err = parseCap(where, windowLabels(env, c.Account, c.Model), a.Value)
		return err
	case "skip_during", "allow_during":
		return setSchedules(doc, c, where, a.Field, a.Value)
	}
	return refuse("%s: unknown field %s", where, a.Field)
}

// setAccount keeps the model if the new account offers it, else takes the
// account's first model; then fixes the thinking.
func setAccount(doc *core.Doc, env OpEnv, c *core.DocChoice, where, account string) error {
	if _, ok := doc.Accounts[account]; !ok {
		return refuse("%s: unknown account %s", where, account)
	}
	ms, why := listModels(env, account)
	if why != "" {
		return refuse("%s: %s", where, why)
	}
	c.Account = account
	if modelOf(ms, c.Model) == nil {
		c.Model = ms[0].ID
	}
	fixThinking(c, ms)
	return nil
}

func setModel(env OpEnv, c *core.DocChoice, where, model string) error {
	ms, why := listModels(env, c.Account)
	if why != "" {
		return refuse("%s: %s", where, why)
	}
	if modelOf(ms, model) == nil {
		return refuse("%s: %s does not offer %s", where, c.Account, model)
	}
	c.Model = model
	fixThinking(c, ms)
	return nil
}

// setThinking takes a level the model offers, or null for a model with none.
func setThinking(env OpEnv, c *core.DocChoice, where string, raw json.RawMessage) error {
	ms, why := listModels(env, c.Account)
	if why != "" {
		return refuse("%s: %s", where, why)
	}
	var levels []string
	if m := modelOf(ms, c.Model); m != nil {
		levels = m.Thinking
	}
	var s *string
	if json.Unmarshal(raw, &s) != nil || (s == nil) != (len(levels) == 0) || (s != nil && !contains(levels, *s)) {
		return refuse("%s: thinking %s is not offered by %s", where, rawText(raw), c.Model)
	}
	c.Thinking = s
	return nil
}

// setSchedules sets skip_during or allow_during; an empty list is dropped.
func setSchedules(doc *core.Doc, c *core.DocChoice, where, field string, raw json.RawMessage) error {
	var list []string
	if json.Unmarshal(raw, &list) != nil || list == nil {
		return refuse("%s: schedules must be a list", where)
	}
	for _, n := range list {
		if _, ok := doc.Schedules[n]; !ok {
			return refuse("%s: unknown schedule %s", where, n)
		}
	}
	if len(list) == 0 {
		list = nil
	}
	if field == "skip_during" {
		c.SkipDuring = list
	} else {
		c.AllowDuring = list
	}
	return nil
}

func windowLabels(env OpEnv, account, model string) []string {
	if env.Windows == nil {
		return nil
	}
	return env.Windows(account, model)
}

var paceRE = regexp.MustCompile(`^pace\+(\d+)$`)

// parseCap reads a cap list (null = uncapped); prefix is "Profile p, choice
// n" and labels name the positions that have a window.
func parseCap(prefix string, labels []string, raw json.RawMessage) ([]core.CapElem, bool, error) {
	if rawText(raw) == "null" {
		return nil, false, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false, refuse("%s: cap must be a list or null", prefix)
	}
	out := []core.CapElem{}
	for k, it := range items {
		e, err := capElement(capWhere(prefix, labels, k), it)
		if err != nil {
			return nil, false, err
		}
		out = append(out, e)
	}
	return out, true, nil
}

// capWhere names cap position k by its window label, else by number
// (owner, 2026-10-23, N13).
func capWhere(prefix string, labels []string, k int) string {
	if k < len(labels) {
		return prefix + ", " + labels[k] + " cap"
	}
	return prefix + ", cap " + strconv.Itoa(k+1)
}

// capElement reads one element: a number 0–100, "pace+N" with N 5–50, or
// "default" (owner rulings, rounds 3, 5 and 6).
func capElement(where string, raw json.RawMessage) (core.CapElem, error) {
	text := rawText(raw)
	var n float64
	if text != "null" && json.Unmarshal(raw, &n) == nil {
		if n < 0 || n > 100 {
			return core.CapElem{}, refuse("%s: %s is outside 0–100%%", where, text)
		}
		return core.CapElem{Kind: core.CapFixed, N: n}, nil
	}
	var s string
	if text != "null" && json.Unmarshal(raw, &s) == nil {
		if s == "default" {
			return core.CapElem{Kind: core.CapDefault}, nil
		}
		if m := paceRE.FindStringSubmatch(s); m != nil {
			v, err := strconv.Atoi(m[1])
			if err != nil || v < core.MinPaceMargin || v > core.MaxPaceMargin {
				return core.CapElem{}, refuse("%s: %s is outside pace+%d–pace+%d", where, s, core.MinPaceMargin, core.MaxPaceMargin)
			}
			return core.CapElem{Kind: core.CapPace, N: float64(v)}, nil
		}
		text = s
	}
	return core.CapElem{}, refuse("%s: %s is not a number, pace+N or default", where, text)
}

// wireChoice is a choice as the page sends it in expect. Group and
// LoadBalance say whether the page saw it as a group member.
type wireChoice struct {
	Account     string          `json:"account"`
	Model       string          `json:"model"`
	Thinking    *string         `json:"thinking"`
	Cap         json.RawMessage `json:"cap"`
	SkipDuring  []string        `json:"skip_during"`
	AllowDuring []string        `json:"allow_during"`
	Group       *int            `json:"group"`
	LoadBalance *string         `json:"load_balance"`
}

// expectedChoice decodes expect; false when it is missing or malformed. A
// member (group set and not 0, load_balance set and not "") comes back with
// Group 1 and its LoadBalance; the page's group id itself is not kept, as
// ids renumber.
func expectedChoice(raw json.RawMessage) (core.DocChoice, bool) {
	var w wireChoice
	if len(raw) == 0 || json.Unmarshal(raw, &w) != nil {
		return core.DocChoice{}, false
	}
	c := core.DocChoice{Account: w.Account, Model: w.Model, Thinking: w.Thinking,
		SkipDuring: w.SkipDuring, AllowDuring: w.AllowDuring}
	if w.Group != nil && *w.Group != 0 && w.LoadBalance != nil && *w.LoadBalance != "" {
		c.Group, c.LoadBalance = 1, *w.LoadBalance
	}
	if w.Cap == nil || rawText(w.Cap) == "null" {
		return c, true
	}
	var items []json.RawMessage
	if json.Unmarshal(w.Cap, &items) != nil {
		return core.DocChoice{}, false
	}
	c.Capped = true
	for _, it := range items {
		e, err := core.ParseCapElem(it)
		if err != nil {
			return core.DocChoice{}, false
		}
		c.Cap = append(c.Cap, e)
	}
	return c, true
}

// findChoice resolves a choice the page addressed by index and sent as
// expect (spec §8.4): the choice at index if identical, else the one
// identical choice in the profile, else a refusal. Identity includes
// whether the page saw the choice plain or as a member (and the member's
// algorithm), and a member is looked for only inside the group memberOf
// names (expectScope), so a stale edit never reaches into, out of or across
// a group (spec §8); group ids are not identity, as they renumber.
func findChoice(doc *core.Doc, profile string, index int, expect, memberOf json.RawMessage) ([]core.DocChoice, int, error) {
	cs, ok := doc.Profiles[profile]
	if !ok {
		return nil, 0, refuse("Unknown profile %s", profile)
	}
	want, ok := expectedChoice(expect)
	lo, hi, err := expectScope(doc, profile, index, want, memberOf)
	if err != nil {
		return nil, 0, err
	}
	match := func(i int) bool { return ok && sameStatus(cs[i], want) && sameChoice(cs[i], want) }
	if index >= lo && index < hi && match(index) {
		return cs, index, nil
	}
	found, n := -1, 0
	for i := lo; i < hi; i++ {
		if match(i) {
			found, n = i, n+1
		}
	}
	if n == 1 {
		return cs, found, nil
	}
	if index < 0 || index >= len(cs) {
		return nil, 0, refuse("Profile %s has no choice %d", profile, index+1)
	}
	return nil, 0, refuse("Choice %s of %s was changed outside the UI", label(cs, index), profile)
}

// expectScope is the flat range [lo, hi) findChoice searches for want: the
// whole profile for a plain choice (a member never matches it); for a
// member, the group memberOf names, resolved as group.dissolve resolves its
// expect, or without memberOf only index itself (spec §8: the fallback
// stays on the same side of a group boundary).
func expectScope(doc *core.Doc, profile string, index int, want core.DocChoice, memberOf json.RawMessage) (int, int, error) {
	cs := doc.Profiles[profile]
	if want.Group == 0 {
		return 0, len(cs), nil
	}
	if len(memberOf) == 0 || rawText(memberOf) == "null" {
		if index < 0 || index >= len(cs) {
			return 0, 0, nil
		}
		return index, index + 1, nil
	}
	_, p, err := findGroup(doc, profile, index, memberOf)
	return p.Start, p.End, err
}

// sameStatus is true when both are plain, or both are members of a group
// with the same algorithm.
func sameStatus(have, want core.DocChoice) bool {
	return (have.Group != 0) == (want.Group != 0) && have.LoadBalance == want.LoadBalance
}

// sameChoice compares as written: null and [] lists alike, 90 and 90.0
// alike, trailing "default" positions ignored; uncapped differs from [].
// Group and LoadBalance are not compared here: findChoice adds sameStatus.
func sameChoice(a, b core.DocChoice) bool {
	if a.Account != b.Account || a.Model != b.Model || a.Capped != b.Capped {
		return false
	}
	if (a.Thinking == nil) != (b.Thinking == nil) || (a.Thinking != nil && *a.Thinking != *b.Thinking) {
		return false
	}
	return sameCap(a.Cap, b.Cap) && sameList(a.SkipDuring, b.SkipDuring) && sameList(a.AllowDuring, b.AllowDuring)
}

func sameCap(a, b []core.CapElem) bool {
	a, b = trimDefaults(a), trimDefaults(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func trimDefaults(cap []core.CapElem) []core.CapElem {
	n := len(cap)
	for n > 0 && cap[n-1].Kind == core.CapDefault {
		n--
	}
	return cap[:n]
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// describe names flat choice i of profile cs for a refusal by its label
// ("choice 2b").
func describe(profile string, cs []core.DocChoice, i int) string {
	return fmt.Sprintf("profile %s, choice %s", profile, label(cs, i))
}
