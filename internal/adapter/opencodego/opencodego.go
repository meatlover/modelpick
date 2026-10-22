// Package opencodego reads OpenCode Go usage through the zen usage endpoint,
// using a key from a key file the owner already keeps. It never writes a
// vendor file.
package opencodego

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

const (
	defaultBaseURL   = "https://opencode.ai"
	defaultUserAgent = "modelpick/0.1.0-dev"
	usagePath        = "/zen/go/v1/usage"
	maxBody          = 1 << 20
)

// Options configures an Adapter. Zero fields take the real implementation.
type Options struct {
	BaseURL   string // default "https://opencode.ai"
	HTTP      *http.Client
	Now       func() time.Time
	Run       platform.Runner
	LookPath  platform.LookPathFunc
	UserAgent string // default "modelpick/0.1.0-dev"
}

// Adapter implements adapter.Adapter for OpenCode Go.
type Adapter struct{ o Options }

// New returns an OpenCode Go adapter, filling unset Options with real defaults.
func New(o Options) *Adapter {
	if o.BaseURL == "" {
		o.BaseURL = defaultBaseURL
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Run == nil {
		o.Run = platform.ExecRunner
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.UserAgent == "" {
		o.UserAgent = defaultUserAgent
	}
	return &Adapter{o: o}
}

// Vendor returns "opencode-go".
func (a *Adapter) Vendor() string { return "opencode-go" }

// credential is the in-memory key. It never leaves this package except in the
// Authorization header of the usage request.
type credential struct{ key string }

// load reads the key line. The returned status never holds the key.
func (a *Adapter) load(acct adapter.Account) (credential, adapter.CredStatus) {
	if acct.KeyFile == "" {
		return credential{}, adapter.CredStatus{Source: "no key_file configured"}
	}
	source := fmt.Sprintf("key file %s line %d", acct.KeyFile, acct.KeyLine)
	key, found, err := KeyAt(acct.KeyFile, acct.KeyLine)
	if err != nil {
		return credential{}, adapter.CredStatus{Source: source, Err: err}
	}
	if !found {
		return credential{}, adapter.CredStatus{Source: source}
	}
	return credential{key: key}, adapter.CredStatus{Present: true, Source: source}
}

// Credential reports whether the key line exists. A key never expires locally.
func (a *Adapter) Credential(acct adapter.Account) adapter.CredStatus {
	_, st := a.load(acct)
	return st
}

// Usage makes exactly one GET to the usage endpoint, unless the key is missing
// or unreadable, in which case it makes none.
func (a *Adapter) Usage(ctx context.Context, acct adapter.Account) (adapter.Reading, error) {
	cred, st := a.load(acct)
	switch {
	case st.Err != nil:
		return adapter.Reading{}, fmt.Errorf("opencode-go: credential (%s): %w", st.Source, st.Err)
	case !st.Present:
		return adapter.Reading{}, fmt.Errorf("opencode-go: no credential: %s", st.Source)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.o.BaseURL+usagePath, nil)
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("opencode-go: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cred.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.o.UserAgent)

	resp, err := a.o.HTTP.Do(req)
	if err != nil {
		// *url.Error names the method and URL only; neither holds a secret.
		return adapter.Reading{}, fmt.Errorf("opencode-go: usage request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return adapter.Reading{}, fmt.Errorf("opencode-go: %w", adapter.ErrUnauthorized)
	case http.StatusTooManyRequests:
		return adapter.Reading{}, &adapter.HTTPError{Status: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	default:
		return adapter.Reading{}, &adapter.HTTPError{Status: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("opencode-go: read usage response: %w", err)
	}
	r, err := parseUsage(body)
	if err != nil {
		return adapter.Reading{}, err
	}
	r.TakenAt = a.o.Now()
	return r, nil
}

// retryAfter reads a delay-seconds Retry-After; anything else is 0 (unknown).
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

type usageJSON struct {
	Usage *struct {
		Rolling *windowJSON `json:"rolling"`
		Weekly  *windowJSON `json:"weekly"`
		Monthly *windowJSON `json:"monthly"`
	} `json:"usage"`
}

type windowJSON struct {
	Status   string   `json:"status"`
	Percent  *float64 `json:"percent"`
	ResetsAt string   `json:"resetsAt"`
}

func shapeErr(detail string) error {
	return fmt.Errorf("opencode-go: %w: %s", adapter.ErrShape, detail)
}

// parseUsage reads the three windows. Error messages never quote the body.
func parseUsage(body []byte) (adapter.Reading, error) {
	var u usageJSON
	if err := json.Unmarshal(body, &u); err != nil {
		return adapter.Reading{}, shapeErr("response is not the expected JSON object")
	}
	if u.Usage == nil {
		return adapter.Reading{}, shapeErr("no usage object")
	}
	var r adapter.Reading
	for _, n := range []struct {
		label  string
		length time.Duration
		w      *windowJSON
	}{
		{"rolling", 5 * time.Hour, u.Usage.Rolling},
		{"weekly", 7 * 24 * time.Hour, u.Usage.Weekly},
		{"monthly", 30 * 24 * time.Hour, u.Usage.Monthly},
	} {
		if n.w == nil {
			continue
		}
		win := adapter.Window{Label: n.label, Length: n.length}
		switch n.w.Status {
		case "ok":
		case "rate-limited":
			win.UsedPct = 100
		default:
			r.Notes = append(r.Notes, fmt.Sprintf("unrecognised status %q on %s", n.w.Status, n.label))
		}
		if n.w.Status != "rate-limited" {
			if n.w.Percent == nil {
				return adapter.Reading{}, shapeErr(n.label + " has no percent")
			}
			win.UsedPct = *n.w.Percent
		}
		if n.w.ResetsAt != "" {
			t, err := time.Parse(time.RFC3339Nano, n.w.ResetsAt)
			if err != nil {
				return adapter.Reading{}, shapeErr(n.label + " resetsAt is not an ISO timestamp")
			}
			win.ResetsAt = t.UTC()
		}
		r.Windows = append(r.Windows, win)
	}
	if len(r.Windows) == 0 {
		return adapter.Reading{}, shapeErr("usage has no rolling, weekly or monthly window")
	}
	return r, nil
}

// Models runs `opencode models opencode-go` and returns one model per
// non-empty output line. OpenCode Go models take no thinking level.
func (a *Adapter) Models(ctx context.Context, _ adapter.Account) ([]adapter.Model, error) {
	out, err := a.o.Run(ctx, "opencode", "models", "opencode-go")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("opencode-go: opencode models exited with status %d", ee.ExitCode())
		}
		return nil, fmt.Errorf("opencode-go: run opencode models: %w", err)
	}
	models := []adapter.Model{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		if id := strings.TrimSpace(string(line)); id != "" {
			models = append(models, adapter.Model{ID: id})
		}
	}
	return models, nil
}

// Check is doctor's view of the account: the opencode binary and the key
// line. It makes no network call.
func (a *Adapter) Check(_ context.Context, acct adapter.Account) []adapter.Finding {
	var out []adapter.Finding
	add := func(sev adapter.Severity, msg string) {
		out = append(out, adapter.Finding{Severity: sev, Account: acct.Name, Message: msg})
	}
	if _, err := a.o.LookPath("opencode"); err != nil {
		add(adapter.SeverityError, "opencode not found on PATH")
	}
	st := a.Credential(acct)
	switch {
	case st.Err != nil:
		add(adapter.SeverityError, fmt.Sprintf("credential unreadable (%s): %v", st.Source, st.Err))
	case !st.Present:
		add(adapter.SeverityError, "no credential: "+st.Source)
	}
	return out
}

var _ adapter.Adapter = (*Adapter)(nil)
