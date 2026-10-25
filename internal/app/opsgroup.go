package app

import (
	"encoding/json"
	"sort"

	"github.com/meatlover/modelpick/internal/core"
)

// newGroupAlgorithm is the algorithm group.create gives a new group. It is
// the only entry of core.Algorithms today; the ui will offer the others
// once a second one exists (load balancing spec §8).
const newGroupAlgorithm = "random"

// groupsOf returns each choice's Group and LoadBalance, the input of
// core.Positions and core.Labels.
func groupsOf(cs []core.DocChoice) ([]int, []string) {
	groups, lbs := make([]int, len(cs)), make([]string, len(cs))
	for i, c := range cs {
		groups[i], lbs[i] = c.Group, c.LoadBalance
	}
	return groups, lbs
}

func positionsOf(cs []core.DocChoice) []core.Position {
	return core.Positions(groupsOf(cs))
}

// label names flat choice i as doctor and the ui do: "2", or "2b" for a
// member of the group at position 2.
func label(cs []core.DocChoice, i int) string {
	groups, _ := groupsOf(cs)
	return core.Labels(groups)[i]
}

// positionOf is the number (0-based) of the position that holds flat
// choice i.
func positionOf(ps []core.Position, i int) int {
	for n, p := range ps {
		if i >= p.Start && i < p.End {
			return n
		}
	}
	return -1
}

// normalizeGroups keeps a profile in a shape the file can hold (spec §8
// "Fewer than two"): a group left with one member becomes a plain choice at
// its place (a group with none is already gone), groups are numbered 1, 2, …
// in order, and every member carries its group's first member's algorithm.
// Every op that changes a profile's list runs it.
func normalizeGroups(cs []core.DocChoice) []core.DocChoice {
	out := append([]core.DocChoice{}, cs...)
	id := 0
	for _, p := range positionsOf(out) {
		switch {
		case out[p.Start].Group == 0:
		case p.End-p.Start == 1:
			out[p.Start].Group, out[p.Start].LoadBalance = 0, ""
		default:
			id++
			for i := p.Start; i < p.End; i++ {
				out[i].Group, out[i].LoadBalance = id, p.LoadBalance
			}
		}
	}
	return out
}

// targetGroup resolves the group argument of choice.add and choice.move
// (a.Group != nil): a.Group is any member's flat index and a.GroupExpect the
// group as the page saw it, resolved as group.dissolve resolves its expect,
// so a hand edit cannot send the card to a group the user did not choose.
func targetGroup(doc *core.Doc, a opArgs, op string) (core.Position, error) {
	if len(a.GroupExpect) == 0 || rawText(a.GroupExpect) == "null" {
		return core.Position{}, refuse("Bad arguments for %s", op)
	}
	_, p, err := findGroup(doc, a.Profile, *a.Group, a.GroupExpect)
	return p, err
}

// opGroupCreate makes one group of the chosen plain choices at the place of
// the first of them; the others move up into it in file order.
func opGroupCreate(doc *core.Doc, a opArgs, _ OpEnv) error {
	if len(a.Indexes) != len(a.Expects) {
		return refuse("Bad arguments for group.create")
	}
	if len(a.Indexes) < 2 {
		return refuse("Load balancing needs at least 2 choices")
	}
	cs, picked, err := resolveSelection(doc, a)
	if err != nil {
		return err
	}
	sort.Ints(picked)
	chosen := map[int]bool{}
	for _, i := range picked {
		chosen[i] = true
	}
	out := make([]core.DocChoice, 0, len(cs))
	for i, c := range cs {
		if i == picked[0] {
			out = append(out, newGroup(cs, picked)...)
		}
		if !chosen[i] {
			out = append(out, c)
		}
	}
	doc.Profiles[a.Profile] = normalizeGroups(out)
	return nil
}

// resolveSelection finds each chosen choice by index and expect, refusing a
// choice chosen twice or one already in a group.
func resolveSelection(doc *core.Doc, a opArgs) ([]core.DocChoice, []int, error) {
	var cs []core.DocChoice
	seen := map[int]bool{}
	picked := make([]int, 0, len(a.Indexes))
	for k, index := range a.Indexes {
		list, i, err := findChoice(doc, a.Profile, index, a.Expects[k])
		if err != nil {
			return nil, nil, err
		}
		cs = list
		if seen[i] {
			return nil, nil, refuse("Choice %s of %s is selected twice", label(cs, i), a.Profile)
		}
		if cs[i].Group != 0 {
			return nil, nil, refuse("Choice %s of %s is already in a load balancing group", label(cs, i), a.Profile)
		}
		seen[i] = true
		picked = append(picked, i)
	}
	return cs, picked, nil
}

// newGroup is the members picked (sorted flat indexes into cs), tagged with
// an id no group of cs uses; normalizeGroups renumbers it.
func newGroup(cs []core.DocChoice, picked []int) []core.DocChoice {
	id := 0
	for _, c := range cs {
		id = max(id, c.Group)
	}
	members := make([]core.DocChoice, len(picked))
	for k, i := range picked {
		members[k] = cs[i]
		members[k].Group, members[k].LoadBalance = id+1, newGroupAlgorithm
	}
	return members
}

