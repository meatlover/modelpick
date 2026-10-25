package demo

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/meatlover/modelpick/internal/web"
)

// Load balancing groups over the wire types, mirroring the live ops
// (internal/app/opsgroup.go, opsprofile.go; the wire shape and the op
// contract in api.go, items 19 and 20). This package may import only internal/web (archtest), so the
// few pure helpers core has (Positions, Labels) are repeated here, small;
// webtest's group scenarios hold both back ends to the same results.

// newGroupAlgorithm is what group.create gives a new group, as live does.
const newGroupAlgorithm = "random"

// position is one place in a profile: a plain choice (end = start+1, lb
// "") or a group, whose members are choices start..end-1.
type position struct {
	start, end int
	lb         string
}

func lbOf(c web.Choice) string {
	if c.LoadBalance == nil {
		return ""
	}
	return *c.LoadBalance
}

func positions(cs []web.Choice) []position {
	var out []position
	for start := 0; start < len(cs); {
		p := position{start: start, end: start + 1}
		if cs[start].Group != 0 {
			for p.end < len(cs) && cs[p.end].Group == cs[start].Group {
				p.end++
			}
			p.lb = lbOf(cs[start])
		}
		out = append(out, p)
		start = p.end
	}
	return out
}

func positionOf(ps []position, i int) int {
	for n, p := range ps {
		if i >= p.start && i < p.end {
			return n
		}
	}
	return -1
}

// labels names each choice as doctor and the page do: "2", or "2b" for
// the second member of the group at position 2.
func labels(cs []web.Choice) []string {
	out := make([]string, len(cs))
	for n, p := range positions(cs) {
		num := strconv.Itoa(n + 1)
		if cs[p.start].Group == 0 {
			out[p.start] = num
			continue
		}
		for i := p.start; i < p.end; i++ {
			s := ""
			for k := i - p.start + 1; k > 0; k = (k - 1) / 26 {
				s = string(rune('a'+(k-1)%26)) + s
			}
			out[i] = num + s
		}
	}
	return out
}

// normalizeGroups keeps a profile in a shape the file can hold: a group
// left with one member becomes plain at its place, groups are numbered 1,
// 2, … in order, and every member carries its group's first algorithm.
func normalizeGroups(cs []web.Choice) []web.Choice {
	out := append([]web.Choice{}, cs...)
	id := 0
	for _, p := range positions(out) {
		switch {
		case out[p.start].Group == 0:
		case p.end-p.start == 1:
			out[p.start].Group, out[p.start].LoadBalance = 0, nil
		default:
			id++
			for i := p.start; i < p.end; i++ {
				out[i].Group, out[i].LoadBalance = id, str(p.lb)
			}
		}
	}
	return out
}

// expectedChoice decodes a choice's expect. A member (group not 0 and
// load_balance not empty) comes back with group 1 and its algorithm, as
// group ids are not identity; anything else comes back plain.
func expectedChoice(raw json.RawMessage) (web.Choice, bool) {
	var c web.Choice
	if len(raw) == 0 || json.Unmarshal(raw, &c) != nil {
		return web.Choice{}, false
	}
	if c.Group != 0 && lbOf(c) != "" {
		c.Group = 1
	} else {
		c.Group, c.LoadBalance = 0, nil
	}
	return c, true
}

// sameStatus: both plain, or both members of a group with one algorithm.
func sameStatus(have, want web.Choice) bool {
	return (have.Group != 0) == (want.Group != 0) && lbOf(have) == lbOf(want)
}

// sameChoice compares the choices themselves, null and [] lists alike;
// membership is sameStatus's.
func sameChoice(a, b web.Choice) bool {
	a.Group, a.LoadBalance, b.Group, b.LoadBalance = 0, nil, 0, nil
	return reflect.DeepEqual(normalized(a), normalized(b))
}

