package pathx

import "testing"

func TestExpandHome(t *testing.T) {
	cases := map[string]string{
		"~/.claude-rec/": "/home/u/.claude-rec",
		"~":              "/home/u",
		"/a/../b":        "/b",
		"":               "",
	}
	for in, want := range cases {
		if got := ExpandHome(in, "/home/u"); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}
