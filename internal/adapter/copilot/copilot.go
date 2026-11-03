// Package copilot reads GitHub Copilot premium-request quota through the
// internal copilot_internal/user endpoint, using the GitHub token OpenCode
// already keeps in its auth.json. It never writes a vendor file.
package copilot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

const (
	defaultBaseURL   = "https://api.github.com"
	defaultUserAgent = "GitHubCopilotChat/0.26.7"
	usagePath        = "/copilot_internal/user"
	maxBody          = 1 << 20
)

// Options configures an Adapter. Zero fields take the real implementation.
type Options struct {
	BaseURL   string // default "https://api.github.com"
	HTTP      *http.Client
	Now       func() time.Time
	AuthFile  string // OpenCode's auth.json
	Run       platform.Runner
	LookPath  platform.LookPathFunc
	UserAgent string // default "GitHubCopilotChat/0.26.7"
}

// Adapter implements adapter.Adapter for GitHub Copilot.
type Adapter struct{ o Options }

// New returns a Copilot adapter, filling unset Options with real defaults.
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

// Vendor returns "copilot".
func (a *Adapter) Vendor() string { return "copilot" }

// Usage makes exactly one GET to the usage endpoint, unless the credential is
// missing or unreadable, in which case it makes none.
func (a *Adapter) Usage(ctx context.Context, acct adapter.Account) (adapter.Reading, error) {
	cred, st := a.load(acct)
	switch {
	case st.Err != nil:
		return adapter.Reading{}, fmt.Errorf("copilot: credential (%s): %w", st.Source, st.Err)
	case !st.Present:
		return adapter.Reading{}, fmt.Errorf("copilot: no credential: %s", st.Source)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.o.BaseURL+usagePath, nil)
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("copilot: build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+cred.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.o.UserAgent)

	resp, err := a.o.HTTP.Do(req)
	if err != nil {
		// *url.Error names the method and URL only; neither holds a secret.
		return adapter.Reading{}, fmt.Errorf("copilot: usage request: %w", err)
	}
	defer resp.Body.Close()

	if err := statusError(resp); err != nil {
		return adapter.Reading{}, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("copilot: read usage response: %w", err)
	}
	r, err := parseUsage(body)
	if err != nil {
		return adapter.Reading{}, err
	}
	r.TakenAt = a.o.Now()
	return r, nil
}

// statusError maps a non-200 status to an error; nil for 200. 403 and 404 on
// this internal endpoint more likely mean drift than a bad login.
func statusError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("copilot: %w", adapter.ErrUnauthorized)
	case http.StatusTooManyRequests:
		return &adapter.HTTPError{Status: resp.StatusCode, RetryAfter: adapter.ParseRetryAfter(resp.Header.Get("Retry-After"))}
	case http.StatusForbidden, http.StatusNotFound:
		return fmt.Errorf("copilot: HTTP %d: the copilot_internal endpoint may have changed: %w", resp.StatusCode, &adapter.HTTPError{Status: resp.StatusCode})
	default:
		return &adapter.HTTPError{Status: resp.StatusCode}
	}
}

// Check is doctor's view of the account: the opencode binary and the
// credential. It makes no network call.
func (a *Adapter) Check(_ context.Context, acct adapter.Account) []adapter.Finding {
	var out []adapter.Finding
	add := func(sev adapter.Severity, msg string) {
		out = append(out, adapter.Finding{Severity: sev, Account: acct.Name, Message: msg})
	}
	if _, err := a.o.LookPath("opencode"); err != nil {
		add(adapter.SeverityWarning, "opencode not found on PATH — models and launching need it")
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
