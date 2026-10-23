package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/app"
)

// syncBuf is a writer the test can read while the command still writes.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// uiRig is a ui command with every seam faked and all paths in a temp dir.
type uiRig struct {
	t        *testing.T
	d        app.Deps
	env      uiEnv
	cacheDir string
	sig      chan os.Signal
	listened []string
	failOn   string // an address the fake listen refuses
	pingOK   bool
	alive    bool
	opened   []string
	openErr  error
	out, err syncBuf
}

func newUIRig(t *testing.T) *uiRig {
	t.Helper()
	dir := t.TempDir()
	r := &uiRig{t: t, cacheDir: filepath.Join(dir, "modelpick"), sig: make(chan os.Signal, 1)}
	env := map[string]string{"XDG_CACHE_HOME": dir, "MODELPICK_CONFIG": filepath.Join(dir, "none.json")}
	r.d = app.Deps{Getenv: func(k string) string { return env[k] }, Home: dir, Now: time.Now}
	r.env = uiEnv{
		Listen: func(addr string) (net.Listener, error) {
			r.listened = append(r.listened, addr)
			if addr == r.failOn {
				return nil, errors.New("address in use")
			}
			return net.Listen("tcp", "127.0.0.1:0")
		},
		Ping:     func(int, string) bool { return r.pingOK },
		Open:     func(u string) error { r.opened = append(r.opened, u); return r.openErr },
		Signal:   func() (<-chan os.Signal, func()) { return r.sig, func() {} },
		PidAlive: func(int) bool { return r.alive },
		Pid:      4242,
	}
	return r
}

func (r *uiRig) uiFile() string { return filepath.Join(r.cacheDir, "ui.json") }

