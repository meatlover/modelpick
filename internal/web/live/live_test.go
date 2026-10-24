package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/adapter/opencodego"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/web"
	"github.com/meatlover/modelpick/internal/web/webtest"
)

// secretKey is go-1's API key, in a temp key file. No response may hold it.
const secretKey = "sk-live-test-7f3a9c0e5b1d4a2f"

const configTmpl = `{
  "version": 1,
  "settings": {"default_cap": 90},
  "accounts": {
    "codex-main": {"vendor": "codex", "codex_home": "~/.codex"},
    "claude-main": {"vendor": "claude", "config_dir": "~/.claude"},
    "go-1": {"vendor": "opencode-go", "key_file": %q, "key_line": 1},
    "claude-rec": {"vendor": "claude", "config_dir": "~/.claude-rec"}
  },
  "schedules": {"night": {"days": ["mon"], "utc": ["00:00-02:30"]}},
  "profiles": {
    "rec": [{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium", "cap": []}],
    "coder": [
      {"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, "default", "pace+10", "default"]},
      {"account": "go-1", "model": "opencode-go/deepseek-v4.1-flash", "thinking": null, "skip_during": ["night"]},
      {"account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high"}
    ]
  }
}`

// claude-main's saved reading: two general windows (one past its reset,
// one with no reset time) and one scoped to Fable, ten minutes old.
const claudeStored = `{"last_good":{"taken_at":"2026-10-22T11:50:00Z","windows":[
 {"label":"weekly","length":604800000000000,"used_pct":58,"resets_at":"2026-10-24T00:00:00Z"},
 {"label":"session","length":18000000000000,"used_pct":31,"resets_at":"2026-10-22T11:00:00Z"},
 {"label":"weekly-fable","length":604800000000000,"used_pct":12,"scope_model_name":"fable"},
 {"label":"other","length":604800000000000,"used_pct":1,"scope_model_id":"claude-haiku-5-5"}]}}`

const goUsage = `{"usage":{"rolling":{"status":"ok","percent":3,"resetsAt":"2026-10-22T15:00:00Z"},
 "weekly":{"status":"ok","percent":40,"resetsAt":"2026-10-25T00:00:00Z"},
 "monthly":{"status":"ok","percent":70,"resetsAt":"2026-11-01T00:00:00Z"}}}`

// goServer answers the OpenCode Go usage endpoint without a network.
type goServer struct {
	mu    sync.Mutex
	calls int
}

func (g *goServer) RoundTrip(r *http.Request) (*http.Response, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r,
		Body: io.NopCloser(strings.NewReader(goUsage))}, nil
}

func (g *goServer) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

type fixture struct {
	t       *testing.T
	d       app.Deps
	path    string
	claude  *apptest.Fake
	codex   *apptest.Fake
	goSrv   *goServer
	mu      sync.Mutex
	firsts  []string
	firstCh chan string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "go-keys")
	if err := os.WriteFile(keyFile, []byte("# keys\n"+secretKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, claude: apptest.NewFake("claude"), codex: apptest.NewFake("codex"),
		goSrv: &goServer{}, firstCh: make(chan string, 16)}
	opus := adapter.Model{ID: "claude-opus-5-5", Thinking: []string{"low", "high"}}
	fable := adapter.Model{ID: "claude-fable-5-1", Thinking: []string{"high"}}
	sonnet := adapter.Model{ID: "claude-sonnet-5-5", Thinking: []string{"medium"}}
	f.claude.ModelList["claude-main"] = []adapter.Model{opus, fable}
	f.claude.ModelList["claude-rec"] = []adapter.Model{sonnet}
	f.claude.Readings["claude-main"] = apptest.ClaudeReading(30, 50)
	f.codex.ModelList["codex-main"] = []adapter.Model{{ID: "gpt-6.1-sol", Thinking: []string{"low", "high"}}}
	f.d = apptest.NewDeps(t, fmt.Sprintf(configTmpl, keyFile), f.claude, f.codex)
	f.d.Adapters["opencode-go"] = opencodego.New(opencodego.Options{BaseURL: "http://usage.invalid",
		HTTP: &http.Client{Transport: f.goSrv}, Now: func() time.Time { return apptest.Now },
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("opencode-go/deepseek-v4.1-flash\n"), nil
		},
		LookPath: func(string) (string, error) { return "/usr/bin/opencode", nil }})
	f.path = f.d.Getenv("MODELPICK_CONFIG")
	apptest.WriteStored(t, f.d, "claude-main", claudeStored)
	return f
}

// first records a first-reading trigger instead of taking the reading.
func (f *fixture) first(_ context.Context, name string) {
	f.mu.Lock()
	f.firsts = append(f.firsts, name)
	f.mu.Unlock()
	f.firstCh <- name
}

// live serves the fixture's config; the test's cleanup closes it.
func (f *fixture) live() *Live {
	l := newLive(f.d, f.path, f.first)
	f.t.Cleanup(l.Close)
	return l
}

