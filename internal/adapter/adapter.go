// Package adapter defines the vendor-neutral types and the Adapter interface
// that each vendor package implements.
package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Account identifies one configured vendor login.
type Account struct {
	Name      string `json:"-"`
	Vendor    string `json:"vendor"`
	ConfigDir string `json:"config_dir,omitempty"` // claude
	CodexHome string `json:"codex_home,omitempty"` // codex
	KeyFile   string `json:"key_file,omitempty"`   // opencode-go
	KeyLine   int    `json:"key_line,omitempty"`   // opencode-go, 1-based
}

// Window is one usage window as reported by a vendor.
type Window struct {
	Label          string        `json:"label"`  // vendor's name: "session", "weekly", "primary_window", "rolling", …
	Length         time.Duration `json:"length"` // 0 = unknown
	UsedPct        float64       `json:"used_pct"`
	ResetsAt       time.Time     `json:"resets_at"` // zero = unknown
	ScopeModelID   string        `json:"scope_model_id,omitempty"`
	ScopeModelName string        `json:"scope_model_name,omitempty"` // "" and "" = general window
}

// Reading is the result of one Usage call.
type Reading struct {
	TakenAt time.Time `json:"taken_at"`
	Windows []Window  `json:"windows"`
	Notes   []string  `json:"notes,omitempty"` // adapter-level oddities for doctor, e.g. "unrecognised window group \"x\""
}

// CredStatus describes a locally stored credential without exposing it.
type CredStatus struct {
	Present bool
	Expired bool
	Source  string // human description, e.g. "keychain: Claude Code-credentials-6a4e8c1e", never the secret
	Err     error  // non-nil when the source exists but cannot be read/parsed
}

// Severity grades a doctor Finding.
type Severity string

// Severity values.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Finding is one doctor result for an account.
type Finding struct {
	Severity Severity
	Account  string
	Message  string
}

// Model is a model an account can use.
type Model struct {
	ID       string
	Thinking []string // empty = model takes no thinking level
}

// Adapter is implemented once per vendor.
type Adapter interface {
	Vendor() string                                           // "claude" | "codex" | "opencode-go"
	Credential(acct Account) CredStatus                       // local only, no network
	Usage(ctx context.Context, acct Account) (Reading, error) // exactly one HTTP call, no caching
	Models(ctx context.Context, acct Account) ([]Model, error)
	Check(ctx context.Context, acct Account) []Finding // doctor: binary on PATH, credential, expiry
}

// Sentinel errors returned (possibly wrapped) by adapters.
var (
	ErrTokenExpired = errors.New("token expired")
	ErrUnauthorized = errors.New("unauthorized")
	ErrShape        = errors.New("unrecognised response shape")
)

// HTTPError reports a non-success status from a usage endpoint.
type HTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("usage endpoint returned HTTP %d", e.Status)
}