func (r *uiRig) writeUIFile(pid, port int, token string) {
	r.t.Helper()
	if err := os.MkdirAll(r.cacheDir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	b := fmt.Sprintf(`{"pid":%d,"port":%d,"token":%q}`, pid, port, token)
	if err := os.WriteFile(r.uiFile(), []byte(b), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// start runs the command in a goroutine and waits for the printed line.
func (r *uiRig) start(args ...string) (done chan int, line string) {
	r.t.Helper()
	done = make(chan int, 1)
	go func() { done <- runUIWith(args, &r.out, &r.err, r.d, r.env) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.out.String(); strings.HasSuffix(s, "\n") {
			return done, strings.TrimSpace(s)
		}
		select {
		case code := <-done:
			r.t.Fatalf("exited %d before printing: stdout %q stderr %q", code, r.out.String(), r.err.String())
		case <-time.After(5 * time.Millisecond):
		}
	}
	r.t.Fatal("no output line in 5 s")
	return nil, ""
}

func (r *uiRig) stop(done chan int) int {
	r.t.Helper()
	r.sig <- os.Interrupt
	select {
	case code := <-done:
		return code
	case <-time.After(5 * time.Second):
		r.t.Fatal("did not exit after the signal")
		return -1
	}
}

var uiLine = regexp.MustCompile(`^modelpick ui: http://127\.0\.0\.1:(\d+)/\?t=([0-9a-zA-Z_-]+)$`)

func readUIFile(t *testing.T, path string) (m struct {
	Pid   int    `json:"pid"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUIReusesLiveInstance(t *testing.T) {
	r := newUIRig(t)
	r.writeUIFile(777, 5555, "tok")
	r.alive, r.pingOK = true, true
	code := runUIWith(nil, &r.out, &r.err, r.d, r.env)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, r.err.String())
	}
	want := "modelpick ui: http://127.0.0.1:5555/?t=tok\n"
	if r.out.String() != want {
		t.Errorf("stdout %q, want %q", r.out.String(), want)
	}
	if len(r.opened) != 1 || r.opened[0] != "http://127.0.0.1:5555/?t=tok" {
		t.Errorf("opened %v", r.opened)
	}
	if len(r.listened) != 0 {
		t.Errorf("listened %v; a live instance must not start a second server", r.listened)
	}
	if got := readUIFile(t, r.uiFile()); got.Pid != 777 {
		t.Errorf("ui.json was rewritten: %+v", got)
	}
}

func TestUIStaleFileReplaced(t *testing.T) {
	for name, tc := range map[string]struct{ alive, ping bool }{
		"pid gone":       {false, true},
		"no ping answer": {true, false},
	} {
		t.Run(name, func(t *testing.T) {
			r := newUIRig(t)
			r.writeUIFile(777, 5555, "oldtoken")
			r.alive, r.pingOK = tc.alive, tc.ping
			done, line := r.start("--no-open")
			m := uiLine.FindStringSubmatch(line)
			if m == nil || m[2] == "oldtoken" {
				t.Fatalf("line %q", line)
			}
			got := readUIFile(t, r.uiFile())
			if got.Pid != 4242 || fmt.Sprint(got.Port) != m[1] || got.Token != m[2] {
				t.Errorf("ui.json %+v does not match line %q", got, line)
			}
			if fi, _ := os.Stat(r.uiFile()); fi.Mode().Perm() != 0o600 {
				t.Errorf("ui.json mode %v", fi.Mode().Perm())
			}
			if code := r.stop(done); code != 0 {
				t.Errorf("exit %d", code)
			}
		})
	}
}

func TestUIPrefersPreviousPort(t *testing.T) {
	r := newUIRig(t)
	r.writeUIFile(777, 5555, "old")
	done, _ := r.start("--no-open")
	if len(r.listened) != 1 || r.listened[0] != "127.0.0.1:5555" {
		t.Errorf("listened %v, want the previous port first", r.listened)
	}
	r.stop(done)

	r = newUIRig(t)
	r.writeUIFile(777, 5555, "old")
	r.failOn = "127.0.0.1:5555"
	done, _ = r.start("--no-open")
	if len(r.listened) != 2 || r.listened[1] != "127.0.0.1:0" {
		t.Errorf("listened %v, want a fall back to port 0", r.listened)
	}
	r.stop(done)

	r = newUIRig(t)
	r.writeUIFile(777, 5555, "old")
	done, _ = r.start("--no-open", "--port", "6666")
	if r.listened[0] != "127.0.0.1:6666" {
		t.Errorf("listened %v, an explicit --port wins", r.listened)
	}
	r.stop(done)
}

func TestUIOpenFailureIsANote(t *testing.T) {
	r := newUIRig(t)
	r.openErr = errors.New("no display")
	done, line := r.start()
	if uiLine.FindStringSubmatch(line) == nil {
		t.Fatalf("stdout line %q", line)
	}
	if want := "modelpick ui: could not open a browser: no display\n"; r.err.String() != want {
		t.Errorf("stderr %q, want %q", r.err.String(), want)
	}
	if len(r.opened) != 1 || r.opened[0] != line[len("modelpick ui: "):] {
		t.Errorf("opened %v for line %q", r.opened, line)
	}
	if code := r.stop(done); code != 0 {
		t.Errorf("exit %d; the server must keep running after an open failure", code)
	}
}

func TestUIRemovesFileOnExit(t *testing.T) {
	r := newUIRig(t)
	done, _ := r.start("--no-open")
	if len(r.opened) != 0 {
		t.Errorf("--no-open still opened %v", r.opened)
	}
	if _, err := os.Stat(r.uiFile()); err != nil {
		t.Fatal(err)
	}
	if code := r.stop(done); code != 0 {
		t.Errorf("exit %d", code)
	}
	if _, err := os.Stat(r.uiFile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ui.json after exit: %v", err)
	}
}

func TestUIKeepsFileOfAnotherPid(t *testing.T) {
	r := newUIRig(t)
	done, _ := r.start("--no-open")
	r.writeUIFile(9999, 1, "theirs")
	r.stop(done)
	if got := readUIFile(t, r.uiFile()); got.Pid != 9999 {
		t.Errorf("another instance's ui.json was removed or changed: %+v", got)
	}
}

func TestUIUnknownFlag(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"extra"}, {"--port", "x"}} {
		r := newUIRig(t)
		code := runUIWith(args, &r.out, &r.err, r.d, r.env)
		if code != 1 || !strings.Contains(r.err.String(), "usage:") || r.out.String() != "" {
			t.Errorf("%v: exit %d stdout %q stderr %q", args, code, r.out.String(), r.err.String())
		}
	}
}

func TestUINoHome(t *testing.T) {
	r := newUIRig(t)
	r.d.Home = ""
	code := runUIWith(nil, &r.out, &r.err, r.d, r.env)
	if code != 1 || !strings.Contains(r.err.String(), app.ErrNoHome.Error()) {
		t.Errorf("exit %d stderr %q", code, r.err.String())
	}
}

func TestMainDispatchesUI(t *testing.T) {
	var out, errOut syncBuf
	if code := Main([]string{"ui", "--bogus"}, &out, &errOut, app.Deps{}); code != 1 || !strings.Contains(errOut.String(), "modelpick ui") {
		t.Errorf("exit %d stderr %q", code, errOut.String())
	}
	if !strings.Contains(usageText, "modelpick ui [--port N] [--no-open]") {
		t.Error("usage text does not list ui")
	}
}
