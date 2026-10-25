package core

import "strconv"

// Algorithm chooses one member of a load balancing group. members are the
// group's members still in play (those that passed the checks that read no
// usage), and i in usable and in the result indexes that slice. usable is
// lazy: it is asked about one member at a time, and asking may read usage, so
// an algorithm asks only as far as it must. Pick returns the index of the
// chosen member, or false when no member is usable.
type Algorithm interface {
	Pick(members []Choice, usable func(i int) bool, shuffle func(n int, swap func(i, j int))) (int, bool)
}

// Algorithms is the one list of load balancing algorithms: validation, pick
// and the ui all read it. A new algorithm is a new entry here.
var Algorithms = map[string]Algorithm{"random": randomAlgorithm{}}

// AlgorithmNames returns the names in Algorithms, sorted.
func AlgorithmNames() []string { return sortedKeys(Algorithms) }

// randomAlgorithm tries the members in shuffled order and picks the first
// usable one.
type randomAlgorithm struct{}

func (randomAlgorithm) Pick(members []Choice, usable func(i int) bool, shuffle func(n int, swap func(i, j int))) (int, bool) {
	order := make([]int, len(members))
	for i := range order {
		order[i] = i
	}
	shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, i := range order {
		if usable(i) {
			return i, true
		}
	}
	return 0, false
}

// Position is one place in a profile's order: a plain choice (End = Start+1,
// LoadBalance "") or a group, whose members are the flat choices Start..End-1.
type Position struct {
	Start, End  int
	LoadBalance string
}

// Positions folds a profile's flat choices into positions. groups and lbs
// hold each choice's Group and LoadBalance; a run of equal non-zero group
// ids is one group.
func Positions(groups []int, lbs []string) []Position {
	var out []Position
	for start := 0; start < len(groups); {
		pos := Position{Start: start, End: start + 1}
		if groups[start] != 0 {
			for pos.End < len(groups) && groups[pos.End] == groups[start] {
				pos.End++
			}
			pos.LoadBalance = lbs[start]
		}
		out = append(out, pos)
		start = pos.End
	}
	return out
}

// Labels names each flat choice by its position: "1" for a plain choice,
// "2a", "2b", … for the members of a group at position 2.
func Labels(groups []int) []string {
	out := make([]string, len(groups))
	for n, pos := range Positions(groups, make([]string, len(groups))) {
		num := strconv.Itoa(n + 1)
		if groups[pos.Start] == 0 {
			out[pos.Start] = num
			continue
		}
		for i := pos.Start; i < pos.End; i++ {
			out[i] = num + letters(i-pos.Start)
		}
	}
	return out
}

// letters is a, b, …, z, aa, ab, … for 0, 1, ….
func letters(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('a'+(i-1)%26)) + s
	}
	return s
}