func TestConformance(t *testing.T) {
	var f *fixture
	var orig []byte
	write := func(t *testing.T, b []byte) {
		if err := os.WriteFile(f.path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	webtest.Run(t, webtest.Harness{
		New: func(t *testing.T) web.Backend {
			f = newFixture(t)
			var err error
			if orig, err = os.ReadFile(f.path); err != nil {
				t.Fatal(err)
			}
			return f.live()
		},
		HandEdit: func(t *testing.T) {
			b, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			write(t, []byte(strings.Replace(string(b), `"default_cap": 90`, `"default_cap": 85`, 1)))
		},
		Break: func(t *testing.T) { write(t, []byte(`{"version": 1, "accounts": `)) },
		BadThinking: func(t *testing.T) (string, int) {
			b, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			const ok = `"model": "gpt-6.1-sol", "thinking": "high"`
			write(t, []byte(strings.Replace(string(b), ok, `"model": "gpt-6.1-sol", "thinking": "ultra"`, 1)))
			return "coder", 2
		},
		Fix:         func(t *testing.T) { write(t, orig) },
		Secrets:     []string{secretKey},
		Refreshable: "claude-main",
		UsageCalls:  func() int { return f.usageCalls() },
		MissingTool: func(*testing.T) []string {
			names := []string{"claude-main", "claude-rec"}
			for _, n := range names {
				f.claude.Checks[n] = []adapter.Finding{{Severity: adapter.SeverityError, Account: n,
					Message: "claude not found on PATH"}}
			}
			return names
		},
	})
}

// postDoctorAll sends POST /api/doctor/all through the real handler, logged
// in, and answers the decoded body.
func postDoctorAll(t *testing.T, l *Live) web.Doctor {
	t.Helper()
	const port, token = 4455, "live-test-token"
	h := web.NewServer(l, port, token)
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("GET", "http://127.0.0.1:4455/?t="+token, nil))
	r := httptest.NewRequest("POST", "http://127.0.0.1:4455/api/doctor/all", nil)
	r.Header.Set("Origin", "http://127.0.0.1:4455")
	for _, c := range login.Result().Cookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var d web.Doctor
	if err := json.Unmarshal(w.Body.Bytes(), &d); w.Code != 200 || err != nil {
		t.Fatalf("POST /api/doctor/all: %d %s (%v)", w.Code, w.Body.String(), err)
	}
	return d
}

// TestLiveDoctorAllCallsStaleAccount: claude-main's reading is 20 minutes
// old, past the 15-minute floor, so Check all calls it; claude-rec's is 5
// minutes old and is not called. Plain doctor first calls nobody.
func TestLiveDoctorAllCallsStaleAccount(t *testing.T) {
	f := newFixture(t)
	apptest.WriteStored(t, f.d, "claude-main", strings.Replace(claudeStored, "11:50:00Z", "11:40:00Z", 1))
	apptest.WriteStored(t, f.d, "claude-rec", strings.Replace(claudeStored, "11:50:00Z", "11:55:00Z", 1))
	l := f.live()
	l.Doctor()
	if n := f.claude.TotalUsage(); n != 0 {
		t.Fatalf("plain doctor made %d claude usage calls", n)
	}
	postDoctorAll(t, l)
	if n := f.claude.TotalUsage(); n != 1 {
		t.Errorf("Check all made %d claude usage calls, want 1 (claude-main)", n)
	}
	if row := rowOf(t, l.Usage(), "claude-main"); row.ReadingAgeSeconds == nil || *row.ReadingAgeSeconds != 0 {
		t.Errorf("claude-main after Check all: %+v, want a new reading", row)
	}
}

// DoctorAll keeps Doctor's guard: a config that does not parse is its one
// finding, and no account is called.
func TestLiveDoctorAllReadOnlyMakesNoCall(t *testing.T) {
	f := newFixture(t)
	l := f.live()
	if err := os.WriteFile(f.path, []byte(`{"version": 1, "accounts": `), 0o600); err != nil {
		t.Fatal(err)
	}
	d := postDoctorAll(t, l)
	if d.Errors != 1 || len(d.Findings) != 1 || d.Findings[0].Subject != "config" {
		t.Errorf("doctor all on a broken config: %+v", d)
	}
	if n := f.usageCalls(); n != 0 {
		t.Errorf("doctor all on a broken config made %d usage calls", n)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStateAccounts(t *testing.T) {
	f := newFixture(t)
	s := f.live().State()
	keyFile := strings.Split(strings.Split(mustJSON(t, s.Accounts[3].Fields), `"key_file":"`)[1], `"`)[0]
	want := `[{"name":"claude-main","vendor":"claude","fields":{"config_dir":"~/.claude"},"location":"~/.claude","source":"fake: claude-main"},` +
		`{"name":"claude-rec","vendor":"claude","fields":{"config_dir":"~/.claude-rec"},"location":"~/.claude-rec","source":"fake: claude-rec"},` +
		`{"name":"codex-main","vendor":"codex","fields":{"codex_home":"~/.codex"},"location":"~/.codex","source":"fake: codex-main"},` +
		`{"name":"go-1","vendor":"opencode-go","fields":{"key_file":"` + keyFile + `","key_line":1},"location":"` + keyFile + `","source":"key line 1 of 1"}]`
	if got := mustJSON(t, s.Accounts); got != want {
		t.Errorf("accounts:\n got %s\nwant %s", got, want)
	}
	if !strings.HasSuffix(keyFile, "go-keys") {
		t.Errorf("key_file %q", keyFile)
	}
	if s.Path != f.path || s.ReadOnly != "" || s.DefaultCap != 90 || len(s.SHA256) != 64 {
		t.Errorf("path %q read_only %q default_cap %v sha %q", s.Path, s.ReadOnly, s.DefaultCap, s.SHA256)
	}
}

func TestStateModelsWindowsSchedulesProfiles(t *testing.T) {
	f := newFixture(t)
	s := f.live().State()
	checks := map[string]string{
		"models": mustJSON(t, s.Models),
		"windows": `{"claude-main":{"claude-fable-5-1":["5h","7d","7d fable"],` +
			`"claude-opus-5-5":["5h","7d"]}}`,
		"profiles": `[{"name":"coder","choices":[` +
			`{"account":"claude-main","model":"claude-opus-5-5","thinking":"high","cap":[90,"default","pace+10"],"skip_during":[],"allow_during":[]},` +
			`{"account":"go-1","model":"opencode-go/deepseek-v4.1-flash","thinking":null,"cap":null,"skip_during":["night"],"allow_during":[]},` +
			`{"account":"codex-main","model":"gpt-6.1-sol","thinking":"high","cap":null,"skip_during":[],"allow_during":[]}]},` +
			`{"name":"rec","choices":[{"account":"claude-rec","model":"claude-sonnet-5-5","thinking":"medium","cap":[],"skip_during":[],"allow_during":[]}]}]`,
	}
	if got := mustJSON(t, s.Windows); got != checks["windows"] {
		t.Errorf("windows: %s", got)
	}
	if got := mustJSON(t, s.Profiles); got != checks["profiles"] {
		t.Errorf("profiles:\n got %s\nwant %s", got, checks["profiles"])
	}
	wantModels := `{"claude-main":[{"id":"claude-opus-5-5","thinking":["low","high"]},{"id":"claude-fable-5-1","thinking":["high"]}],` +
		`"claude-rec":[{"id":"claude-sonnet-5-5","thinking":["medium"]}],"codex-main":[{"id":"gpt-6.1-sol","thinking":["low","high"]}],` +
		`"go-1":[{"id":"opencode-go/deepseek-v4.1-flash","thinking":[]}]}`
	if checks["models"] != wantModels {
		t.Errorf("models:\n got %s\nwant %s", checks["models"], wantModels)
	}
	if len(s.Schedules) != 1 || s.Schedules[0].Name != "night" {
		t.Fatalf("schedules %+v", s.Schedules)
	}
	cells := s.Schedules[0].Cells
	if cells[1][0] != 1 || cells[1][1] != 1 || cells[1][2] != 0.5 || cells[1][3] != 0 || cells[0][0] != 0 {
		t.Errorf("night cells, Monday: %v", cells[1][:4])
	}
}

// Ruling 1: a missing config.json is read-only; nothing is created.
func TestMissingConfigIsReadOnly(t *testing.T) {
	f := newFixture(t)
	f.path = filepath.Join(t.TempDir(), "config.json")
	l := f.live()
	s := l.State()
	want := "config.json not found at " + f.path
	if s.ReadOnly != want || s.SHA256 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("read_only %q sha %s", s.ReadOnly, s.SHA256)
	}
	if mustJSON(t, s.Accounts)+mustJSON(t, s.Profiles)+mustJSON(t, s.Schedules) != "[][][]" {
		t.Errorf("lists not empty: %+v", s)
	}
	_, err := l.Apply(web.OpRequest{Base: s.SHA256, Op: "schedule.add", Args: json.RawMessage(`{"name":"x"}`)})
	if r, ok := err.(web.Refused); !ok || r.Reason != want {
		t.Errorf("apply: %#v", err)
	}
	if d := l.Doctor(); d.Errors != 1 || d.Findings[0].Subject != "config" {
		t.Errorf("doctor %+v", d)
	}
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Errorf("config.json created: %v", err)
	}
	if len(f.firsts) != 0 {
		t.Errorf("first readings without a config: %v", f.firsts)
	}
}

// A config that decodes but does not validate keeps its lists.
func TestInvalidConfigKeepsLists(t *testing.T) {
	f := newFixture(t)
	b, _ := os.ReadFile(f.path)
	bad := strings.Replace(string(b), `"vendor": "codex"`, `"vendor": "nope"`, 1)
	if err := os.WriteFile(f.path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	s := f.live().State()
	if !strings.Contains(s.ReadOnly, "nope") || len(s.Accounts) != 4 || len(s.Profiles) != 2 {
		t.Errorf("read_only %q, %d accounts, %d profiles", s.ReadOnly, len(s.Accounts), len(s.Profiles))
	}
	if len(s.Models) != 0 || len(s.Windows) != 0 {
		t.Errorf("models %v windows %v", s.Models, s.Windows)
	}
}

var errBoom = errors.New("boom")
