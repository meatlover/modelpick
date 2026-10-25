package app_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// Group fixtures: c3 is a fourth plain choice, c0b a second claude-main one.
// grp (doctor_test.go) writes a group item, which is also the shape of a
// group's expect (spec §8).
const (
	c3  = `{"account":"claude-two","model":"claude-haiku-5","thinking":null,"cap":[]}`
	e3  = `{"account":"claude-two","model":"claude-haiku-5","thinking":null,"cap":[],"skip_during":[],"allow_during":[]}`
	c0b = `{"account":"claude-main","model":"claude-sonnet-5","thinking":"high","cap":[]}`
)

// member is a member's expect as the page sends it: the choice plus its
// group id and algorithm, which identity ignores.
func member(expect string, group int) string {
	return strings.TrimSuffix(expect, "}") + `,"group":` + itoa(group) + `,"load_balance":"random"}`
}

// gmove is choice.move on coder that ends plain (group null).
func gmove(from, to int, expect string) string {
	return `{"profile":"coder","from":` + itoa(from) + `,"to":` + itoa(to) + `,"expect":` + expect + `,"group":null}`
}

// gjoin is choice.move on coder into the group at flat index group, which
// the page saw as groupExpect.
func gjoin(from, to int, expect string, group int, groupExpect string) string {
	return `{"profile":"coder","from":` + itoa(from) + `,"to":` + itoa(to) + `,"expect":` + expect +
		`,"group":` + itoa(group) + `,"group_expect":` + groupExpect + `}`
}

// addMember is choice.add on coder into the group at flat index group.
func addMember(group int, groupExpect string) string {
	return `{"profile":"coder","group":` + itoa(group) + `,"group_expect":` + groupExpect + `}`
}

func create(indexes string, expects ...string) string {
	return `{"profile":"coder","indexes":` + indexes + `,"expects":[` + strings.Join(expects, ",") + `]}`
}

func groupMove(index, to int, expect string) string {
	return `{"profile":"coder","index":` + itoa(index) + `,"to":` + itoa(to) + `,"expect":` + expect + `}`
}

var groupCreateCases = []opCase{
	{name: "adjacent", doc: base, op: "group.create", args: create(`[0,1]`, e0, e1),
		want: base.prof("coder", grp(c0, c1), c2)},
	{name: "non-adjacent pulled up", doc: base, op: "group.create", args: create(`[0,2]`, e0, e2),
		want: base.prof("coder", grp(c0, c2), c1)},
	{name: "file order, not selection order", doc: base, op: "group.create", args: create(`[2,1]`, e2, e1),
		want: base.prof("coder", c0, grp(c1, c2))},
	{name: "beside a group", doc: base.prof("coder", grp(c0, c1), c2, c3), op: "group.create",
		args: create(`[2,3]`, e2, e3), want: base.prof("coder", grp(c0, c1), grp(c2, c3))},
	{name: "one selected", doc: base, op: "group.create", args: create(`[0]`, e0),
		refused: "Load balancing needs at least 2 choices"},
	{name: "selected twice", doc: base, op: "group.create", args: create(`[1,1]`, e1, e1),
		refused: "Choice 2 of coder is selected twice"},
	{name: "expects do not match indexes", doc: base, op: "group.create", args: create(`[0,1]`, e0),
		refused: "Bad arguments for group.create"},
	{name: "already in a group", doc: base.prof("coder", grp(c0, c1), c2), op: "group.create",
		args: create(`[1,2]`, member(e1, 1), e2), refused: "Choice 1b of coder is already in a load balancing group"},
	{name: "stale", doc: base, op: "group.create", args: create(`[0,1]`, e0, es),
		refused: "Choice 2 of coder was changed outside the UI"},
}

func TestOpGroupCreate(t *testing.T) { runOps(t, groupCreateCases) }

