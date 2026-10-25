package core

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
)

// groupPickConfig is the example's accounts plus go-3, with the given profiles.
func groupPickConfig(t *testing.T, profiles string) *Config {
	t.Helper()
	data := `{"version": 1,
  "accounts": {
    "claude-main": {"vendor": "claude", "config_dir": "~/.claude"},
    "go-1": {"vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 1},
    "go-2": {"vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 2},
    "go-3": {"vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 3}
  },
  "schedules": {"deepseek-peak": {"days": ["mon", "tue", "wed", "thu", "fri"], "utc": ["01:00-04:00", "06:00-10:00"]}},
  "profiles": ` + profiles + `}`
	cfg, err := Parse([]byte(data), "group.json", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// goGroupProfile is profile p: a group of the given go accounts (as in the
// example's tester choices) under algorithm alg, then claude-main uncapped.
func goGroupProfile(alg string, accounts ...string) string {
	members := make([]string, len(accounts))
	for i, a := range accounts {
		members[i] = `{"account": "` + a + `", "model": "opencode-go/deepseek-v4.1", "thinking": null,
			"cap": [95, 95, 98], "skip_during": ["deepseek-peak"]}`
	}
	return `{"p": [
		{"load_balance": "` + alg + `", "choices": [` + strings.Join(members, ",") + `]},
		{"account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`
}

// goReading is an OpenCode Go reading taken at noon.
func goReading(rolling, monthly float64) *adapter.Reading {
	return reading(noon, win("rolling", 5*hour, rolling), win("weekly", 7*day, 40), win("monthly", 30*day, monthly))
}

// noShuffle keeps the members in file order.
func noShuffle(int, func(i, j int)) {}

// pickedAccount is r's account, or "<none>".
func pickedAccount(r Result) string {
	if r.Account == nil {
		return "<none>"
	}
	return *r.Account
}

func usableGo(accounts ...string) *fakeEnv {
	env := &fakeEnv{usage: map[string]AccountState{}}
	for _, a := range accounts {
		env.usage[a] = AccountState{Reading: goReading(20, 50)}
	}
	return env
}

func TestPickGroupPicksShuffledMember(t *testing.T) {
	cfg := groupPickConfig(t, goGroupProfile("random", "go-1", "go-2", "go-3"))
	cases := []struct {
		name    string
		shuffle func(int, func(i, j int))
		account string
		member  string
	}{
		{"reversed", reverse, "go-3", "1c"},
		{"file order", noShuffle, "go-1", "1a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Pick(cfg, "p", noon, usableGo("go-1", "go-2", "go-3"), tc.shuffle)
			if r.Status != StatusOK || pickedAccount(r) != tc.account {
				t.Fatalf("status %s account %v, want ok %s", r.Status, pickedAccount(r), tc.account)
			}
			want := &LBPick{Algorithm: "random", Choice: 1, Member: tc.member}
			if !reflect.DeepEqual(r.LoadBalance, want) {
				t.Errorf("LoadBalance = %+v, want %+v", r.LoadBalance, want)
			}
			if len(r.Skipped) != 0 {
				t.Errorf("skipped %v, want none: the members not reached were not passed over", r.Skipped)
			}
		})
	}
}

func TestPickGroupFallsThrough(t *testing.T) {
	cfg := groupPickConfig(t, goGroupProfile("random", "go-1", "go-2", "go-3"))
	env := &fakeEnv{
		creds: map[string]adapter.CredStatus{"go-1": {Source: "file: go-1"}},
		usage: map[string]AccountState{
			"go-2": {Reading: goReading(20, 99.5)},
			"go-3": {Reading: goReading(96, 50)},
		},
	}
	r := Pick(cfg, "p", noon, env, reverse)
	if r.Status != StatusOKWithErrors || pickedAccount(r) != "claude-main" {
		t.Fatalf("status %s account %v, want ok_with_errors claude-main", r.Status, pickedAccount(r))
	}
	if r.LoadBalance != nil {
		t.Errorf("LoadBalance = %+v, want nil for a plain choice", r.LoadBalance)
	}
	// The local check runs on every member first, in file order; then the
	// shuffled walk (go-3, go-2) skips each with its own why.
	const m = "opencode-go/deepseek-v4.1"
	want := []Skip{
		{Account: "go-1", Model: m, Why: "credential missing (see errors)"},
		{Account: "go-3", Model: m, Why: "5h window 96% >= cap 95"},
		{Account: "go-2", Model: m, Why: "30d window 99.5% >= cap 98"},
	}
	if !reflect.DeepEqual(r.Skipped, want) {
		t.Errorf("skipped\n%v\nwant\n%v", r.Skipped, want)
	}
	wantErrs := []RunError{{Account: "go-1", Error: "file: go-1: not found"}}
	if !reflect.DeepEqual(r.Errors, wantErrs) {
		t.Errorf("errors %v, want %v", r.Errors, wantErrs)
	}
}

func TestPickGroupLazy(t *testing.T) {
	cfg := groupPickConfig(t, goGroupProfile("random", "go-1", "go-2", "go-3"))
	env := usableGo("go-1", "go-2", "go-3")
	r := Pick(cfg, "p", noon, env, reverse)
	if pickedAccount(r) != "go-3" {
		t.Fatalf("picked %s, want go-3 (first after reversing)", pickedAccount(r))
	}
	if n := env.totalCalls(); n != 1 || env.called["go-3"] != 1 {
		t.Errorf("Usage calls %v, want go-3 once and nothing else", env.called)
	}
}

func TestPickGroupCredentialStatusDeterministic(t *testing.T) {
	cfg := groupPickConfig(t, goGroupProfile("random", "go-1", "go-2", "go-3"))
	picked := map[string]int{}
	for seed := range uint64(20) {
		env := usableGo("go-1", "go-3")
		env.creds = map[string]adapter.CredStatus{"go-2": {Source: "file: go-2"}}
		r := Pick(cfg, "p", noon, env, rand.New(rand.NewPCG(seed, seed)).Shuffle)
		wantErrs := []RunError{{Account: "go-2", Error: "file: go-2: not found"}}
		if r.Status != StatusOKWithErrors || !reflect.DeepEqual(r.Errors, wantErrs) {
			t.Fatalf("seed %d: status %s errors %v, want ok_with_errors %v", seed, r.Status, r.Errors, wantErrs)
		}
		picked[pickedAccount(r)]++
	}
	// Both orders occurred, so the broken member was both before and after
	// the picked one in the shuffle.
	if picked["go-1"] == 0 || picked["go-3"] == 0 {
		t.Errorf("picked %v over 20 seeds, want both go-1 and go-3", picked)
	}
}

func TestPickPlainHasNullLoadBalance(t *testing.T) {
	env := &fakeEnv{usage: map[string]AccountState{"claude-main": {Reading: claudeReading(noon, 40, 50)}}}
	results := map[string]Result{
		"picked": Pick(exampleConfig(t), "coder-difficult", noon, env, nil),
		"failed": Pick(exampleConfig(t), "coder-hard", noon, &fakeEnv{}, nil),
	}
	for name, r := range results {
		if r.LoadBalance != nil {
			t.Errorf("%s: LoadBalance = %+v, want nil", name, r.LoadBalance)
		}
		if !strings.Contains(string(r.JSON()), "\n  \"account_ref\": ") ||
			!strings.Contains(string(r.JSON()), "\n  \"load_balance\": null,\n  \"reason\": ") {
			t.Errorf("%s: JSON lacks \"load_balance\": null after account_ref:\n%s", name, r.JSON())
		}
	}
}

// lastAlgorithm picks the last usable member, ignoring the shuffle, and
// records the accounts of the members it was given.
type lastAlgorithm struct{ given *[]string }

func (a lastAlgorithm) Pick(members []Choice, usable func(int) bool, _ func(int, func(int, int))) (int, bool) {
	*a.given = nil
	for _, m := range members {
		*a.given = append(*a.given, m.Account)
	}
	for i := len(members) - 1; i >= 0; i-- {
		if usable(i) {
			return i, true
		}
	}
	return 0, false
}

// TestPickUsesAlgorithmTable shows the table is pick's only source of
// algorithms (spec §10), and that an algorithm is given the members that
// passed the local checks (spec §5). It mutates Algorithms, so no t.Parallel.
func TestPickUsesAlgorithmTable(t *testing.T) {
	var given []string
	Algorithms["test-last"] = lastAlgorithm{&given}
	defer delete(Algorithms, "test-last")
	cfg := groupPickConfig(t, goGroupProfile("test-last", "go-1", "go-2", "go-3"))
	// With the members in file order, random would pick go-1.
	r := Pick(cfg, "p", noon, usableGo("go-1", "go-2", "go-3"), noShuffle)
	if pickedAccount(r) != "go-3" {
		t.Fatalf("picked %s, want go-3 (the fake algorithm's choice)", pickedAccount(r))
	}
	want := &LBPick{Algorithm: "test-last", Choice: 1, Member: "1c"}
	if !reflect.DeepEqual(r.LoadBalance, want) {
		t.Errorf("LoadBalance = %+v, want %+v", r.LoadBalance, want)
	}
	if !reflect.DeepEqual(given, []string{"go-1", "go-2", "go-3"}) {
		t.Errorf("algorithm was given %q, want all three members", given)
	}
	// go-3 has no credential: it fails the local checks, so the algorithm
	// is given go-1 and go-2 only, and its index 1 is member 1b.
	env := usableGo("go-1", "go-2", "go-3")
	env.creds = map[string]adapter.CredStatus{"go-3": {Source: "test: go-3"}}
	r = Pick(cfg, "p", noon, env, noShuffle)
	if !reflect.DeepEqual(given, []string{"go-1", "go-2"}) {
		t.Errorf("algorithm was given %q, want the survivors go-1, go-2", given)
	}
	want = &LBPick{Algorithm: "test-last", Choice: 1, Member: "1b"}
	if pickedAccount(r) != "go-2" || !reflect.DeepEqual(r.LoadBalance, want) {
		t.Errorf("picked %s %+v, want go-2 %+v", pickedAccount(r), r.LoadBalance, want)
	}
}

// TestPickGroupPositionNumber: choice is the group's position, not its flat
// index (2 here, flat index 2 would read 3), and member labels count within
// the group.
func TestPickGroupPositionNumber(t *testing.T) {
	cfg := groupPickConfig(t, `{"p": [
		{"load_balance": "random", "choices": [
			{"account": "go-1", "model": "m", "thinking": null, "cap": [10]},
			{"account": "go-1", "model": "m2", "thinking": null, "cap": [10]}]},
		{"load_balance": "random", "choices": [
			{"account": "go-2", "model": "m", "thinking": null},
			{"account": "go-3", "model": "m", "thinking": null}]}]}`)
	env := &fakeEnv{usage: map[string]AccountState{"go-1": {Reading: goReading(50, 50)}}}
	r := Pick(cfg, "p", noon, env, reverse)
	want := &LBPick{Algorithm: "random", Choice: 2, Member: "2b"}
	if pickedAccount(r) != "go-3" || !reflect.DeepEqual(r.LoadBalance, want) {
		t.Fatalf("account %s LoadBalance %+v, want go-3 %+v", pickedAccount(r), r.LoadBalance, want)
	}
	if len(r.Skipped) != 2 {
		t.Errorf("skipped %v, want both members of the first group", r.Skipped)
	}
}
