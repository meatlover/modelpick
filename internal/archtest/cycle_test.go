package archtest

import (
	"sort"
	"strings"
	"testing"
)

// findCycle returns one import cycle in graph as a path that starts and ends
// at the same node, or nil when graph is acyclic. Nodes are visited in sorted
// order so the reported cycle is stable.
func findCycle(graph map[string][]string) []string {
	const (
		unseen = iota
		onPath
		done
	)
	state := map[string]int{}
	var path []string
	var visit func(n string) []string
	visit = func(n string) []string {
		state[n] = onPath
		path = append(path, n)
		deps := append([]string(nil), graph[n]...)
		sort.Strings(deps)
		for _, d := range deps {
			switch state[d] {
			case onPath:
				for i, p := range path {
					if p == d {
						return append(append([]string(nil), path[i:]...), d)
					}
				}
			case unseen:
				if c := visit(d); c != nil {
					return c
				}
			}
		}
		path = path[:len(path)-1]
		state[n] = done
		return nil
	}
	nodes := make([]string, 0, len(graph))
	for n := range graph {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if state[n] == unseen {
			if c := visit(n); c != nil {
				return c
			}
		}
	}
	return nil
}

func TestFindCycleDetectsCycle(t *testing.T) {
	got := findCycle(map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}, "d": {"a"}})
	if strings.Join(got, ">") != "a>b>c>a" {
		t.Fatalf("want cycle a>b>c>a, got %q", got)
	}
	if got := findCycle(map[string][]string{"a": {"a"}}); strings.Join(got, ">") != "a>a" {
		t.Fatalf("want self-cycle a>a, got %q", got)
	}
	if got := findCycle(map[string][]string{"a": {"b", "c"}, "b": {"c"}, "c": nil}); got != nil {
		t.Fatalf("acyclic graph reported cycle %q", got)
	}
}

// TestLayerMapIsAcyclic: the allowed edges themselves form a DAG, so no
// future edit to the map can permit a cycle between layers.
func TestLayerMapIsAcyclic(t *testing.T) {
	graph := map[string][]string{"internal/adapter/<vendor>": vendorAllowed}
	for pkg, deps := range allowed {
		graph[pkg] = deps
	}
	if c := findCycle(graph); c != nil {
		t.Fatalf("layer map allows a cycle: %s", strings.Join(c, " -> "))
	}
}

// TestModuleImportGraphIsAcyclic checks the real graph, unrestricted
// packages (cli, cmd) included. The Go compiler also refuses an import
// cycle; this names it in the gate's own terms.
func TestModuleImportGraphIsAcyclic(t *testing.T) {
	module, imports := moduleImports(t)
	graph := map[string][]string{}
	for pkg, deps := range imports {
		for _, d := range deps {
			if d == module || strings.HasPrefix(d, module+"/") {
				graph[pkg] = append(graph[pkg], d)
			}
		}
	}
	if c := findCycle(graph); c != nil {
		t.Fatalf("import cycle: %s", strings.Join(c, " -> "))
	}
}
