package app

import (
	"errors"

	"github.com/meatlover/modelpick/internal/adapter"
)

// toolNames are the vendors' CLI names as users know them, for advice in
// error messages.
var toolNames = map[string]string{
	"claude":      "Claude Code",
	"codex":       "Codex",
	"opencode-go": "OpenCode",
}

// UsageError rewrites err as what the user should do about it, keeping err
// matchable with errors.Is.
func UsageError(vendor string, err error) error {
	tool, ok := toolNames[vendor]
	if !ok {
		tool = vendor
	}
	switch {
	case errors.Is(err, adapter.ErrUnauthorized):
		return &adviceError{"usage endpoint returned HTTP 401; log in to " + tool + " with this account", err}
	case errors.Is(err, adapter.ErrTokenExpired):
		return &adviceError{"token expired; open " + tool + " with this account to refresh it", err}
	}
	return err
}

// adviceError replaces an error's text while keeping it as the cause.
type adviceError struct {
	msg   string
	cause error
}

func (e *adviceError) Error() string { return e.msg }
func (e *adviceError) Unwrap() error { return e.cause }
