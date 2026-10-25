package webtest

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/web"
)

// The load balancing scenarios (spec §8, the op contract of task 4): each
// builds profile gp from ops, so it runs on any fixture, applies one op and
// compares the resulting profile by its shape. Both back ends pass the
// same table, so the demo mirrors the live ops, refusal texts included.

// gp is the profile the group scenarios build.
const gp = "wt-g"

// step builds one op call from the state the page would hold.
type step func(t *testing.T, s web.State) opCall

type groupCase struct {
	name  string
	start string // the profile before, as shape writes it
	prep  []step // applied after start, before op
	op    step
	want  string // the shape after; "" when refused
	// refused is the exact reason when the op must be refused.
	refused string
}

func groupCases() []groupCase {
	return slices.Concat(groupCreateCases(), groupEditCases(), groupMoveCases(), groupStaleCases(), memberSetCases())
}

func groupCreateCases() []groupCase {
	return []groupCase{
		{name: "create adjacent", start: "10 20 30 40", op: create("20", "30"), want: "10 [20 30] 40"},
		{name: "create non-adjacent", start: "10 20 30 40", op: create("40", "20"), want: "10 [20 40] 30"},
		{name: "create one", start: "10 20", op: create("20"), refused: "Load balancing needs at least 2 choices"},
		{name: "create twice", start: "10 20 30", op: create("20", "20"), refused: "Choice 2 of wt-g is selected twice"},
		{name: "create member", start: "[10 20] 30", op: create("20", "30"),
			refused: "Choice 1b of wt-g is already in a load balancing group"},
		{name: "dissolve", start: "10 [20 30] 40", op: dissolve("30"), want: "10 20 30 40"},
		{name: "dissolve stale", start: "10 [20 30] 40", prep: []step{setCap("30", 31)}, op: dissolveSeen("31", 30),
			refused: "Load balancing group 2 of wt-g was changed outside the UI"},
		{name: "group move to the end", start: "10 [20 30] 40", op: groupMove("20", 2), want: "10 40 [20 30]"},
		{name: "group move to the start", start: "10 [20 30] 40", op: groupMove("30", 0), want: "[20 30] 10 40"},
		{name: "dissolve the first of two", start: "[10 20] [30 40]", op: dissolve("10"), want: "10 20 [30 40]"},
		{name: "group move past another", start: "[10 20] [30 40] 50", op: groupMove("10", 1), want: "[30 40] [10 20] 50"},
		{name: "move to another group", start: "[10 20 30] [40 50]", op: move("10", 4, "40"), want: "[20 30] [40 50 10]"},
		{name: "group move no position", start: "10 [20 30] 40", op: groupMove("30", 3),
			refused: "Profile wt-g has no position 4"},
	}
}

func groupEditCases() []groupCase {
	return []groupCase{
		{name: "plain add after a group", start: "10 [20 30]", op: plainAdd, want: "10 [20 30] _"},
		{name: "member add", start: "10 [20 30] 40", op: memberAdd("20"), want: "10 [20 30 _] 40"},
		{name: "member add without group_expect", start: "10 [20 30] 40", op: without(memberAdd("20"), "group_expect"),
			refused: "Bad arguments for choice.add"},
		{name: "remove to one member", start: "10 [20 30] 40", op: remove("20"), want: "10 30 40"},
		{name: "remove a member of three", start: "[10 20 30] 40", op: remove("10"), want: "[20 30] 40"},
		{name: "account remove leaves one", start: "10 [20 30] 40", prep: []step{toOther("30")},
			op: removeOther, want: "10 20 40"},
		{name: "account remove leaves none", start: "10 [20 30] 40", prep: []step{toOther("20"), toOther("30")},
			op: removeOther, want: "10 40"},
	}
}

func groupMoveCases() []groupCase {
	return []groupCase{
		{name: "move in at the end edge", start: "10 [20 30] 40", op: move("40", 3, "20"), want: "10 [20 30 40]"},
		{name: "move in at the start edge", start: "10 [20 30] 40", op: move("10", 0, "30"), want: "[10 20 30] 40"},
		{name: "move within the group", start: "[10 20 30] 40", op: move("30", 0, "10"), want: "[30 10 20] 40"},
		{name: "move out", start: "10 [20 30 40]", op: move("20", 0, ""), want: "20 10 [30 40]"},
		{name: "move out leaving one", start: "10 [20 30] 40", op: move("20", 3, ""), want: "10 30 40 20"},
		{name: "plain into a group", start: "10 [20 30 40]", op: move("10", 2, ""),
			refused: "Choice 1 of wt-g cannot be placed inside a load balancing group"},
		{name: "join outside the group", start: "10 [20 30] 40", op: move("40", 0, "20"),
			refused: "Choice 3 of wt-g must be placed inside the load balancing group it joins"},
		{name: "move past the end", start: "10 20 30 40", op: move("10", 4, ""), refused: "Profile wt-g has no choice 5"},
		{name: "move without group_expect", start: "10 [20 30] 40", op: without(move("40", 3, "20"), "group_expect"),
			refused: "Bad arguments for choice.move"},
	}
}

