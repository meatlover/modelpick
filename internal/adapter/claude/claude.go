// Package claude reads Claude Code subscription usage through the
// undocumented OAuth usage endpoint, using the login Claude Code already
// keeps. It never refreshes the token and never writes a vendor file.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	usagePath      = "/api/oauth/usage"
	betaHeader     = "oauth-2025-04-20"
	// fallbackVersion is sent when `claude --version` cannot be read.
	fallbackVersion = "2.1.0"
	versionTimeout  = 5 * time.Second
	maxBody         = 1 << 20
	expiredMessage  = "token expired — open Claude Code with this account to refresh it"
)

// Options configures an Adapter. Zero fields take the real implementation.
type Options struct {
	BaseURL  string // default "https://api.anthropic.com"
	HTTP     *http.Client
	Home     string
	Now      func() time.Time
	Keychain func(ctx context.Context, service string) (string, bool, error)
	Run      platform.Runner
	LookPath platform.LookPathFunc
}

// Adapter implements adapter.Adapter for Claude.
type Adapter struct {
	o Options

	uaOnce sync.Once
	ua     string
}

// New returns a Claude adapter, filling unset Options with real defaults.
func New(o Options) *Adapter {
	if o.BaseURL == "" {
		o.BaseURL = defaultBaseURL
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if o.Home == "" {
		o.Home, _ = os.UserHomeDir()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Keychain == nil {
		o.Keychain = platform.KeychainPassword
	}
	if o.Run == nil {
		o.Run = platform.ExecRunner
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	return &Adapter{o: o}
}

// Vendor returns "claude".
func (a *Adapter) Vendor() string { return "claude" }

// userAgent is "claude-cli/<v> (external, cli)", where <v> is the first field
// of `claude --version`, read once per adapter.
func (a *Adapter) userAgent(ctx context.Context) string {
	a.uaOnce.Do(func() {
		v := fallbackVersion
		vctx, cancel := context.WithTimeout(ctx, versionTimeout)
		defer cancel()
		if out, err := a.o.Run(vctx, "claude", "--version"); err == nil {
			if f := strings.Fields(string(out)); len(f) > 0 {
				v = f[0]
			}
		}
		a.ua = "claude-cli/" + v + " (external, cli)"
	})
	return a.ua
}

// Usage makes exactly one GET to the usage endpoint, unless the credential is
// missing, unreadable or expired, in which case it makes none.
func (a *Adapter) Usage(ctx context.Context, acct adapter.Account) (adapter.Reading, error) {
	cred, st := a.load(ctx, acct)
	switch {
	case st.Err != nil:
		return adapter.Reading{}, fmt.Errorf("claude: credential (%s): %w", st.Source, st.Err)
	case !st.Present:
		return adapter.Reading{}, fmt.Errorf("claude: no credential: %s", st.Source)
	case st.Expired:
		return adapter.Reading{}, adapter.ErrTokenExpired
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.o.BaseURL+usagePath, nil)
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("claude: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cred.token)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.userAgent(ctx))

	resp, err := a.o.HTTP.Do(req)
	if err != nil {
		// *url.Error names the method and URL only; neither holds the token.
		return adapter.Reading{}, fmt.Errorf("claude: usage request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return adapter.Reading{}, fmt.Errorf("claude: %w", adapter.ErrUnauthorized)
	case http.StatusTooManyRequests:
		return adapter.Reading{}, &adapter.HTTPError{Status: resp.StatusCode, RetryAfter: adapter.ParseRetryAfter(resp.Header.Get("Retry-After"))}
	default:
		return adapter.Reading{}, &adapter.HTTPError{Status: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return adapter.Reading{}, fmt.Errorf("claude: read usage response: %w", err)
	}
	r, err := parseUsage(body)
	if err != nil {
		return adapter.Reading{}, err
	}
	r.TakenAt = a.o.Now()
	return r, nil
}

type usageJSON struct {
	Limits   []limitJSON `json:"limits"`
	FiveHour *namedJSON  `json:"five_hour"`
	SevenDay *namedJSON  `json:"seven_day"`
}

type limitJSON struct {
	Kind     string   `json:"kind"`
	Group    string   `json:"group"`
	Percent  *float64 `json:"percent"`
	ResetsAt *string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			ID          *string `json:"id"`
			DisplayName *string `json:"display_name"`
		} `json:"model"`
		Surface *string `json:"surface"`
	} `json:"scope"`
}

type namedJSON struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

const (
	fiveHours = 5 * time.Hour
	sevenDays = 7 * 24 * time.Hour
)

var groupLength = map[string]time.Duration{"session": fiveHours, "weekly": sevenDays}

func shapeErr(detail string) error {
	return fmt.Errorf("claude: %w: %s", adapter.ErrShape, detail)
}

// parseUsage reads limits[], falling back to five_hour/seven_day when limits
// is absent or empty. Error messages never quote the body.
func parseUsage(body []byte) (adapter.Reading, error) {
	var u usageJSON
	if err := json.Unmarshal(body, &u); err != nil {
		return adapter.Reading{}, shapeErr("response is not the expected JSON object")
	}
	var r adapter.Reading
	if len(u.Limits) > 0 {
		for i, l := range u.Limits {
			if l.Scope != nil && l.Scope.Surface != nil && *l.Scope.Surface != "claude_code" {
				continue // a window for another surface (chat, cowork, …) does not limit Claude Code
			}
			if l.Percent == nil {
				return adapter.Reading{}, shapeErr(fmt.Sprintf("limits[%d] has no percent", i))
			}
			w := adapter.Window{Label: l.Kind, UsedPct: *l.Percent}
			if w.Label == "" {
				w.Label = l.Group
			}
			if n, ok := groupLength[l.Group]; ok {
				w.Length = n
			} else {
				r.Notes = append(r.Notes, fmt.Sprintf("unrecognised window group %q", l.Group))
			}
			w.ResetsAt = resetTime(l.ResetsAt, &r)
			if l.Scope != nil && l.Scope.Model != nil {
				w.ScopeModelID = deref(l.Scope.Model.ID)
				w.ScopeModelName = deref(l.Scope.Model.DisplayName)
			}
			r.Windows = append(r.Windows, w)
		}
		return r, nil
	}

	for _, n := range []struct {
		label  string
		length time.Duration
		v      *namedJSON
	}{{"five_hour", fiveHours, u.FiveHour}, {"seven_day", sevenDays, u.SevenDay}} {
		if n.v == nil || n.v.Utilization == nil {
			continue
		}
		r.Windows = append(r.Windows, adapter.Window{
			Label: n.label, Length: n.length, UsedPct: *n.v.Utilization, ResetsAt: resetTime(n.v.ResetsAt, &r),
		})
	}
	if len(r.Windows) == 0 {
		return adapter.Reading{}, shapeErr("no limits, five_hour or seven_day")
	}
	return r, nil
}

// resetTime parses an RFC 3339 timestamp (fractional seconds allowed). Null
// is zero (unknown); an unparseable value is zero plus a note.
func resetTime(s *string, r *adapter.Reading) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		r.Notes = append(r.Notes, "unparseable resets_at; reset time unknown")
		return time.Time{}
	}
	return t.UTC()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Check is doctor's view of the account: the claude binary, the credential
// and its expiry. It makes no network call.
func (a *Adapter) Check(_ context.Context, acct adapter.Account) []adapter.Finding {
	var out []adapter.Finding
	add := func(sev adapter.Severity, msg string) {
		out = append(out, adapter.Finding{Severity: sev, Account: acct.Name, Message: msg})
	}
	if _, err := a.o.LookPath("claude"); err != nil {
		add(adapter.SeverityError, "claude not found on PATH")
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