func opGroupDissolve(doc *core.Doc, a opArgs, _ OpEnv) error {
	cs, p, err := findGroup(doc, a.Profile, a.Index, a.Expect)
	if err != nil {
		return err
	}
	out := append([]core.DocChoice{}, cs...)
	for i := p.Start; i < p.End; i++ {
		out[i].Group, out[i].LoadBalance = 0, ""
	}
	doc.Profiles[a.Profile] = normalizeGroups(out)
	return nil
}

// opGroupMove moves a whole group; a.To is a position number (0-based)
// among the positions without the group, as choice.move's to counts
// choices without the moved one.
func opGroupMove(doc *core.Doc, a opArgs, _ OpEnv) error {
	cs, p, err := findGroup(doc, a.Profile, a.Index, a.Expect)
	if err != nil {
		return err
	}
	rest := append(append([]core.DocChoice{}, cs[:p.Start]...), cs[p.End:]...)
	ps := positionsOf(rest)
	if a.To < 0 || a.To > len(ps) {
		return refuse("Profile %s has no position %d", a.Profile, a.To+1)
	}
	at := len(rest)
	if a.To < len(ps) {
		at = ps[a.To].Start
	}
	out := append(append(append([]core.DocChoice{}, rest[:at]...), cs[p.Start:p.End]...), rest[at:]...)
	doc.Profiles[a.Profile] = normalizeGroups(out)
	return nil
}

// wireGroup is a group as the page sends it in expect: the file's shape.
type wireGroup struct {
	LoadBalance string            `json:"load_balance"`
	Choices     []json.RawMessage `json:"choices"`
}

// expectedGroup decodes a group's expect; false when it is missing or
// malformed.
func expectedGroup(raw json.RawMessage) (string, []core.DocChoice, bool) {
	var w wireGroup
	if len(raw) == 0 || json.Unmarshal(raw, &w) != nil {
		return "", nil, false
	}
	members := make([]core.DocChoice, len(w.Choices))
	for k, it := range w.Choices {
		c, ok := expectedChoice(it)
		if !ok {
			return "", nil, false
		}
		members[k] = c
	}
	return w.LoadBalance, members, true
}

// sameGroup compares a group in the Doc with an expected one: the same
// algorithm and the same members in order.
func sameGroup(cs []core.DocChoice, p core.Position, lb string, want []core.DocChoice) bool {
	if cs[p.Start].Group == 0 || p.LoadBalance != lb || p.End-p.Start != len(want) {
		return false
	}
	for k, w := range want {
		if !sameChoice(cs[p.Start+k], w) {
			return false
		}
	}
	return true
}

// findGroup resolves a group the page addressed by any member's index and
// sent whole as expect (spec §8): the group at index if identical, else the
// one identical group in the profile, else a refusal.
func findGroup(doc *core.Doc, profile string, index int, expect json.RawMessage) ([]core.DocChoice, core.Position, error) {
	cs, ok := doc.Profiles[profile]
	if !ok {
		return nil, core.Position{}, refuse("Unknown profile %s", profile)
	}
	lb, want, ok := expectedGroup(expect)
	ps := positionsOf(cs)
	inRange := index >= 0 && index < len(cs)
	if ok && inRange && sameGroup(cs, ps[positionOf(ps, index)], lb, want) {
		return cs, ps[positionOf(ps, index)], nil
	}
	var found core.Position
	n := 0
	for _, p := range ps {
		if ok && sameGroup(cs, p, lb, want) {
			found, n = p, n+1
		}
	}
	if n == 1 {
		return cs, found, nil
	}
	if !inRange {
		return nil, core.Position{}, refuse("Profile %s has no choice %d", profile, index+1)
	}
	return nil, core.Position{}, refuse("Load balancing group %d of %s was changed outside the UI", positionOf(ps, index)+1, profile)
}

// opMemberAdd adds a member at the end of the group a.Group names. It uses
// an account the group does not use yet, preferring the last member's
// vendor, with that account's first model; when the group uses every
// account it copies the last member's account as choice.add does (spec §8).
func opMemberAdd(doc *core.Doc, a opArgs, env OpEnv, cs []core.DocChoice) error {
	p, err := targetGroup(doc, a, "choice.add")
	if err != nil {
		return err
	}
	last := cs[p.End-1]
	c, err := newChoice(env, memberAccount(doc, cs[p.Start:p.End]))
	if err != nil {
		return err
	}
	c.Group, c.LoadBalance = last.Group, last.LoadBalance
	doc.Profiles[a.Profile] = append(append(append([]core.DocChoice{}, cs[:p.End]...), c), cs[p.End:]...)
	return nil
}

// memberAccount is the account a new member of members uses.
func memberAccount(doc *core.Doc, members []core.DocChoice) string {
	used := map[string]bool{}
	for _, m := range members {
		used[m.Account] = true
	}
	last := members[len(members)-1].Account
	vendor := doc.Accounts[last].Vendor
	other := ""
	for _, name := range sortedNames(doc.Accounts) {
		switch {
		case used[name]:
		case doc.Accounts[name].Vendor == vendor:
			return name
		case other == "":
			other = name
		}
	}
	if other != "" {
		return other
	}
	return last
}