func groupStaleCases() []groupCase {
	return append(groupHandEditCases(), []groupCase{
		{name: "member sent as plain", start: "10 [20 30] 40", op: setSeen("20", 1, plain, 21),
			refused: "Choice 2a of wt-g was changed outside the UI"},
		{name: "plain sent as a member", start: "10 [20 30] 40", op: setSeen("10", 0, asMember, 11),
			refused: "Choice 1 of wt-g was changed outside the UI"},
		{name: "stale index finds the member", start: "10 [20 30] 40", op: setSeen("30", 0, same, 35),
			want: "10 [20 35] 40"},
		{name: "stale plain move does not take a member", start: "[10 30 20] 40",
			op: moveSeen("20", 1, 0, plain), refused: "Choice 1b of wt-g was changed outside the UI"},
		{name: "member expect with another algorithm", start: "[10 20] 30",
			op:      withArg(remove("10"), "expect", seenChoice("10", otherAlgorithm)),
			refused: "Choice 1a of wt-g was changed outside the UI"},
		{name: "member cannot stay inside without group", start: "[10 20 30]", op: move("20", 1, ""),
			refused: "Choice 1b of wt-g cannot be placed inside a load balancing group"},
	}...)
}

// groupHandEditCases: the page addressed a group that a hand edit has
// since moved, changed or removed. The start is the file after the edit;
// the op carries the page's older view (indexes, group expects).
func groupHandEditCases() []groupCase {
	return []groupCase{
		{name: "create expects do not match indexes", start: "10 20 30",
			op: withArg(create("10", "20"), "expects", seenChoices("10")), refused: "Bad arguments for group.create"},
		{name: "dissolve out of range", start: "[10 20] 30",
			op: withArg(dissolveSeen("20", 99), "index", 5), refused: "Profile wt-g has no choice 6"},
		{name: "dissolve follows the one identical group", start: "30 [10 20]",
			op: withArg(dissolve("10"), "index", 0), want: "30 10 20"},
		{name: "group.move stale", start: "40 [10 20] 30",
			op:      withArg(groupMove("10", 0), "expect", reversedGroup("10")),
			refused: "Load balancing group 2 of wt-g was changed outside the UI"},
		{name: "move: group out of range", start: "[10 20] 30",
			op:      withArg(withArg(move("30", 1, "10"), "group", 9), "group_expect", staleGroup("20", 99)),
			refused: "Profile wt-g has no choice 10"},
		{name: "move: group expect matches no group", start: "[10 20] 30 40",
			op:      withArg(withArg(move("40", 2, "10"), "group", 2), "group_expect", seenAsGroup("30", "40")),
			refused: "Load balancing group 2 of wt-g was changed outside the UI"},
		// The page saw 50 [10 20] [30 40] and drops 50 into [10 20]; a hand
		// edit swapped the groups. group_expect follows the group the user
		// chose, and the drop is not inside it.
		{name: "move: groups swapped by hand", start: "50 [30 40] [10 20]",
			op:      withArg(move("50", 1, "10"), "group", 1),
			refused: "Choice 1 of wt-g must be placed inside the load balancing group it joins"},
		{name: "move: group changed by hand", start: "30 [10 40]",
			op:      withArg(move("30", 0, "10"), "group_expect", staleGroup("40", 20)),
			refused: "Load balancing group 2 of wt-g was changed outside the UI"},
		{name: "add member: out of range", start: "10 20 30",
			op:      withArg(withArg(plainAdd, "group", 3), "group_expect", seenAsGroup("10", "20")),
			refused: "Profile wt-g has no choice 4"},
		{name: "add member: group expect matches no group", start: "10 20 30",
			op:      withArg(withArg(plainAdd, "group", 1), "group_expect", seenAsGroup("10", "20")),
			refused: "Load balancing group 2 of wt-g was changed outside the UI"},
		{name: "add member: groups swapped by hand", start: "[30 40] [10 20]",
			op: withArg(memberAdd("10"), "group", 0), want: "[30 40] [10 20 _]"},
	}
}

