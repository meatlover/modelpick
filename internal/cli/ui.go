package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/platform"
	"github.com/meatlover/modelpick/internal/web"
	"github.com/meatlover/modelpick/internal/web/live"
)

// uiEnv is what `modelpick ui` takes from the machine; tests replace it.
type uiEnv struct {
	Listen   func(addr string) (net.Listener, error)
	Ping     func(port int, token string) bool // is an instance answering with this token?
	Open     func(url string) error
	Signal   func() (<-chan os.Signal, func()) // the stop request, and its release
	PidAlive func(pid int) bool
	// BinaryReplaced says whether the running executable was replaced on disk
	// since start; nil means never.
	BinaryReplaced func() bool
	Pid            int
}

// uiState is the content of <cache>/ui.json.
type uiState struct {
	Pid   int    `json:"pid"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func realUIEnv() uiEnv {
	return uiEnv{
		Listen: func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		Ping:   pingInstance,
		Open:   platform.OpenBrowser,
		Signal: func() (<-chan os.Signal, func()) {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
			return ch, func() { signal.Stop(ch) }
		},
		PidAlive:       pidAlive,
		BinaryReplaced: binaryReplacedFunc(executablePath, os.Stat),
		Pid:            os.Getpid(),
	}
}

// pidAlive: signal 0 reports whether the process exists; EPERM means it
// exists under another user.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// pingClient talks to 127.0.0.1 only. Proxy nil says so outright: the
// default transport reads HTTP_PROXY, and only Go's loopback bypass would
// keep the ping off a proxy.
func pingClient() *http.Client {
	return &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
}

// pingInstance asks the instance on port whether it takes token, within 1 s.
func pingInstance(port int, token string) bool {
	c := pingClient()
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping?t=%s", port, token))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

func runUI(args []string, stdout, stderr io.Writer, d app.Deps) int {
	return runUIWith(args, stdout, stderr, d, realUIEnv())
}

// runUIWith is `modelpick ui`: serve the page against the real config until
// SIGINT or SIGTERM, or reuse an instance that is already serving.
func runUIWith(args []string, stdout, stderr io.Writer, d app.Deps, env uiEnv) int {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "")
	noOpen := fs.Bool("no-open", false, "")
	if err := fs.Parse(args); err != nil {
		return badUsage(stderr, "ui: "+err.Error())
	}
	if fs.NArg() > 0 {
		return badUsage(stderr, fmt.Sprintf("ui: unexpected argument %q", fs.Arg(0)))
	}
	if d.Home == "" {
		fmt.Fprintf(stderr, "modelpick: %v\n", app.ErrNoHome)
		return 1
	}
	file := filepath.Join(platform.CacheDir(d.Getenv, d.Home), "ui.json")
	prev, havePrev := readUIState(file)
	if havePrev && env.PidAlive(prev.Pid) && env.Ping(prev.Port, prev.Token) {
		announce(stdout, stderr, env, uiURL(prev.Port, prev.Token), *noOpen)
		return 0
	}
	ln, err := listenUI(env, *port, prev.Port)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: ui: %v\n", err)
		return 1
	}
	return serveUI(ln, file, stdout, stderr, d, env, *noOpen)
}

// listenUI listens on loopback: the asked-for port, else the previous run's
// port when it is free (the page keeps its localStorage), else a free one.
func listenUI(env uiEnv, port, prevPort int) (net.Listener, error) {
	if port == 0 && prevPort > 0 {
		if ln, err := env.Listen(fmt.Sprintf("127.0.0.1:%d", prevPort)); err == nil {
			return ln, nil
		}
	}
	return env.Listen(fmt.Sprintf("127.0.0.1:%d", port))
}

func serveUI(ln net.Listener, file string, stdout, stderr io.Writer, d app.Deps, env uiEnv, noOpen bool) int {
	port := ln.Addr().(*net.TCPAddr).Port
	token, err := web.NewToken()
	if err != nil {
		_ = ln.Close()
		fmt.Fprintf(stderr, "modelpick: ui: %v\n", err)
		return 1
	}
	if err := writeUIState(file, uiState{Pid: env.Pid, Port: port, Token: token}); err != nil {
		_ = ln.Close()
		fmt.Fprintf(stderr, "modelpick: ui: write %s: %v\n", file, err)
		return 1
	}
	backend := live.New(d, platform.ConfigPath(d.Getenv, d.Home), live.WithBinaryCheck(env.BinaryReplaced))
	srv := &http.Server{Handler: web.NewServer(backend, port, token), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stderr, "modelpick ui: %v\n", err)
		}
	}()
	stop, release := env.Signal()
	defer release()
	announce(stdout, stderr, env, uiURL(port, token), noOpen)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx) // a slow client does not delay exit
	backend.Close()
	if cur, ok := readUIState(file); ok && cur.Pid == env.Pid {
		_ = os.Remove(file)
	}
	return 0
}

func uiURL(port int, token string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token)
}

// announce prints the URL and opens it unless noOpen; a failed open is a note.
func announce(stdout, stderr io.Writer, env uiEnv, url string, noOpen bool) {
	fmt.Fprintf(stdout, "modelpick ui: %s\n", url)
	if noOpen {
		return
	}
	if err := env.Open(url); err != nil {
		fmt.Fprintf(stderr, "modelpick ui: could not open a browser: %v\n", err)
	}
}

func readUIState(file string) (uiState, bool) {
	var s uiState
	data, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(data, &s) != nil || s.Pid <= 0 || s.Port <= 0 || s.Token == "" {
		return uiState{}, false
	}
	return s, true
}

func writeUIState(file string, s uiState) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(file, append(data, '\n'))
}