var groupDissolveCases = []opCase{
	{name: "dissolve", doc: base.prof("coder", grp(c0, c1), c2), op: "group.dissolve",
		args: at("coder", 1, grp(e0, e1)), want: base.prof("coder", c0, c1, c2)},
	{name: "dissolve renumbers the next group", doc: base.prof("coder", grp(c0, c1), grp(c2, c3)),
		op: "group.dissolve", args: at("coder", 0, grp(e0, e1)), want: base.prof("coder", c0, c1, grp(c2, c3))},
	{name: "dissolve follows the one identical group", doc: base.prof("coder", c2, grp(c0, c1)),
		op: "group.dissolve", args: at("coder", 0, grp(e0, e1)), want: base.prof("coder", c2, c0, c1)},
	{name: "dissolve stale", doc: base.prof("coder", grp(c0, c1), c2), op: "group.dissolve",
		args: at("coder", 0, grp(e0, e2)), refused: "Load balancing group 1 of coder was changed outside the UI"},
	{name: "dissolve out of range", doc: base.prof("coder", grp(c0, c1), c2), op: "group.dissolve",
		args: at("coder", 5, grp(e0, e2)), refused: "Profile coder has no choice 6"},
	{name: "group.move to the front", doc: base.prof("coder", c3, grp(c0, c1), c2), op: "group.move",
		args: groupMove(2, 0, grp(e0, e1)), want: base.prof("coder", grp(c0, c1), c3, c2)},
	{name: "group.move to the end", doc: base.prof("coder", c3, grp(c0, c1), c2), op: "group.move",
		args: groupMove(1, 2, grp(e0, e1)), want: base.prof("coder", c3, c2, grp(c0, c1))},
	{name: "group.move past a group renumbers", doc: base.prof("coder", grp(c0, c1), grp(c2, c3)),
		op: "group.move", args: groupMove(0, 1, grp(e0, e1)), want: base.prof("coder", grp(c2, c3), grp(c0, c1))},
	{name: "group.move no position", doc: base.prof("coder", c3, grp(c0, c1), c2), op: "group.move",
		args: groupMove(1, 3, grp(e0, e1)), refused: "Profile coder has no position 4"},
	{name: "group.move stale", doc: base.prof("coder", c3, grp(c0, c1), c2), op: "group.move",
		args: groupMove(1, 0, grp(e1, e0)), refused: "Load balancing group 2 of coder was changed outside the UI"},
}

func TestOpGroupDissolve(t *testing.T) { runOps(t, groupDissolveCases) }