// memberSetCases: choice.set on a member names it by label, with the same
// words on both back ends.
func memberSetCases() []groupCase {
	const at = "Profile wt-g, choice 2a: "
	return []groupCase{
		{name: "set cap on a member", start: "10 [20 30]", op: setField("20", "cap", []any{25}), want: "10 [25 30]"},
		{name: "set unknown field", start: "10 [20 30]", op: setField("20", "colour", "red"),
			refused: at + "unknown field colour"},
		{name: "set unknown account", start: "10 [20 30]", op: setField("20", "account", "wt-nope"),
			refused: at + "unknown account wt-nope"},
		{name: "set schedules not a list", start: "10 [20 30]", op: setField("20", "skip_during", "x"),
			refused: at + "schedules must be a list"},
	}
}

func groupScenarios(t *testing.T, h Harness) {
	for _, c := range groupCases() {
		t.Run(c.name, func(t *testing.T) {
			b := h.New(t)
			build(t, b, c.start)
			for _, p := range c.prep {
				o := p(t, b.State())
				mustApply(t, b, o.op, o.args)
			}
			s := b.State()
			o := c.op(t, s)
			_, err := apply(t, b, s.SHA256, o.op, o.args)
			if c.refused != "" {
				if r := mustRefuse(t, err, o.op); r.Reason != c.refused {
					t.Errorf("reason %q, want %q", r.Reason, c.refused)
				}
				if b.State().SHA256 != s.SHA256 {
					t.Error("refused, but the sha moved")
				}
				return
			}
			if err != nil {
				t.Fatalf("%s %v: %v", o.op, o.args, err)
			}
			if got := shape(t, b.State()); got != c.want {
				t.Errorf("profile %s, want %s", got, c.want)
			}
		})
	}
}

// memberAddAccount: a new member uses an account the group does not use,
// of the last member's vendor first, else the first unused one, else the
// last member's, with that account's first model. "last on other" puts
// the last member on otherAccount, which separates the vendor preference
// from the name order (demo) or reaches the first unused account (live);
// "every account" gives the group a member on each account.
func memberAddAccount(t *testing.T, h Harness) {
	for _, mode := range []string{"first account", "last on other", "every account"} {
		t.Run(mode, func(t *testing.T) {
			b := h.New(t)
			n := memberSetup(t, b, mode)
			s := b.State()
			var used []string
			for _, c := range profileChoices(t, s, gp)[:n] {
				used = append(used, c.Account)
			}
			o := memberAdd("10")(t, s)
			mustApply(t, b, o.op, o.args)
			after := b.State()
			got := profileChoices(t, after, gp)[n]
			want := memberAccountFor(t, s, used)
			m := after.Models[want]
			if got.Account != want || len(m) == 0 || got.Model != m[0].ID {
				t.Errorf("members on %v: new member %s/%s, want %s and its first model", used, got.Account, got.Model, want)
			}
			if fmt.Sprint(got.Cap) != "[]" || got.Group != 1 {
				t.Errorf("new member cap %v group %d, want [] and 1", got.Cap, got.Group)
			}
		})
	}
}

// memberSetup builds gp as one group for mode and returns its size.
func memberSetup(t *testing.T, b web.Backend, mode string) int {
	t.Helper()
	accounts := b.State().Accounts
	n := 2
	if mode == "every account" {
		n = len(accounts)
	}
	tags := make([]string, n)
	for k := range tags {
		tags[k] = strconv.Itoa(10 * (k + 1))
	}
	build(t, b, "["+strings.Join(tags, " ")+"]")
	for k := 1; k < n; k++ {
		acct := accounts[k].Name
		if mode == "last on other" {
			acct = otherAccount(t, b.State())
		}
		o := setField(tags[k], "account", acct)(t, b.State())
		mustApply(t, b, o.op, o.args)
	}
	return n
}

// memberAccountFor is the contract's choice for a group whose members use
// used, the last one last.
func memberAccountFor(t *testing.T, s web.State, used []string) string {
	t.Helper()
	vendor := map[string]string{}
	for _, a := range s.Accounts {
		vendor[a.Name] = a.Vendor
	}
	last := used[len(used)-1]
	other := ""
	for _, a := range s.Accounts {
		switch {
		case slices.Contains(used, a.Name):
		case a.Vendor == vendor[last]:
			return a.Name
		case other == "":
			other = a.Name
		}
	}
	if other == "" {
		return last
	}
	return other
}

