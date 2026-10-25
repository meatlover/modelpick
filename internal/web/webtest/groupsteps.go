package webtest

import (
	"slices"
	"testing"

	"github.com/meatlover/modelpick/internal/web"
)

// The ops of the group scenarios, each built from the state as the page
// would build it: choices by flat index with the choice as expect, groups
// by any member's index with the whole group as expect.

func create(tags ...string) step {
	return func(t *testing.T, s web.State) opCall {
		indexes, expects := []int{}, []web.Choice{}
		for _, tg := range tags {
			i, c := find(t, s, tg)
			indexes, expects = append(indexes, i), append(expects, c)
		}
		return opCall{"group.create", map[string]any{"profile": gp, "indexes": indexes, "expects": expects}}
	}
}

func dissolve(tg string) step {
	return func(t *testing.T, s web.State) opCall {
		i, _ := find(t, s, tg)
		return opCall{"group.dissolve", map[string]any{"profile": gp, "index": i, "expect": groupOf(t, s, tg)}}
	}
}

// dissolveSeen dissolves the group of tg as the page saw it before tg's
// cap was changed by hand from [was].
func dissolveSeen(tg string, was int) step {
	return withArg(dissolve(tg), "expect", staleGroup(tg, was))
}

// arg is an op argument computed from the state, for withArg.
type arg func(t *testing.T, s web.State) any

// withArg sets one argument of what st builds: value, or what an arg
// computes.
func withArg(st step, key string, value any) step {
	return func(t *testing.T, s web.State) opCall {
		o := st(t, s)
		if f, ok := value.(arg); ok {
			o.args[key] = f(t, s)
		} else {
			o.args[key] = value
		}
		return o
	}
}

// staleGroup is the group of tg as the page saw it before tg's cap was
// changed by hand from [was].
func staleGroup(tg string, was int) arg {
	return func(t *testing.T, s web.State) any {
		g := groupOf(t, s, tg)
		members := append([]web.Choice{}, g["choices"].([]web.Choice)...)
		for k := range members {
			if tag(members[k]) == tg {
				members[k].Cap = []any{was}
			}
		}
		g["choices"] = members
		return g
	}
}

// reversedGroup is the group of tg with its members in reverse order.
func reversedGroup(tg string) arg {
	return func(t *testing.T, s web.State) any {
		g := groupOf(t, s, tg)
		members := append([]web.Choice{}, g["choices"].([]web.Choice)...)
		slices.Reverse(members)
		g["choices"] = members
		return g
	}
}

// seenAsGroup is a group of the plain choices tags, as a page that saw
// them grouped sends it.
func seenAsGroup(tags ...string) arg {
	return func(t *testing.T, s web.State) any {
		members := []web.Choice{}
		for _, tg := range tags {
			_, c := find(t, s, tg)
			members = append(members, asMember(c))
		}
		return map[string]any{"load_balance": "random", "choices": members}
	}
}

// seenChoices is the choices tags as the state gives them.
func seenChoices(tags ...string) arg {
	return func(t *testing.T, s web.State) any {
		out := []web.Choice{}
		for _, tg := range tags {
			_, c := find(t, s, tg)
			out = append(out, c)
		}
		return out
	}
}

// seenChoice is tg as a page holding view of it sends it.
func seenChoice(tg string, view seen) arg {
	return func(t *testing.T, s web.State) any {
		_, c := find(t, s, tg)
		return view(c)
	}
}

func groupMove(tg string, to int) step {
	return func(t *testing.T, s web.State) opCall {
		i, _ := find(t, s, tg)
		return opCall{"group.move", map[string]any{"profile": gp, "index": i, "expect": groupOf(t, s, tg), "to": to}}
	}
}

func plainAdd(*testing.T, web.State) opCall {
	return opCall{"choice.add", map[string]any{"profile": gp}}
}

// memberAdd adds a member to the group holding tg.
func memberAdd(tg string) step {
	return func(t *testing.T, s web.State) opCall {
		i, _ := find(t, s, tg)
		return opCall{"choice.add", map[string]any{"profile": gp, "group": i, "group_expect": groupOf(t, s, tg)}}
	}
}

// without drops one argument of what st builds.
func without(st step, key string) step {
	return func(t *testing.T, s web.State) opCall {
		o := st(t, s)
		delete(o.args, key)
		return o
	}
}

