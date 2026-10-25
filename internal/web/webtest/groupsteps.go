package webtest

import (
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
	return func(t *testing.T, s web.State) opCall {
		i, _ := find(t, s, tg)
		g := groupOf(t, s, tg)
		members := append([]web.Choice{}, g["choices"].([]web.Choice)...)
		for k := range members {
			if tag(members[k]) == tg {
				members[k].Cap = []any{was}
			}
		}
		g["choices"] = members
		return opCall{"group.dissolve", map[string]any{"profile": gp, "index": i, "expect": g}}
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

func remove(tg string) step {
	return func(t *testing.T, s web.State) opCall {
		i, c := find(t, s, tg)
		return opCall{"choice.remove", map[string]any{"profile": gp, "index": i, "expect": c}}
	}
}

// move moves tg to to (an index into the list without it); join names a
// member of the group it joins, "" to end plain.
func move(tg string, to int, join string) step {
	return func(t *testing.T, s web.State) opCall {
		i, c := find(t, s, tg)
		args := map[string]any{"profile": gp, "from": i, "to": to, "expect": c, "group": nil}
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

func asMember(c web.Choice) web.Choice {
	lb := "random"
	c.Group, c.LoadBalance = 1, &lb
	return c
}

// setSeen sets tg's cap to [n], addressed by index with the page's view
// of tg as expect.
func setSeen(tg string, index int, view seen, n int) step {
	return func(t *testing.T, s web.State) opCall {
		_, c := find(t, s, tg)
		return opCall{"choice.set", map[string]any{"profile": gp, "index": index, "expect": view(c),
			"field": "cap", "value": []any{n}}}
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
		return opCall{"choice.set", map[string]any{"profile": gp, "index": i, "expect": c, "field": field, "value": value}}
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
