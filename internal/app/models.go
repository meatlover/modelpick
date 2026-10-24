package app

import (
	"context"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// AccountModels lists one account's models. A failure comes back already
// rewritten as advice (UsageError), with no models.
func AccountModels(e *Env, name string) ([]adapter.Model, error) {
	acct, a, err := e.AdapterFor(name)
	if err != nil {
		return nil, UsageError(acct.Vendor, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ms, err := a.Models(ctx, acct)
	if err != nil {
		return nil, UsageError(acct.Vendor, err)
	}
	return ms, nil
}
