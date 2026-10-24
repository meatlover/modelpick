package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
)

// badThinking is offered by neither DoctorFakes model: an error finding.
const badThinking = `{"account":"claude-main","model":"claude-opus-5-5","thinking":"max","cap":[90]}`

// editorConfig is a hand-written (not canonical) file: profile p has a
// capped claude choice and an uncapped codex fallback, both healthy.
func editorConfig(first string) string {
	return `{"version":1, "settings":{"default_cap":95},
 "accounts":{"codex-main":{"vendor":"codex","codex_home":"~/.codex"},
  "claude-main":{"vendor":"claude","config_dir":"~/.claude"}},
 "profiles":{"p":[` + first + `,
  {"account":"codex-main","model":"gpt-6.1-sol","thinking":"high"}]}}`
}

const goodFirst = `{"account":"claude-main","model":"claude-opus-5-5","thinking":"high","cap":[90]}`

type editorFx struct {
	ed     *app.Editor
	d      app.Deps
	path   string
	claude *apptest.Fake
	codex  *apptest.Fake
}

func newEditorFx(t *testing.T, cfgJSON string) editorFx {
	t.Helper()
	claude, codex := apptest.DoctorFakes()
	d := apptest.NewDeps(t, cfgJSON, claude, codex)
	path := platform.ConfigPath(d.Getenv, d.Home)
	return editorFx{app.NewEditor(d, path), d, path, claude, codex}
}

func (f editorFx) cacheDir() string { return platform.CacheDir(f.d.Getenv, f.d.Home) }

func (f editorFx) bytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f editorFx) load(t *testing.T) app.Snapshot {
	t.Helper()
	s, err := f.ed.Load()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func shaHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func wantRefused(t *testing.T, err error, reason string) {
	t.Helper()
	var r *app.Refused
	if !errors.As(err, &r) {
		t.Fatalf("err = %v (%T), want *Refused %q", err, err, reason)
	}
	if r.Reason != reason {
		t.Fatalf("reason = %q, want %q", r.Reason, reason)
	}
}

func TestApplyWritesCanonical(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	if err := os.Chmod(f.path, 0o640); err != nil {
		t.Fatal(err)
	}
	s0 := f.load(t)
	if s0.ReadOnly != "" || s0.SHA256 != shaHex(f.bytes(t)) || s0.Config == nil || s0.Doc == nil {
		t.Fatalf("load: %+v", s0)
	}
	s1, fs, err := f.ed.Apply(s0.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
	if err != nil {
		t.Fatal(err)
	}
	want, err := core.ParseDoc([]byte(editorConfig(goodFirst)))
	if err != nil {
		t.Fatal(err)
	}
	want.Settings["default_cap"] = 80
	got := f.bytes(t)
	if !bytes.Equal(got, want.Encode()) {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want.Encode())
	}
	if s1.SHA256 != shaHex(got) || !bytes.Equal(s1.Bytes, got) || s1.Config.Settings.DefaultCap != 80 {
		t.Fatalf("snapshot does not describe the written file: sha %s, cap %v", s1.SHA256, s1.Config.Settings.DefaultCap)
	}
	if fi, _ := os.Stat(f.path); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640 kept", fi.Mode().Perm())
	}
	for _, x := range fs {
		if x.Severity == "error" {
			t.Fatalf("unexpected error finding %+v", x)
		}
	}
}