var groupMoveCases = []opCase{
	{name: "in", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.move", args: gjoin(2, 1, e2, 0, grp(e0, e1)),
		want: base.prof("coder", grp(c0, c2, c1))},
	{name: "in at the end edge", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.move",
		args: gjoin(2, 2, e2, 1, grp(e0, e1)), want: base.prof("coder", grp(c0, c1, c2))},
	{name: "in at the front edge", doc: base.prof("coder", c2, grp(c0, c1)), op: "choice.move",
		args: gjoin(0, 0, e2, 1, grp(e0, e1)), want: base.prof("coder", grp(c2, c0, c1))},
	{name: "in lands outside the group", doc: base.prof("coder", grp(c0, c1), c2, c3), op: "choice.move",
		args: gjoin(3, 3, e3, 0, grp(e0, e1)), refused: "Choice 3 of coder must be placed inside the load balancing group it joins"},
	{name: "group expect matches no group", doc: base.prof("coder", grp(c0, c1), c2, c3), op: "choice.move",
		args: gjoin(3, 2, e3, 2, grp(e2, e3)), refused: "Load balancing group 2 of coder was changed outside the UI"},
	{name: "group without group_expect", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.move",
		args: `{"profile":"coder","from":2,"to":1,"expect":` + e2 + `,"group":0}`, refused: "Bad arguments for choice.move"},
	// The page saw [c2, group(c0, c1), group(c3, c0b)] and drops c2 into the
	// first group; a hand edit swapped the two groups. group_expect follows
	// the group the user chose, and the drop is not inside it.
	{name: "groups swapped by hand", doc: base.prof("coder", c2, grp(c3, c0b), grp(c0, c1)), op: "choice.move",
		args: gjoin(0, 1, e2, 1, grp(e0, e1)), refused: "Choice 1 of coder must be placed inside the load balancing group it joins"},
	{name: "group changed by hand", doc: base.prof("coder", c2, grp(c0, c3)), op: "choice.move",
		args: gjoin(0, 0, e2, 1, grp(e0, e1)), refused: "Load balancing group 2 of coder was changed outside the UI"},
	// The page saw [c0, c1, c2], all plain; a hand edit grouped them. A
	// stale plain expect must not pull a member out, by fallback or by a
	// direct hit at index (reviewer, Task 4 round 1).
	{name: "stale plain expect, fallback finds a member", doc: base.prof("coder", grp(c0, c2, c1)), op: "choice.move",
		args: gmove(1, 0, e1), refused: "Choice 1b of coder was changed outside the UI"},
	{name: "stale plain expect, direct hit on a member", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.move",
		args: gmove(1, 2, e1), refused: "Choice 1b of coder was changed outside the UI"},
	{name: "group out of range", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.move",
		args: gjoin(2, 1, e2, 9, grp(e0, e2)), refused: "Profile coder has no choice 10"},
	{name: "out", doc: base.prof("coder", grp(c0, c1, c2), c3), op: "choice.move",
		args: gmove(0, 3, member(e0, 1)), want: base.prof("coder", grp(c1, c2), c3, c0)},
	{name: "out at the front edge", doc: base.prof("coder", grp(c0, c1, c2), c3), op: "choice.move",
		args: gmove(1, 0, member(e1, 1)), want: base.prof("coder", c1, grp(c0, c2), c3)},
	{name: "out leaving one dissolves", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.move",
		args: gmove(0, 2, member(e0, 1)), want: base.prof("coder", c1, c2, c0)},
	{name: "plain cannot land inside a group", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.move",
		args: gmove(2, 1, e2), refused: "Choice 2 of coder cannot be placed inside a load balancing group"},
	{name: "member cannot stay inside without group", doc: base.prof("coder", grp(c0, c1, c2)), op: "choice.move",
		args: gmove(1, 1, member(e1, 1)), refused: "Choice 1b of coder cannot be placed inside a load balancing group"},
	{name: "plain past a group", doc: base.prof("coder", c2, grp(c0, c1)), op: "choice.move",
		args: gmove(0, 2, e2), want: base.prof("coder", grp(c0, c1), c2)},
	{name: "between two groups", doc: base.prof("coder", grp(c0, c1), grp(c2, c3)), op: "choice.move",
		args: gmove(0, 1, member(e0, 1)), want: base.prof("coder", c1, c0, grp(c2, c3))},
	{name: "within a group", doc: base.prof("coder", grp(c0, c1, c2)), op: "choice.move",
		args: gjoin(0, 2, member(e0, 1), 0, grp(e0, e1, e2)), want: base.prof("coder", grp(c1, c2, c0))},
	{name: "member to another group", doc: base.prof("coder", grp(c0, c1), grp(c2, c3)), op: "choice.move",
		args: gjoin(0, 2, member(e0, 1), 2, grp(e2, e3)), want: base.prof("coder", c1, grp(c2, c0, c3))},
}

func TestOpGroupMove(t *testing.T) { runOps(t, groupMoveCases) }

