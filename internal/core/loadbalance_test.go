package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestLabels(t *testing.T) {
	got := Labels([]int{0, 1, 1, 0, 2, 2})
	if want := []string{"1", "2a", "2b", "3", "4a", "4b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Labels = %q, want %q", got, want)
	}
	// Adjacent groups are separate positions; letters run past z.
	groups := []int{1, 1, 2}
	for len(groups) < 30 {
		groups = append(groups, 3)
	}
	got = Labels(groups)
	if len(got) != 30 {
		t.Fatalf("Labels returned %d labels for 30 choices", len(got))
	}
	if got[1] != "1b" || got[2] != "2a" || got[28] != "3z" || got[29] != "3aa" {
		t.Errorf("Labels = %q", got)
	}
}

func TestPositions(t *testing.T) {
	got := Positions([]int{0, 1, 1, 2, 2, 2, 0}, []string{"", "random", "random", "x", "x", "x", ""})
	want := []Position{{0, 1, ""}, {1, 3, "random"}, {3, 6, "x"}, {6, 7, ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Positions = %+v, want %+v", got, want)
	}
}

// reverse is a fixed shuffle: it reverses the order.
func reverse(n int, swap func(i, j int)) {
	for i := 0; i < n/2; i++ {
		swap(i, n-1-i)
	}
}

func TestRandomAlgorithm(t *testing.T) {
	alg := Algorithms["random"]
	var asked []int
	usable := func(ok ...int) func(int) bool {
		asked = nil
		return func(i int) bool {
			asked = append(asked, i)
			for _, o := range ok {
				if o == i {
					return true
				}
			}
			return false
		}
	}
	// Shuffled order is 3 2 1 0: 3 is unusable, 2 is the first usable.
	i, ok := alg.Pick(make([]Choice, 4), usable(0, 2), reverse)
	if !ok || i != 2 {
		t.Errorf("Pick = %d, %v; want 2, true", i, ok)
	}
	if !reflect.DeepEqual(asked, []int{3, 2}) {
		t.Errorf("usable asked %v, want [3 2] (lazy, in shuffled order)", asked)
	}
	if i, ok := alg.Pick(make([]Choice, 3), usable(), reverse); ok || i != 0 {
		t.Errorf("none usable: Pick = %d, %v; want 0, false", i, ok)
	}
	if !reflect.DeepEqual(asked, []int{2, 1, 0}) {
		t.Errorf("none usable: asked %v, want all three", asked)
	}
}

type firstAlgorithm struct{}

func (firstAlgorithm) Pick(members []Choice, usable func(int) bool, _ func(int, func(int, int))) (int, bool) {
	for i := range members {
		if usable(i) {
			return i, true
		}
	}
	return 0, false
}

func TestAlgorithmsTableIsTheOnlyList(t *testing.T) {
	src := groupConfig(`"load_balance":"test-only"`)
	if _, err := Parse([]byte(src), "c.json", "/h"); err == nil || !strings.Contains(err.Error(), "test-only") {
		t.Fatalf("before registering: err = %v, want unknown algorithm", err)
	}
	Algorithms["test-only"] = firstAlgorithm{}
	defer delete(Algorithms, "test-only")
	cfg, err := Parse([]byte(src), "c.json", "/h")
	if err != nil {
		t.Fatalf("a registered algorithm was refused: %v", err)
	}
	if lb := cfg.Profiles["tester"][0].LoadBalance; lb != "test-only" {
		t.Errorf("LoadBalance = %q", lb)
	}
	if names := AlgorithmNames(); !reflect.DeepEqual(names, []string{"random", "test-only"}) {
		t.Errorf("AlgorithmNames = %q", names)
	}
}
