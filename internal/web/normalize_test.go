package web

import (
	"strings"
	"testing"
)

// nilStub returns every list as nil, the zero value a Go backend produces
// by default; the page calls .find/.map on them and would blank a tab.
type nilStub struct{}

func (nilStub) State() State {
	return State{
		SHA256:   "abc",
		Accounts: []Account{{Name: "a"}},
		Models:   map[string][]Model{"a": {{ID: "m"}}},
		Windows:  map[string]map[string][]string{"a": {"m": nil}},
		Profiles: []Profile{{Name: "p"}, {Name: "q", Choices: []Choice{{Account: "a"}, {Account: "a", Cap: []any{}}}}},
	}
}
func (nilStub) Usage() []UsageRow                  { return nil }
func (nilStub) Refresh(a string) (UsageRow, error) { return UsageRow{Account: a}, nil }
func (nilStub) Doctor() Doctor                     { return Doctor{} }
func (nilStub) Apply(OpRequest) (OpResult, error)  { return OpResult{SHA256: "s"}, nil }

// allowedNulls are the fields whose null means something (uncapped, no
// level, no scope, no reading); every other null in a body is a bug.
var allowedNulls = []string{`"cap":null`, `"thinking":null`, `"scope":null`, `"reading_age_seconds":null`}

func unexpectedNull(body string) string {
	for _, ok := range allowedNulls {
		body = strings.ReplaceAll(body, ok, "")
	}
	if i := strings.Index(body, "null"); i >= 0 {
		return body[max(0, i-60):min(len(body), i+10)]
	}
	return ""
}

func TestNilListsEncodeAsEmpty(t *testing.T) {
	h := NewServer(nilStub{}, testPort, testToken)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/state"}, {"GET", "/api/usage"}, {"GET", "/api/doctor"},
		{"POST", "/api/usage/a/refresh"}, {"POST", "/api/op"},
	} {
		w := do(t, h, c.method, c.path, goodHost, "http://127.0.0.1:4455", true)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", c.path, w.Code, w.Body.String())
		}
		if near := unexpectedNull(w.Body.String()); near != "" {
			t.Errorf("%s: null near %q in %s", c.path, near, w.Body.String())
		}
	}
}

func TestNormalStateShapes(t *testing.T) {
	s := normalState(nilStub{}.State())
	if s.Accounts[0].Fields == nil || s.Models["a"][0].Thinking == nil || s.Windows["a"]["m"] == nil {
		t.Fatalf("inner lists left nil: %+v", s)
	}
	p := s.Profiles[0]
	if p.Choices == nil {
		t.Fatal("profile choices nil")
	}
	c := s.Profiles[1].Choices
	if c[0].Cap != nil || c[1].Cap == nil || len(c[1].Cap) != 0 {
		t.Fatalf("cap nil must stay nil and [] stay []: %#v %#v", c[0].Cap, c[1].Cap)
	}
	if c[0].SkipDuring == nil || c[0].AllowDuring == nil {
		t.Fatal("skip/allow lists nil")
	}
	e := normalState(State{})
	if e.Accounts == nil || e.Models == nil || e.Windows == nil || e.Schedules == nil || e.Profiles == nil {
		t.Fatalf("empty state lists nil: %+v", e)
	}
	if d := normalDoctor(Doctor{}); d.Findings == nil {
		t.Fatal("doctor findings nil")
	}
	if r := normalRow(UsageRow{}); r.Windows == nil {
		t.Fatal("usage windows nil")
	}
	if u := normalUsage(nil); u == nil {
		t.Fatal("usage nil")
	}
	if o := normalOpResult(OpResult{}); o.Findings == nil {
		t.Fatal("op findings nil")
	}
}

func TestNormalStateDoesNotWriteBackendSlices(t *testing.T) {
	in := State{Profiles: []Profile{{Name: "p", Choices: []Choice{{Account: "a"}}}}}
	normalState(in)
	if in.Profiles[0].Choices[0].SkipDuring != nil {
		t.Fatal("normalState wrote into the backend's slice")
	}
}