var groupMemberCases = []opCase{
	{name: "remove a member", doc: base.prof("coder", grp(c0, c1, c2), c3), op: "choice.remove",
		args: at("coder", 1, member(e1, 1)), want: base.prof("coder", grp(c0, c2), c3)},
	{name: "remove down to one dissolves", doc: base.prof("coder", c3, grp(c0, c1)), op: "choice.remove",
		args: at("coder", 1, member(e0, 1)), want: base.prof("coder", c3, c1)},
	{name: "remove renumbers", doc: base.prof("coder", grp(c0, c1), grp(c2, c3)), op: "choice.remove",
		args: at("coder", 0, member(e0, 1)), want: base.prof("coder", c1, grp(c2, c3))},
	{name: "member expect with another algorithm", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.remove",
		args:    at("coder", 0, strings.Replace(member(e0, 1), "random", "weighted", 1)),
		refused: "Choice 1a of coder was changed outside the UI"},
	{name: "stale names the member", doc: base.prof("coder", grp(c0, c1), c2), op: "choice.remove",
		args: at("coder", 1, es), refused: "Choice 1b of coder was changed outside the UI"},
	// The file was [c0, c1, c2]; a hand edit made it [group(c0, c2), c1].
	// The page still removes choice 3 (c2): c2 is now in a group, so the
	// fallback must not reach it.
	{name: "stale does not cross into a group", doc: base.prof("coder", grp(c0, c2), c1), op: "choice.remove",
		args: at("coder", 2, e2), refused: "Choice 2 of coder was changed outside the UI"},
	{name: "stale does not cross out of a group", doc: base.prof("coder", c1, grp(c0, c2)), op: "choice.remove",
		args: at("coder", 1, member(e1, 1)), refused: "Choice 2a of coder was changed outside the UI"},
	{name: "stale follows inside the same group", doc: base.prof("coder", grp(c0, c1, c2)), op: "choice.remove",
		args: at("coder", 0, member(e1, 1)), want: base.prof("coder", grp(c0, c2))},
	{name: "set names the member", doc: base.prof("coder", grp(c0, c1)), op: "choice.set",
		args:    set("coder", 1, member(e1, 1), "thinking", `"max"`),
		refused: `Profile coder, choice 1b: thinking "max" is not offered by gpt-6.1-sol`},
	{name: "add after a trailing group", doc: base.prof("coder", c0, grp(c1, c2)), op: "choice.add",
		args: `{"profile":"coder"}`, want: base.prof("coder", c0, grp(c1, c2),
			`{"account":"go-1","model":"opencode-go/deepseek-v4.1-flash","thinking":null,"cap":[]}`)},
	{name: "add member: unused account of the last member's vendor", doc: base.prof("coder", grp(c1, c0), c2),
		op: "choice.add", args: addMember(0, grp(e1, e0)),
		want: base.prof("coder", grp(c1, c0, `{"account":"claude-two","model":"claude-haiku-5","thinking":null,"cap":[]}`), c2)},
	{name: "add member: the vendor comes before the name order", doc: withGo2.prof("coder", grp(c0, c2)),
		op: "choice.add", args: addMember(0, grp(e0, e2)),
		want: withGo2.prof("coder", grp(c0, c2, `{"account":"go-2","model":"opencode-go/kimi-k3","thinking":"max","cap":[]}`))},
	{name: "add member: vendor used up, first unused account", doc: base.prof("coder", c2, grp(c0, c1)),
		op: "choice.add", args: addMember(2, grp(e0, e1)),
		want: base.prof("coder", c2, grp(c0, c1, `{"account":"claude-two","model":"claude-haiku-5","thinking":null,"cap":[]}`))},
	{name: "add member: every account used, copies the last", doc: twoAccounts, op: "choice.add",
		args: addMember(1, grp(e0, e1)),
		want: twoAccounts.prof("coder", grp(c0, c1, `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"low","cap":[]}`))},
	{name: "add member: models not listed", doc: base.prof("coder", grp(c1, c2)), op: "choice.add",
		args: addMember(0, grp(e1, e2)), refused: "Cannot list the models of go-9: opencode not found on PATH"},
	{name: "add member: group expect matches no group", doc: base, op: "choice.add", args: addMember(1, grp(e0, e1)),
		refused: "Load balancing group 2 of coder was changed outside the UI"},
	{name: "add member: without group_expect", doc: base.prof("coder", grp(c0, c1)), op: "choice.add",
		args: `{"profile":"coder","group":0}`, refused: "Bad arguments for choice.add"},
	// The page saw [group(c0, c1), group(c2, c3)] and adds to the first; a
	// hand edit swapped them. The member goes to the group the user chose.
	{name: "add member: groups swapped by hand", doc: base.prof("coder", grp(c2, c3), grp(c0, c1)), op: "choice.add",
		args: addMember(0, grp(e0, e1)),
		want: base.prof("coder", grp(c2, c3), grp(c0, c1, `{"account":"claude-two","model":"claude-haiku-5","thinking":null,"cap":[]}`))},
	{name: "add member: out of range", doc: base, op: "choice.add", args: addMember(3, grp(e0, e1)),
		refused: "Profile coder has no choice 4"},
}

