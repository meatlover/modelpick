package core

import (
	"bytes"
	"encoding/json"

	"github.com/meatlover/modelpick/internal/adapter"
)

// Status is the outcome of a pick; it determines the exit code.
type Status string

// Status values.
const (
	StatusOK           Status = "ok"
	StatusOKWithErrors Status = "ok_with_errors"
	StatusFailed       Status = "failed"
)

// Skip records a choice that was passed over and why.
type Skip struct {
	Account string `json:"account"`
	Model   string `json:"model"`
	Why     string `json:"why"`
}

// RunError is a runtime failure that happened during this pick.
type RunError struct {
	Account string `json:"account"`
	Error   string `json:"error"`
}

// LBPick says which load balancing group member was picked: the group's
// position number in the profile and the member's label ("2b").
type LBPick struct {
	Algorithm string `json:"algorithm"`
	Choice    int    `json:"choice"`
	Member    string `json:"member"`
}

// Result is the output of pick (spec §7).
type Result struct {
	Status     Status           `json:"status"`
	Profile    string           `json:"profile"`
	Vendor     *string          `json:"vendor"`
	Account    *string          `json:"account"`
	Model      *string          `json:"model"`
	Thinking   *string          `json:"thinking"`
	AccountRef *adapter.Account `json:"account_ref"`
	// LoadBalance: nil unless the picked choice is a group member.
	LoadBalance *LBPick    `json:"load_balance"`
	Reason      *string    `json:"reason"`
	Skipped     []Skip     `json:"skipped"`
	Errors      []RunError `json:"errors"`
	Warnings    []string   `json:"warnings"`
}

// ExitCode maps Status to the process exit code: ok 0, failed 1,
// ok_with_errors 2.
func (r Result) ExitCode() int {
	switch r.Status {
	case StatusOK:
		return 0
	case StatusOKWithErrors:
		return 2
	}
	return 1
}

// Failed is the result for a failure before any choice was considered, such
// as a config error or an unknown profile.
func Failed(profile, reason string) Result {
	return Result{Status: StatusFailed, Profile: profile, Reason: &reason}
}

// JSON renders r in the fixed layout of spec §7.1: one top-level field per
// line with a 2-space indent, objects inline with ", " and ": " separators,
// each list element on its own line, empty lists as [], and a trailing
// newline. HTML characters are not escaped (">=" stays readable).
func (r Result) JSON() []byte {
	fields := []struct {
		key   string
		value []byte
	}{
		{"status", inline(r.Status)},
		{"profile", inline(r.Profile)},
		{"vendor", inline(r.Vendor)},
		{"account", inline(r.Account)},
		{"model", inline(r.Model)},
		{"thinking", inline(r.Thinking)},
		{"account_ref", inline(r.AccountRef)},
		{"load_balance", inline(r.LoadBalance)},
		{"reason", inline(r.Reason)},
		{"skipped", list(r.Skipped)},
		{"errors", list(r.Errors)},
		{"warnings", list(r.Warnings)},
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, f := range fields {
		b.WriteString("  ")
		b.Write(inline(f.key))
		b.WriteString(": ")
		b.Write(f.value)
		if i < len(fields)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// list renders items as [] or as one inline element per line.
func list[T any](items []T) []byte {
	if len(items) == 0 {
		return []byte("[]")
	}
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, it := range items {
		b.WriteString("    ")
		b.Write(inline(it))
		if i < len(items)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  ]")
	return b.Bytes()
}

// inline encodes v on one line with a space after every ',' and ':' that is
// outside a string.
func inline(v any) []byte {
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic("core: result field not encodable: " + err.Error())
	}
	compact := bytes.TrimSuffix(raw.Bytes(), []byte("\n"))
	out := make([]byte, 0, len(compact)+8)
	inString, escaped := false, false
	for _, c := range compact {
		out = append(out, c)
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == ',' || c == ':'):
			out = append(out, ' ')
		}
	}
	return out
}