// withMemberOf adds member_of, the group holding tg as the file holds it,
// when tg is a member: the page sends it with every op on a member card.
func withMemberOf(t *testing.T, s web.State, tg string, args map[string]any) map[string]any {
	t.Helper()
	if _, c := find(t, s, tg); c.Group != 0 {
		args["member_of"] = groupOf(t, s, tg)
	}
	return args
}

func remove(tg string) step {
	return func(t *testing.T, s web.State) opCall {
		i, c := find(t, s, tg)
		return opCall{"choice.remove", withMemberOf(t, s, tg, map[string]any{"profile": gp, "index": i, "expect": c})}
	}
}

// move moves tg to to (an index into the list without it); join names a
// member of the group it joins, "" to end plain.
func move(tg string, to int, join string) step {
	return func(t *testing.T, s web.State) opCall {
		i, c := find(t, s, tg)
		args := withMemberOf(t, s, tg, map[string]any{"profile": gp, "from": i, "to": to, "expect": c, "group": nil})
		if join != "" {
			j, _ := find(t, s, join)
			args["group"], args["group_expect"] = j, groupOf(t, s, join)
		}
		return opCall{"choice.move", args}
	}
}

// seen turns a choice into what a stale page holds of it.
type seen func(web.Choice) web.Choice

func same(c web.Choice) web.Choice { return c }

func plain(c web.Choice) web.Choice {
	c.Group, c.LoadBalance = 0, nil
	return c
}

// otherAlgorithm is a member as a page that saw another algorithm holds it.
func otherAlgorithm(c web.Choice) web.Choice {
	lb := "weighted"
	c.LoadBalance = &lb
	return c
}

func asMember(c web.Choice) web.Choice {
	lb := "random"
	c.Group, c.LoadBalance = 1, &lb
	return c
}

// setSeen sets tg's cap to [n], addressed by index with the page's view
// of tg as expect, and member_of when the page saw tg as the member it is.
func setSeen(tg string, index int, view seen, n int) step {
	return func(t *testing.T, s web.State) opCall {
		_, c := find(t, s, tg)
		args := map[string]any{"profile": gp, "index": index, "expect": view(c), "field": "cap", "value": []any{n}}
		if view(c).Group != 0 {
			args = withMemberOf(t, s, tg, args)
		}
		return opCall{"choice.set", args}
	}
}

// groupWith is the group holding of as the page saw it before a hand edit
// or another tab took tg out of it: tg, as a member, at place k.
func groupWith(of, tg string, k int) arg {
	return func(t *testing.T, s web.State) any {
		g := groupOf(t, s, of)
		_, c := find(t, s, tg)
		members := g["choices"].([]web.Choice)
		g["choices"] = slices.Insert(append([]web.Choice{}, members...), k, asMember(c))
		return g
	}
}

// moveSeen moves tg as a stale page would: from its old index, as plain.
func moveSeen(tg string, from, to int, view seen) step {
	return func(t *testing.T, s web.State) opCall {
		_, c := find(t, s, tg)
		return opCall{"choice.move", map[string]any{"profile": gp, "from": from, "to": to, "expect": view(c)}}
	}
}

func setCap(tg string, n int) step { return setField(tg, "cap", []any{n}) }

func setField(tg, field string, value any) step {
	return func(t *testing.T, s web.State) opCall {
		i, c := find(t, s, tg)
		args := map[string]any{"profile": gp, "index": i, "expect": c, "field": field, "value": value}
		return opCall{"choice.set", withMemberOf(t, s, tg, args)}
	}
}

// otherAccount is the last account by name, which the fixture must have
// besides the first, with listable models.
func otherAccount(t *testing.T, s web.State) string {
	t.Helper()
	if len(s.Accounts) < 2 {
		t.Fatal("the fixture needs two accounts")
	}
	return s.Accounts[len(s.Accounts)-1].Name
}

// toOther moves tg to otherAccount.
func toOther(tg string) step {
	return func(t *testing.T, s web.State) opCall {
		i, c := find(t, s, tg)
		return opCall{"choice.set", map[string]any{"profile": gp, "index": i, "expect": c,
			"field": "account", "value": otherAccount(t, s)}}
	}
}

func removeOther(t *testing.T, s web.State) opCall {
	return opCall{"account.remove", map[string]any{"name": otherAccount(t, s), "with_choices": true}}
}