// withGo2 has a second listable opencode-go account, named after claude-two.
var withGo2 = base.acct("go-2", `{"vendor":"opencode-go","key_file":"~/.ssh/opencode-go-api","key_line":2}`)

// twoAccounts has only the accounts its one group uses.
var twoAccounts = fx{
	accounts:  map[string]string{"claude-main": base.accounts["claude-main"], "codex-main": base.accounts["codex-main"]},
	schedules: base.schedules,
	profiles:  map[string][]string{"coder": {grp(c0, c1)}},
}

func TestOpGroupMembers(t *testing.T) { runOps(t, groupMemberCases) }

var groupAccountCases = []opCase{
	{name: "with_choices leaves one member", doc: base.prof("coder", grp(c0, c1), c2), op: "account.remove",
		args: `{"name":"claude-main","with_choices":true}`, want: base.prof("coder", c1, c2).acct("claude-main", "")},
	{name: "with_choices leaves no member", doc: base.prof("coder", c2, grp(c0, c0b), c1), op: "account.remove",
		args: `{"name":"claude-main","with_choices":true}`, want: base.prof("coder", c2, c1).acct("claude-main", "")},
	{name: "with_choices renumbers", doc: base.prof("coder", grp(c0, c1), grp(c2, c3)), op: "account.remove",
		args: `{"name":"codex-main","with_choices":true}`,
		want: base.prof("coder", c0, grp(c2, c3)).prof("solo").acct("codex-main", "")},
	{name: "used by a member", doc: base.prof("coder", grp(c0, c1), c2), op: "account.remove",
		args: `{"name":"codex-main"}`, refused: "Account codex-main is used by profile coder, choice 1b"},
	{name: "used after a group", doc: base.prof("coder", grp(c0, c1), c2), op: "account.remove",
		args: `{"name":"go-1"}`, refused: "Account go-1 is used by profile coder, choice 2"},
}

func TestOpGroupAccount(t *testing.T) { runOps(t, groupAccountCases) }

// TestEditorWritesGroups drives the ops through the editor (encode, re-parse,
// doctor gate, write): the file gains a group, then loses it when a removal
// leaves one member.
func TestEditorWritesGroups(t *testing.T) {
	codex := `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"high"}`
	f := newEditorFx(t, editorConfig(goodFirst))
	s, _, err := f.ed.Apply(f.load(t).SHA256, "group.create",
		json.RawMessage(`{"profile":"p","indexes":[0,1],"expects":[`+goodFirst+`,`+codex+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustDoc(t, string(f.bytes(t))).Profiles["p"]; len(got) != 2 || got[0].Group != 1 || got[1].LoadBalance != "random" {
		t.Fatalf("written profile p = %+v, want one random group of 2", got)
	}
	if _, _, err := f.ed.Apply(s.SHA256, "choice.remove",
		json.RawMessage(`{"profile":"p","index":1,"expect":`+member(codex, 1)+`}`)); err != nil {
		t.Fatal(err)
	}
	if text := string(f.bytes(t)); strings.Contains(text, "load_balance") {
		t.Fatalf("a one-member group was written:\n%s", text)
	}
}