// stateAlgorithms: the state names the algorithms, as core does, and a new
// group takes random.
func stateAlgorithms(t *testing.T, h Harness) {
	b := h.New(t)
	if len(h.Algorithms) == 0 {
		t.Fatal("the harness names no algorithms")
	}
	if got := b.State().Algorithms; fmt.Sprint(got) != fmt.Sprint(h.Algorithms) {
		t.Errorf("algorithms %q, want %q", got, h.Algorithms)
	}
	build(t, b, "[10 20]")
	lb := profileChoices(t, b.State(), gp)[0].LoadBalance
	if lb == nil || *lb != "random" || !slices.Contains(h.Algorithms, *lb) {
		t.Errorf("new group's load_balance %v, want random, one of %q", lb, h.Algorithms)
	}
}

// build makes profile gp as start writes it: each tag is a choice whose
// cap is [tag], on the first account; brackets are groups.
func build(t *testing.T, b web.Backend, start string) {
	t.Helper()
	tags, runs := parseShape(start)
	mustApply(t, b, "profile.add", map[string]any{"name": gp})
	for range tags[1:] {
		mustApply(t, b, "choice.add", map[string]any{"profile": gp})
	}
	for i, tag := range tags {
		n, _ := strconv.Atoi(tag)
		c := profileChoices(t, b.State(), gp)[i]
		mustApply(t, b, "choice.set", map[string]any{"profile": gp, "index": i, "expect": c,
			"field": "cap", "value": []any{n}})
	}
	for _, r := range runs {
		o := create(r...)(t, b.State())
		mustApply(t, b, o.op, o.args)
	}
	if got := shape(t, b.State()); got != start {
		t.Fatalf("built %s, want %s", got, start)
	}
}

// parseShape reads "10 [20 30] 40": the tags in order and each group's.
func parseShape(s string) (tags []string, runs [][]string) {
	var run []string
	in := false
	for _, f := range strings.Fields(s) {
		tag := strings.Trim(f, "[]")
		tags = append(tags, tag)
		if strings.HasPrefix(f, "[") {
			in, run = true, nil
		}
		if in {
			run = append(run, tag)
		}
		if strings.HasSuffix(f, "]") {
			in = false
			runs = append(runs, run)
		}
	}
	return tags, runs
}

// shape writes profile gp as its tags with each group in brackets, and
// fails t when the groups are not canonical: ids 1, 2, … in order, two or
// more members, load_balance on every member and on no plain choice.
func shape(t *testing.T, s web.State) string {
	t.Helper()
	cs := profileChoices(t, s, gp)
	var parts []string
	next := 1
	for i := 0; i < len(cs); {
		if cs[i].Group == 0 {
			if cs[i].LoadBalance != nil {
				t.Errorf("plain choice %d has load_balance %q", i+1, *cs[i].LoadBalance)
			}
			parts, i = append(parts, tag(cs[i])), i+1
			continue
		}
		j := i
		for j < len(cs) && cs[j].Group == cs[i].Group {
			if cs[j].LoadBalance == nil || *cs[j].LoadBalance != "random" {
				t.Errorf("member %d: load_balance %v, want random", j+1, cs[j].LoadBalance)
			}
			j++
		}
		if cs[i].Group != next || j-i < 2 {
			t.Errorf("group at choice %d: id %d with %d members, want id %d and 2 or more", i+1, cs[i].Group, j-i, next)
		}
		parts = append(parts, "["+strings.Join(names(cs[i:j], tag), " ")+"]")
		next, i = next+1, j
	}
	return strings.Join(parts, " ")
}

// tag names a choice by its first cap element; "_" when it has none.
func tag(c web.Choice) string {
	if len(c.Cap) == 0 {
		return "_"
	}
	return fmt.Sprint(c.Cap[0])
}

func profileChoices(t *testing.T, s web.State, name string) []web.Choice {
	t.Helper()
	for _, p := range s.Profiles {
		if p.Name == name {
			return p.Choices
		}
	}
	t.Fatalf("no profile %s", name)
	return nil
}

// find is the flat index and choice of the one choice tagged tg.
func find(t *testing.T, s web.State, tg string) (int, web.Choice) {
	t.Helper()
	for i, c := range profileChoices(t, s, gp) {
		if tag(c) == tg {
			return i, c
		}
	}
	t.Fatalf("no choice %s in %s", tg, shape(t, s))
	return 0, web.Choice{}
}

// groupOf is the group holding choice tg in the file's shape, as the page
// sends it in expect.
func groupOf(t *testing.T, s web.State, tg string) map[string]any {
	t.Helper()
	_, c := find(t, s, tg)
	if c.Group == 0 {
		t.Fatalf("choice %s is plain", tg)
	}
	var members []web.Choice
	for _, m := range profileChoices(t, s, gp) {
		if m.Group == c.Group {
			members = append(members, m)
		}
	}
	return map[string]any{"load_balance": *c.LoadBalance, "choices": members}
}
