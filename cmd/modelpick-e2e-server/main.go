// Command modelpick-e2e-server serves the live back end (internal/web/live)
// over a fixture config in a given directory, with apptest fakes for every
// vendor, for the browser tests in e2e/. It is a test tool: no release or
// install path builds it, and it never reads the real config, cache or a
// vendor.
package main

import (
	"bufio"
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
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/web"
	"github.com/meatlover/modelpick/internal/web/live"
)

func main() {
	dir := flag.String("dir", "", "an empty directory for config.json and the cache")
	flag.Parse()
	if err := run(*dir, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "modelpick-e2e-server:", err)
		os.Exit(1)
	}
}

// setup writes the fixture config and readings into dir, which must be
// empty, and returns the deps on the real clock and the fakes by vendor.
func setup(dir string, now time.Time) (app.Deps, map[string]*apptest.Fake, error) {
	if dir == "" {
		return app.Deps{}, nil, errors.New("-dir is required")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return app.Deps{}, nil, fmt.Errorf("-dir %s must be an existing empty directory", dir)
	}
	fakes := fixtureFakes(now)
	list := make([]*apptest.Fake, 0, len(fakes))
	for _, f := range fakes {
		list = append(list, f)
	}
	d, err := apptest.NewDepsDir(dir, fixtureConfig, list...)
	if err != nil {
		return app.Deps{}, nil, err
	}
	d.Now = time.Now
	return d, fakes, writeReadings(d, now)
}

// run serves until stdin ends or SIGTERM/SIGINT, answering stdin commands.
func run(dir string, stdin io.Reader, stdout io.Writer) error {
	d, fakes, err := setup(dir, time.Now())
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	token, err := web.NewToken()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dir, "config.json")
	backend := live.New(d, cfgPath)
	srv := &http.Server{Handler: web.NewServer(backend, port, token), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "modelpick-e2e-server:", err)
		}
	}()
	line, err := json.Marshal(struct {
		URL    string `json:"url"`
		Config string `json:"config"`
	}{fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token), cfgPath})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(line))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		readCommands(stdin, stdout, d, fakes)
		close(done)
	}()
	select {
	case <-stop:
	case <-done:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx) // a slow client does not delay exit
	backend.Close()
	return nil
}

// readCommands answers each stdin line with "ok" or "error <why>" once it
// has been applied, until EOF.
func readCommands(stdin io.Reader, stdout io.Writer, d app.Deps, fakes map[string]*apptest.Fake) {
	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if err := apply(f, d, fakes); err != nil {
			fmt.Fprintln(stdout, "error "+err.Error())
			continue
		}
		fmt.Fprintln(stdout, "ok")
	}
}

// apply runs one command against the fakes. Accounts are looked up in the
// config as it is on disk now, so one the page added is found too.
func apply(f []string, d app.Deps, fakes map[string]*apptest.Fake) error {
	switch {
	case f[0] == "fail" && len(f) >= 3:
		fake, err := fakeFor(f[1], d, fakes)
		if err != nil {
			return err
		}
		fake.SetErr(f[1], errors.New(strings.Join(f[2:], " ")))
		return nil
	case f[0] == "clear" && len(f) == 2:
		fake, err := fakeFor(f[1], d, fakes)
		if err != nil {
			return err
		}
		fake.SetErr(f[1], nil)
		return nil
	case f[0] == "tool" && len(f) == 3 && (f[2] == "missing" || f[2] == "present"):
		return setTool(f[1], f[2] == "missing", d, fakes)
	}
	return fmt.Errorf("unknown command %q (want fail <account> <message…>, clear <account>, tool <vendor> missing|present)", strings.Join(f, " "))
}

// fakeFor is the fake serving account's vendor.
func fakeFor(account string, d app.Deps, fakes map[string]*apptest.Fake) (*apptest.Fake, error) {
	cfg, err := app.LoadConfig(d)
	if err != nil {
		return nil, err
	}
	acct, ok := cfg.Accounts[account]
	if !ok {
		return nil, fmt.Errorf("no account %q in the config", account)
	}
	fake, ok := fakes[acct.Vendor]
	if !ok {
		return nil, fmt.Errorf("no fake for vendor %q", acct.Vendor)
	}
	return fake, nil
}

// setTool makes vendor's tool missing (a Check error, no model list) or
// present again for every account of vendor in the config.
func setTool(vendor string, missing bool, d app.Deps, fakes map[string]*apptest.Fake) error {
	fake, ok := fakes[vendor]
	if !ok {
		return fmt.Errorf("unknown vendor %q", vendor)
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		return err
	}
	var names []string
	for n, a := range cfg.Accounts {
		if a.Vendor == vendor {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if missing {
			fake.SetChecks(n, []adapter.Finding{{Severity: adapter.SeverityError, Message: vendorTool[vendor] + " not found on PATH"}})
			fake.SetModels(n, nil)
			continue
		}
		fake.SetChecks(n, nil)
		fake.SetModels(n, vendorModels[vendor])
	}
	return nil
}
