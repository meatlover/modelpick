package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// TestFakeAppServer is the stand-in for `codex app-server`. It runs only when a
// renew test re-executes the test binary with MODELPICK_FAKE_APPSERVER set to a
// mode: ok, error, silent, chatty or exit.
func TestFakeAppServer(t *testing.T) {
	mode := os.Getenv("MODELPICK_FAKE_APPSERVER")
	if mode == "" {
		t.Skip("helper process, started by the renew tests")
	}
	logf, err := os.Create(os.Getenv("MODELPICK_FAKE_APPSERVER_LOG"))
	if err != nil {
		os.Exit(2)
	}
	defer logf.Close()
	rec := map[string]any{"codex_home": os.Getenv("CODEX_HOME"), "openai_api_key": os.Getenv("OPENAI_API_KEY"), "pid": os.Getpid()}
	var methods []string
	save := func() {
		rec["methods"] = methods
		logf.Truncate(0)
		logf.Seek(0, 0)
		json.NewEncoder(logf).Encode(rec)
	}
	save()
	if mode == "exit" {
		os.Stdout.Close()
		return
	}
	reply := func(s string) { os.Stdout.WriteString(s + "\n") }
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var m struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		methods = append(methods, m.Method)
		switch m.Method {
		case "initialize":
			rec["initialize"] = m.Params
		case "account/read":
			rec["account_read"] = m.Params
		}
		save()
		if m.ID == nil || mode == "silent" {
			continue
		}
		switch {
		case m.Method == "initialize":
			reply(`{"id":0,"result":{"userAgent":"fake"}}`)
		case mode == "error":
			reply(`{"id":1,"error":{"code":-32000,"message":"refresh failed"}}`)
		case mode == "chatty":
			reply(`{"method":"account/updated","params":{}}`)
			reply(`{"id":7,"result":{}}`)
			reply(`{"id":1,"result":{"account":{}}}`)
		default:
			reply(`{"id":1,"result":{"account":{}}}`)
		}
	}
	if mode == "lingers" {
		time.Sleep(time.Minute) // ignores stdin EOF, as a server that never exits would
	}
}

type fakeLog struct {
	Methods     []string
	CodexHome   string          `json:"codex_home"`
	APIKey      string          `json:"openai_api_key"`
	PID         int             `json:"pid"`
	Initialize  json.RawMessage `json:"initialize"`
	AccountRead json.RawMessage `json:"account_read"`
}

// fakeRenew renews acct against the fake app server in the given mode.
func fakeRenew(t *testing.T, mode string, ctx context.Context, env ...string) (error, fakeLog, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "log.json")
	home := t.TempDir()
	a := New(Options{
		AppServer: []string{os.Args[0], "-test.run=^TestFakeAppServer$"},
		Environ: func() []string {
			return append([]string{"MODELPICK_FAKE_APPSERVER=" + mode, "MODELPICK_FAKE_APPSERVER_LOG=" + logPath}, env...)
		},
	})
	err := a.Renew(ctx, adapter.Account{Name: "x", Vendor: "codex", CodexHome: home})
	var l fakeLog
	if b, rerr := os.ReadFile(logPath); rerr == nil {
		json.Unmarshal(b, &l)
	}
	return err, l, home
}

func TestRenewProtocol(t *testing.T) {
	err, l, home := fakeRenew(t, "ok", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(l.Methods, ","); got != "initialize,initialized,account/read" {
		t.Errorf("methods = %s", got)
	}
	if string(l.AccountRead) != `{"refreshToken":true}` {
		t.Errorf("account/read params = %s", l.AccountRead)
	}
	var init struct {
		ClientInfo struct{ Name string } `json:"clientInfo"`
	}
	json.Unmarshal(l.Initialize, &init)
	if init.ClientInfo.Name != "modelpick" {
		t.Errorf("clientInfo = %s", l.Initialize)
	}
	if l.CodexHome != home {
		t.Errorf("CODEX_HOME = %q, want %q", l.CodexHome, home)
	}
}

func TestRenewServerLingers(t *testing.T) {
	start := time.Now()
	err, l, _ := fakeRenew(t, "lingers", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("took %v", d)
	}
	if l.PID == 0 {
		t.Fatal("fake logged no pid")
	}
	if err := syscall.Kill(l.PID, 0); err == nil {
		t.Errorf("child %d still running", l.PID)
	}
}

func TestRenewRPCError(t *testing.T) {
	err, _, _ := fakeRenew(t, "error", context.Background())
	if err == nil || err.Error() != "codex app-server: refresh failed" {
		t.Fatalf("err = %v", err)
	}
}

func TestRenewSkipsOtherMessages(t *testing.T) {
	if err, _, _ := fakeRenew(t, "chatty", context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRenewNoAnswerTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err, _, _ := fakeRenew(t, "silent", ctx)
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestRenewServerExits(t *testing.T) {
	err, _, _ := fakeRenew(t, "exit", context.Background())
	if err == nil || err.Error() != "codex app-server: exited before answering" {
		t.Fatalf("err = %v", err)
	}
}

func TestRenewToolMissing(t *testing.T) {
	a := New(Options{AppServer: []string{"/nonexistent/codex", "app-server"}})
	err := a.Renew(context.Background(), adapter.Account{CodexHome: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "codex not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestRenewEnvDropsAPIKeys(t *testing.T) {
	err, l, _ := fakeRenew(t, "ok", context.Background(), "OPENAI_API_KEY=x")
	if err != nil {
		t.Fatal(err)
	}
	if l.APIKey != "" {
		t.Errorf("child saw OPENAI_API_KEY=%q", l.APIKey)
	}
}