func TestApplyStaleBase(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	s0 := f.load(t)
	hand := []byte(editorConfig(goodFirst) + "\n")
	if err := os.WriteFile(f.path, hand, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.ed.Apply(s0.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
	var c *app.Conflict
	if !errors.As(err, &c) || c.SHA256 != shaHex(hand) {
		t.Fatalf("err = %v, want Conflict with sha %s", err, shaHex(hand))
	}
	if !bytes.Equal(f.bytes(t), hand) {
		t.Fatal("file changed on a conflict")
	}
}

// A file that does not parse is read-only: broken JSON, or JSON that
// decodes but fails validation (a pace below 5).
func TestApplyReadOnly(t *testing.T) {
	for _, c := range []struct{ name, file, readOnly string }{
		{"broken JSON", `{"version":1,"profiles":`, ""},
		{"pace+2", editorConfig(strings.Replace(goodFirst, `[90]`, `["pace+2"]`, 1)),
			`profiles.p[0].cap[0]: "pace+2": N must be 5–50`},
	} {
		f := newEditorFx(t, c.file)
		s := f.load(t)
		if s.ReadOnly == "" || !strings.HasSuffix(s.ReadOnly, c.readOnly) {
			t.Fatalf("%s: ReadOnly = %q, want a parse error ending %q", c.name, s.ReadOnly, c.readOnly)
		}
		_, _, err := f.ed.Apply(s.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
		wantRefused(t, err, "config.json does not parse; fix it by hand")
		if string(f.bytes(t)) != c.file {
			t.Fatalf("%s: file changed", c.name)
		}
	}
}

func TestApplyParseRefused(t *testing.T) {
	app.SetTestOp(t, "test.version2", func(doc *core.Doc) { doc.Version = 2 })
	f := newEditorFx(t, editorConfig(goodFirst))
	s := f.load(t)
	_, _, err := f.ed.Apply(s.SHA256, "test.version2", nil)
	wantRefused(t, err, "Config: version: must be 1")
	if string(f.bytes(t)) != editorConfig(goodFirst) {
		t.Fatal("file changed")
	}
}

const badThinkingErr = `thinking "max" not supported by claude-opus-5-5 (supported: low, high)`

// A second copy of an existing error is new; the refusal quotes the copy's
// real subject, and the refused change does not leak into the next apply.
func TestApplyGateNewError(t *testing.T) {
	app.SetTestOp(t, "test.dup", func(doc *core.Doc) {
		cs := doc.Profiles["p"]
		doc.Profiles["p"] = append([]core.DocChoice{cs[0], cs[0]}, cs[1:]...)
	})
	f := newEditorFx(t, editorConfig(badThinking))
	s := f.load(t)
	_, _, err := f.ed.Apply(s.SHA256, "test.dup", nil)
	wantRefused(t, err, "Profile p, choice 1: "+badThinkingErr) // the first in document order
	if string(f.bytes(t)) != editorConfig(badThinking) {
		t.Fatal("file changed")
	}
	s1, _, err := f.ed.Apply(s.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s1.Config.Profiles["p"]); n != 2 {
		t.Fatalf("profile p has %d choices: the refused copy leaked", n)
	}
}

// profileConfig is editorConfig with profile p's choices given in full.
func profileConfig(choices ...string) string {
	return `{"version":1,"settings":{"default_cap":95},
 "accounts":{"codex-main":{"vendor":"codex","codex_home":"~/.codex"},
  "claude-main":{"vendor":"claude","config_dir":"~/.claude"}},
 "profiles":{"p":[` + strings.Join(choices, ",") + `]}}`
}

const uncappedCodex = `{"account":"codex-main","model":"gpt-6.1-sol","thinking":"high"}`

// A copy inserted before the original is quoted at its own (earlier)
// position: the gate walks findings by profile and choice number, so
// choice 9 comes before choice 11.
func TestApplyGateQuotesFirstInDocumentOrder(t *testing.T) {
	cases := []struct {
		name     string
		choices  int // capped good choices before the bad one
		from, at int // copy choice index from to index at
		want     string
	}{
		{"original at 3, copy at 2", 2, 2, 1, "Profile p, choice 2: " + badThinkingErr},
		{"original at 10, copy at 9", 9, 9, 8, "Profile p, choice 9: " + badThinkingErr},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app.SetTestOp(t, "test.copy", func(doc *core.Doc) {
				cs := doc.Profiles["p"]
				out := append([]core.DocChoice{}, cs[:c.at]...)
				out = append(append(out, cs[c.from]), cs[c.at:]...)
				doc.Profiles["p"] = out
			})
			var list []string
			for i := 0; i < c.choices; i++ {
				list = append(list, goodFirst)
			}
			cfg := profileConfig(append(list, badThinking, uncappedCodex)...)
			f := newEditorFx(t, cfg)
			s := f.load(t)
			_, _, err := f.ed.Apply(s.SHA256, "test.copy", nil)
			wantRefused(t, err, c.want)
			if string(f.bytes(t)) != cfg {
				t.Fatal("file changed")
			}
		})
	}
}

// checkHook runs onCheck inside the adapter's Check, i.e. mid-gate.
type checkHook struct {
	*apptest.Fake
	onCheck func()
}

func (h checkHook) Check(ctx context.Context, acct adapter.Account) []adapter.Finding {
	h.onCheck()
	return h.Fake.Check(ctx, acct)
}

// A hand edit that lands while the gate runs is a conflict, not overwritten,
// and no safety copy is taken for that attempt.
func TestApplyEditDuringGate(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	hand := []byte(editorConfig(goodFirst) + "\n")
	edited := false
	f.d.Adapters["claude"] = checkHook{f.claude, func() {
		if !edited {
			edited = true
			if err := os.WriteFile(f.path, hand, 0o600); err != nil {
				t.Error(err)
			}
		}
	}}
	f.ed = app.NewEditor(f.d, f.path)
	s := f.load(t)
	_, _, err := f.ed.Apply(s.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
	var c *app.Conflict
	if !edited || !errors.As(err, &c) || c.SHA256 != shaHex(hand) {
		t.Fatalf("edited %v, err = %v, want Conflict with sha %s", edited, err, shaHex(hand))
	}
	if !bytes.Equal(f.bytes(t), hand) {
		t.Fatal("the hand edit was overwritten")
	}
	if _, err := os.Stat(filepath.Join(f.cacheDir(), "config.before-ui.json")); !os.IsNotExist(err) {
		t.Fatalf("safety copy taken for a conflicted attempt: %v", err)
	}
}

// An existing error that changes position is still the same error.
func TestApplyGateErrorMoves(t *testing.T) {
	f := newEditorFx(t, editorConfig(badThinking))
	s := f.load(t)
	s1, fs, err := f.ed.Apply(s.SHA256, "choice.move",
		json.RawMessage(`{"profile":"p","from":0,"to":1,"expect":`+badThinking+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if s1.Config.Profiles["p"][1].Account != "claude-main" {
		t.Fatal("not moved")
	}
	want := app.Finding{Severity: "error", Subject: "profile p, choice 2", Message: badThinkingErr}
	if len(fs) == 0 || !reflect.DeepEqual(fs[0], want) {
		t.Fatalf("findings = %+v, want %+v first", fs, want)
	}
}

// Adapter Check findings are machine state, not config validity: a new
// account whose credential is missing is added, and the finding reported.
func TestApplyCheckFindingsDoNotGate(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	f.claude.Checks["claude-new"] = []adapter.Finding{{Severity: adapter.SeverityError, Message: "no credential in ~/.claude-new"}}
	s := f.load(t)
	s1, fs, err := f.ed.Apply(s.SHA256, "account.add",
		json.RawMessage(`{"name":"claude-new","vendor":"claude","fields":{"config_dir":"~/.claude-new"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s1.Config.Accounts["claude-new"]; !ok {
		t.Fatal("account not written")
	}
	want := app.Finding{Severity: "error", Subject: "claude-new", Message: "no credential in ~/.claude-new"}
	if len(fs) == 0 || !reflect.DeepEqual(fs[0], want) {
		t.Fatalf("findings = %+v, want %+v first", fs, want)
	}
}

func TestApplyGateOldErrorTolerated(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	f.claude.Checks["claude-main"] = []adapter.Finding{{Severity: adapter.SeverityError, Message: "claude not found on PATH"}}
	s := f.load(t)
	_, fs, err := f.ed.Apply(s.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
	if err != nil {
		t.Fatal(err)
	}
	want := app.Finding{Severity: "error", Subject: "claude-main", Message: "claude not found on PATH"}
	if len(fs) == 0 || !reflect.DeepEqual(fs[0], want) {
		t.Fatalf("findings = %+v, want %+v first", fs, want)
	}
	if f.claude.CheckCalls["claude-main"] != 1 {
		t.Fatalf("Check ran %d times for one apply, want 1 (cached)", f.claude.CheckCalls["claude-main"])
	}
}

func TestApplyBackupOnce(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	orig := f.bytes(t)
	s := f.load(t)
	for _, v := range []string{"80", "70"} {
		var err error
		s, _, err = f.ed.Apply(s.SHA256, "settings.default_cap", json.RawMessage(`{"value":`+v+`}`))
		if err != nil {
			t.Fatal(err)
		}
	}
	backup := filepath.Join(f.cacheDir(), "config.before-ui.json")
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig) {
		t.Fatalf("backup:\n%s\nwant the original:\n%s", got, orig)
	}
	if fi, _ := os.Stat(backup); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestApplyBackupFails(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	if err := os.MkdirAll(filepath.Dir(f.cacheDir()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.cacheDir(), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := f.load(t)
	_, _, err := f.ed.Apply(s.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
	wantRefused(t, err, "Could not save the safety copy") // the page appends "config.json is unchanged."
	if string(f.bytes(t)) != editorConfig(goodFirst) {
		t.Fatal("file changed")
	}
}

func TestApplyRefusedKeepsSHA(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	s0 := f.load(t)
	s1, _, err := f.ed.Apply(s0.SHA256, "profile.remove", json.RawMessage(`{"name":"nope"}`))
	wantRefused(t, err, "Unknown profile nope")
	if s1.SHA256 != s0.SHA256 || string(f.bytes(t)) != editorConfig(goodFirst) {
		t.Fatalf("sha %s -> %s or file changed", s0.SHA256, s1.SHA256)
	}
	if _, _, err := f.ed.Apply(s1.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`)); err != nil {
		t.Fatalf("the unchanged sha must still apply: %v", err)
	}
}

// The cap refusal names windows by the labels of the saved reading, and the
// thinking default comes from the cached model list.
func TestApplyOpEnvFromCacheAndReading(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	apptest.WriteStored(t, f.d, "claude-main",
		`{"last_good":{"taken_at":"2026-10-22T12:00:00Z","windows":[{"label":"5h","length":18000000000000,"used_pct":10},{"label":"7d","length":604800000000000,"used_pct":20}]}}`)
	s := f.load(t)
	_, _, err := f.ed.Apply(s.SHA256, "choice.set",
		json.RawMessage(`{"profile":"p","index":0,"field":"cap","value":[90,"abc"],"expect":`+goodFirst+`}`))
	wantRefused(t, err, "Profile p, choice 1, 7d cap: abc is not a number, pace+N or default")
	s, _, err = f.ed.Apply(s.SHA256, "profile.add", json.RawMessage(`{"name":"q"}`))
	if err != nil {
		t.Fatal(err)
	}
	c := s.Config.Profiles["q"][0]
	if c.Account != "claude-main" || c.Model != "claude-opus-5-5" || c.Thinking == nil || *c.Thinking != "low" {
		t.Fatalf("new choice = %+v", c)
	}
}

func TestApplyAccountOps(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	added := make(chan string, 2)
	f.ed.OnAccountAdded = func(name string) { added <- name }
	s := f.load(t)
	s, _, err := f.ed.Apply(s.SHA256, "account.add",
		json.RawMessage(`{"name":"claude-new","vendor":"claude","fields":{"config_dir":"~/.claude-new"}}`))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-added:
		if n != "claude-new" {
			t.Fatalf("OnAccountAdded(%q)", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnAccountAdded was not called")
	}
	if n := f.claude.CheckCalls["claude-new"]; n != 1 {
		t.Fatalf("Check calls after add = %d, want 1 (the gate)", n)
	}
	s, _, err = f.ed.Apply(s.SHA256, "settings.default_cap", json.RawMessage(`{"value":80}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := f.claude.CheckCalls["claude-new"]; n != 2 {
		t.Fatalf("Check calls = %d, want 2: the add must forget the cached check", n)
	}
	if _, _, err = f.ed.Apply(s.SHA256, "account.remove", json.RawMessage(`{"name":"claude-new"}`)); err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatal("OnAccountAdded called for an operation that is not account.add")
	}
}

// A tool missing for one account is an ungrouped Check finding; adding a
// second account with the same missing tool turns it into a grouped one. The
// gate must not count the grouped finding as new.
func TestGateIgnoresGroupedCheckErrors(t *testing.T) {
	f := newEditorFx(t, editorConfig(goodFirst))
	for _, n := range []string{"claude-main", "claude-new"} {
		f.claude.Checks[n] = []adapter.Finding{{Severity: adapter.SeverityError, Account: n, Message: "claude not found on PATH"}}
	}
	s := f.load(t)
	s1, fs, err := f.ed.Apply(s.SHA256, "account.add",
		json.RawMessage(`{"name":"claude-new","vendor":"claude","fields":{"config_dir":"~/.claude-new"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s1.Config.Accounts["claude-new"]; !ok {
		t.Fatal("account not written")
	}
	want := app.Finding{Severity: "error", Subject: "claude-main, claude-new", Message: "claude not found on PATH",
		Accounts: []string{"claude-main", "claude-new"}}
	if len(fs) == 0 || !reflect.DeepEqual(fs[0], want) {
		t.Fatalf("findings = %+v, want %+v first", fs, want)
	}
}
