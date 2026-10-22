// Package codex reads Codex (ChatGPT subscription) usage through the
// undocumented wham usage endpoint, using the login the codex CLI already
// keeps. It never refreshes the token and never writes a vendor file.
package codex

import (
	"context"
	"encoding/json"
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
	defaultBaseURL   = "https://chatgpt.com"
	defaultUserAgent = "modelpick/0.1.0-dev"
	usagePath        = "/backend-api/wham/usage"
	maxBody          = 1 << 20
	expiredMessage   = "token expired — open Codex with this account to refresh it"
)

// Options configures an Adapter. Zero fields take the real implementation.
type Options struct {
	BaseURL   string // default "https://chatgpt.com"
	HTTP      *http.Client
	Now       func() time.Time
	LookPath  platform.LookPathFunc
	UserAgent string // default "modelpick/0.1.0-dev"
}

// Adapter implements adapter.Adapter for Codex.
type Adapter struct{ o Options }

// New returns a Codex adapter, filling unset Options with real defaults.
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
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.UserAgent == "" {
		o.UserAgent = defaultUserAgent
	}
	return &Adapter{o: o}
}

// Vendor returns "codex".
func (a *Adapter) Vendor() string { return "codex" }

// Usage makes exactly one GET to the usage endpoint, unless the credential is
// missing, unreadable or expired, in which case it makes none.
func (a *Adapter) Usage(ctx context.Context, acct adapter.Account) (adapter.Reading, error) {
	cred, st := a.load(acct)
	switch {
	case st.Err != nil:
		return adapter.Reading{}, fmt.Errorf("codex: credential (%s): %w", st.Source, st.Err)
	case !st.Present:
		return adapter.Reading{}, fmt.Errorf("codex: no credential: %s", st.Source)
	case st.Expired:
		return adapter.Reading{}, adapter.ErrTokenExpired
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.o.BaseURL+usagePath, nil)
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("codex: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cred.token)
	req.Header.Set("ChatGPT-Account-Id", cred.accountID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.o.UserAgent)

	resp, err := a.o.HTTP.Do(req)
	if err != nil {
		// *url.Error names the method and URL only; neither holds a secret.
		return adapter.Reading{}, fmt.Errorf("codex: usage request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return adapter.Reading{}, fmt.Errorf("codex: %w", adapter.ErrUnauthorized)
	case http.StatusTooManyRequests:
		return adapter.Reading{}, &adapter.HTTPError{Status: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	default:
		return adapter.Reading{}, &adapter.HTTPError{Status: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("codex: read usage response: %w", err)
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
	RateLimit *struct {
		Allowed         *bool       `json:"allowed"`
		LimitReached    bool        `json:"limit_reached"`
		PrimaryWindow   *windowJSON `json:"primary_window"`
		SecondaryWindow *windowJSON `json:"secondary_window"`
	} `json:"rate_limit"`
	AdditionalRateLimits json.RawMessage `json:"additional_rate_limits"`
}

type windowJSON struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds *int64   `json:"limit_window_seconds"`
	ResetAt            *int64   `json:"reset_at"`
}

func shapeErr(detail string) error {
	return fmt.Errorf("codex: %w: %s", adapter.ErrShape, detail)
}

// parseUsage reads rate_limit's two windows. Error messages never quote the body.
func parseUsage(body []byte) (adapter.Reading, error) {
	var u usageJSON
	if err := json.Unmarshal(body, &u); err != nil {
		return adapter.Reading{}, shapeErr("response is not the expected JSON object")
	}
	if u.RateLimit == nil {
		return adapter.Reading{}, shapeErr("no rate_limit object")
	}
	full := u.RateLimit.LimitReached || (u.RateLimit.Allowed != nil && !*u.RateLimit.Allowed)
	var r adapter.Reading
	for _, n := range []struct {
		label string
		w     *windowJSON
	}{{"primary_window", u.RateLimit.PrimaryWindow}, {"secondary_window", u.RateLimit.SecondaryWindow}} {
		if n.w == nil {
			continue
		}
		if n.w.UsedPercent == nil {
			return adapter.Reading{}, shapeErr(n.label + " has no used_percent")
		}
		w := adapter.Window{Label: n.label, UsedPct: *n.w.UsedPercent}
		if full {
			w.UsedPct = 100
		}
		if s := n.w.LimitWindowSeconds; s != nil && *s > 0 {
			w.Length = time.Duration(*s) * time.Second
		}
		if t := n.w.ResetAt; t != nil && *t > 0 {
			w.ResetsAt = time.Unix(*t, 0).UTC()
		}
		r.Windows = append(r.Windows, w)
	}
	if len(u.AdditionalRateLimits) > 0 && string(u.AdditionalRateLimits) != "null" {
		r.Notes = append(r.Notes, "additional_rate_limits present; not parsed in v1")
	}
	return r, nil
}

// Check is doctor's view of the account: the codex binary, the credential
// and its expiry. It makes no network call.
func (a *Adapter) Check(_ context.Context, acct adapter.Account) []adapter.Finding {
	var out []adapter.Finding
	add := func(sev adapter.Severity, msg string) {
		out = append(out, adapter.Finding{Severity: sev, Account: acct.Name, Message: msg})
	}
	if _, err := a.o.LookPath("codex"); err != nil {
		add(adapter.SeverityError, "codex not found on PATH")
	}
	st := a.Credential(acct)
	switch {
	case st.Err != nil:
		add(adapter.SeverityError, fmt.Sprintf("credential unreadable (%s): %v", st.Source, st.Err))
	case !st.Present:
		add(adapter.SeverityError, "no credential: "+st.Source)
	case st.Expired:
		add(adapter.SeverityWarning, expiredMessage)
	}
	return out
}

var _ adapter.Adapter = (*Adapter)(nil)
