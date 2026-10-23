package app

import (
	"errors"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

// StateText derives the STATE column from the store's outcome: an expired
// token or an unexpired Retry-After wins over ok, even on a cached reading.
func StateText(st core.AccountState) string {
	var he *adapter.HTTPError
	switch {
	case st.TokenExpired, errors.Is(st.Err, adapter.ErrTokenExpired):
		return "expired token"
	case !st.RetryUntil.IsZero():
		return "rate-limited until " + st.RetryUntil.UTC().Format(time.RFC3339)
	case st.Err == nil:
		return "ok"
	case errors.As(st.Err, &he) && he.Status == 429:
		return "rate-limited (no retry time given)"
	}
	return "error: " + st.Err.Error()
}
