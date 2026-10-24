package app

import (
	"context"
	"sort"
	"sync"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/usage"
)

// dueCall is one account that gets a vendor usage call.
type dueCall struct {
	name string
	acct adapter.Account
	a    adapter.Adapter
}

// DoctorAll is Doctor after one usage call per account that has no fresh
// reading, made concurrently through the store with refresh (the explicit
// request Refresh is). A reading younger than the call interval is used as
// is; the store itself still refuses an expired token and an unexpired
// Retry-After. A failed call shows as Doctor's "last usage reading failed"
// warning, because the store records it; a cache that cannot be updated
// shows as its own warning. A ctx that ends cancels the calls, which the
// store does not record.
func DoctorAll(ctx context.Context, cfg *core.Config, d Deps, cache *CheckCache) []Finding {
	if cache == nil {
		cache = &CheckCache{}
	}
	store := NewStore(d, cfg)
	cacheErrs := callDue(ctx, store, dueCalls(cfg, d, cache, store))
	out := Doctor(cfg, d, cache)
	seen := map[findingKey]bool{}
	for _, x := range out {
		seen[x.key()] = true
	}
	for _, n := range sortedKeys(cacheErrs) {
		x := Finding{Severity: sevWarning, Subject: n, Message: "cannot update stored usage: " + cacheErrs[n].Error()}
		if !seen[x.key()] {
			seen[x.key()] = true
			out = append(out, x)
		}
	}
	sortFindings(out)
	return out
}

// dueCalls lists the accounts to call, in name order: those whose adapter
// exists, whose Check reports no error, and whose saved reading is missing
// or at least the call interval old. An unreadable state file is left to
// Doctor, which reports it.
func dueCalls(cfg *core.Config, d Deps, cache *CheckCache, store *usage.Store) []dueCall {
	e := NewEnv(d, cfg, false)
	names := make([]string, 0, len(cfg.Accounts))
	for n := range cfg.Accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	cache.warmChecks(e, names)
	var due []dueCall
	for _, n := range names {
		acct, a, err := e.AdapterFor(n)
		if err != nil {
			continue
		}
		var scratch findings
		if checkFindings(&scratch, n, cache.check(n, acct, a).findings) {
			continue
		}
		s, err := store.Saved(acct)
		if err != nil {
			continue
		}
		if s.Reading != nil && usage.Age(s.Reading.TakenAt, d.Now()) < cfg.Settings.MinCallInterval {
			continue
		}
		due = append(due, dueCall{n, acct, a})
	}
	return due
}

// callDue runs one Get per account at once and waits for all of them. It
// returns the cache failure of each account that had one.
func callDue(ctx context.Context, store *usage.Store, due []dueCall) map[string]error {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs = map[string]error{}
	)
	for _, c := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := store.Get(ctx, c.a, c.acct, true)
			if r.CacheErr != nil {
				mu.Lock()
				errs[c.name] = r.CacheErr
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errs
}

func sortedKeys(m map[string]error) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