// findChoice resolves a choice addressed by index and sent as expect: the
// choice at index if identical, else the one identical choice elsewhere,
// else a refusal. Identity includes membership, so a stale edit never
// reaches into or out of a group.
func (d *Demo) findChoice(profile string, index int, expect json.RawMessage) (*web.Profile, int, error) {
	p, err := d.profile(profile)
	if err != nil {
		return nil, 0, err
	}
	cs := p.Choices
	want, ok := expectedChoice(expect)
	match := func(i int) bool { return ok && sameStatus(cs[i], want) && sameChoice(cs[i], want) }
	inRange := index >= 0 && index < len(cs)
	if inRange && match(index) {
		return p, index, nil
	}
	found, n := -1, 0
	for i := range cs {
		if match(i) {
			found, n = i, n+1
		}
	}
	if n == 1 {
		return p, found, nil
	}
	if !inRange {
		return nil, 0, refuse("Profile %s has no choice %d", profile, index+1)
	}
	return nil, 0, refuse("Choice %s of %s was changed outside the UI", labels(cs)[index], profile)
}

// expectedGroup decodes a group's expect, {load_balance, choices}.
func expectedGroup(raw json.RawMessage) (string, []web.Choice, bool) {
	var w struct {
		LoadBalance string            `json:"load_balance"`
		Choices     []json.RawMessage `json:"choices"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &w) != nil {
		return "", nil, false
	}
	members := make([]web.Choice, len(w.Choices))
	for k, it := range w.Choices {
		c, ok := expectedChoice(it)
		if !ok {
			return "", nil, false
		}
		members[k] = c
	}
	return w.LoadBalance, members, true
}

func sameGroup(cs []web.Choice, p position, lb string, want []web.Choice) bool {
	if cs[p.start].Group == 0 || p.lb != lb || p.end-p.start != len(want) {
		return false
	}
	for k, w := range want {
		if !sameChoice(cs[p.start+k], w) {
			return false
		}
	}
	return true
}

// findGroup resolves a group addressed by any member's index and sent
// whole as expect: the group at index if identical, else the one identical
// group, else a refusal.
func findGroup(p *web.Profile, index int, expect json.RawMessage) (position, error) {
	cs := p.Choices
	lb, want, ok := expectedGroup(expect)
	ps := positions(cs)
	inRange := index >= 0 && index < len(cs)
	if ok && inRange && sameGroup(cs, ps[positionOf(ps, index)], lb, want) {
		return ps[positionOf(ps, index)], nil
	}
	var found position
	n := 0
	for _, q := range ps {
		if ok && sameGroup(cs, q, lb, want) {
			found, n = q, n+1
		}
	}
	if n == 1 {
		return found, nil
	}
	if !inRange {
		return position{}, refuse("Profile %s has no choice %d", p.Name, index+1)
	}
	return position{}, refuse("Load balancing group %d of %s was changed outside the UI", positionOf(ps, index)+1, p.Name)
}

// targetGroup resolves choice.add's and choice.move's group argument.
func targetGroup(p *web.Profile, a opArgs, op string) (position, error) {
	if len(a.GroupExpect) == 0 || strings.TrimSpace(string(a.GroupExpect)) == "null" {
		return position{}, refuse("Bad arguments for %s", op)
	}
	return findGroup(p, *a.Group, a.GroupExpect)
}

// checkLanding refuses a plain choice (group 0) landing strictly inside a
// group of rest, and a joining choice landing outside its group's run.
func checkLanding(rest []web.Choice, to, group int, what string) error {
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

func (d *Demo) opGroupCreate(a opArgs) error {
	if len(a.Indexes) != len(a.Expects) {
		return refuse("Bad arguments for group.create")
	}
	if len(a.Indexes) < 2 {
		return refuse("Load balancing needs at least 2 choices")
	}
	p, picked, err := d.resolveSelection(a)
	if err != nil {
		return err
	}
	sort.Ints(picked)
	chosen := map[int]bool{}
	for _, i := range picked {
		chosen[i] = true
	}
	cs := p.Choices
	out := make([]web.Choice, 0, len(cs))
	for i, c := range cs {
		if i == picked[0] {
			out = append(out, newGroup(cs, picked)...)
		}
		if !chosen[i] {
			out = append(out, c)
		}
	}
	p.Choices = normalizeGroups(out)
	return nil
}

// resolveSelection finds each chosen choice, refusing one chosen twice or
// one already in a group.
func (d *Demo) resolveSelection(a opArgs) (*web.Profile, []int, error) {
	var p *web.Profile
	seen := map[int]bool{}
	picked := make([]int, 0, len(a.Indexes))
	for k, index := range a.Indexes {
		q, i, err := d.findChoice(a.Profile, index, a.Expects[k])
		if err != nil {
			return nil, nil, err
		}
		p = q
		if seen[i] {
			return nil, nil, refuse("Choice %s of %s is selected twice", labels(p.Choices)[i], a.Profile)
		}
		if p.Choices[i].Group != 0 {
			return nil, nil, refuse("Choice %s of %s is already in a load balancing group", labels(p.Choices)[i], a.Profile)
		}
		seen[i] = true
		picked = append(picked, i)
	}
	return p, picked, nil
}

// newGroup is the picked choices tagged with an unused id; the normalizer
// renumbers it.
func newGroup(cs []web.Choice, picked []int) []web.Choice {
	id := 0
	for _, c := range cs {
		id = max(id, c.Group)
	}
	members := make([]web.Choice, len(picked))
	for k, i := range picked {
		members[k] = cs[i]
		members[k].Group, members[k].LoadBalance = id+1, str(newGroupAlgorithm)
	}
	return members
}

func (d *Demo) opGroupDissolve(a opArgs) error {
	p, err := d.profile(a.Profile)
	if err != nil {
		return err
	}
	g, err := findGroup(p, a.Index, a.Expect)
	if err != nil {
		return err
	}
	out := append([]web.Choice{}, p.Choices...)
	for i := g.start; i < g.end; i++ {
		out[i].Group, out[i].LoadBalance = 0, nil
	}
	p.Choices = normalizeGroups(out)
	return nil
}

// opGroupMove moves a whole group; a.To is a position number (0-based)
// among the positions without it.
func (d *Demo) opGroupMove(a opArgs) error {
	p, err := d.profile(a.Profile)
	if err != nil {
		return err
	}
	g, err := findGroup(p, a.Index, a.Expect)
	if err != nil {
		return err
	}
	cs := p.Choices
	rest := append(append([]web.Choice{}, cs[:g.start]...), cs[g.end:]...)
	ps := positions(rest)
	if a.To < 0 || a.To > len(ps) {
		return refuse("Profile %s has no position %d", a.Profile, a.To+1)
	}
	at := len(rest)
	if a.To < len(ps) {
		at = ps[a.To].start
	}
	out := append(append(append([]web.Choice{}, rest[:at]...), cs[g.start:g.end]...), rest[at:]...)
	p.Choices = normalizeGroups(out)
	return nil
}

// opMemberAdd adds a member at the end of the group a.Group names, on an
// account the group does not use yet (the last member's vendor first).
func (d *Demo) opMemberAdd(p *web.Profile, a opArgs) error {
	g, err := targetGroup(p, a, "choice.add")
	if err != nil {
		return err
	}
	last := p.Choices[g.end-1]
	c := d.newChoice(d.memberAccount(p.Choices[g.start:g.end]))
	c.Group, c.LoadBalance = last.Group, str(lbOf(last))
	p.Choices = append(append(append([]web.Choice{}, p.Choices[:g.end]...), c), p.Choices[g.end:]...)
	return nil
}

// memberAccount is the first unused account (by name) of the last
// member's vendor, else of any vendor, else the last member's.
func (d *Demo) memberAccount(members []web.Choice) string {
	used := map[string]bool{}
	for _, m := range members {
		used[m.Account] = true
	}
	last := members[len(members)-1].Account
	vendor := ""
	for _, acct := range d.accounts {
		if acct.Name == last {
			vendor = acct.Vendor
		}
	}
	other := ""
	for _, acct := range d.accounts {
		switch {
		case used[acct.Name]:
		case acct.Vendor == vendor:
			return acct.Name
		case other == "":
			other = acct.Name
		}
	}
	if other != "" {
		return other
	}
	return last
}
