// Package usage keeps one state file per account and enforces the call floor:
// at most one vendor usage call per account per Floor, across processes.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

// DefaultCallTimeout bounds one usage call when Store.CallTimeout is zero.
const DefaultCallTimeout = 10 * time.Second

// Result is the outcome of Get. Reading is the freshest reading available
// (possibly cached, possibly old); Err is set whenever Reading is nil, and
// also when a call failed but an older reading is returned.
type Result struct {
	Reading  *adapter.Reading
	Err      error
	Warnings []string
}

// Store reads and writes per-account usage state under Dir.
type Store struct {
	Dir         string
	Floor       time.Duration
	Now         func() time.Time
	CallTimeout time.Duration // 10s
}

// state is the on-disk record <Dir>/<account>.json. It never holds credentials.
type state struct {
	LastGood    *adapter.Reading `json:"last_good"`
	LastAttempt time.Time        `json:"last_attempt"`
	LastError   string           `json:"last_error"`
	LastErrorAt time.Time        `json:"last_error_at"`
	RetryUntil  time.Time        `json:"retry_until"`
}

// Age is how old a reading taken at taken is at now; never negative.
func Age(taken, now time.Time) time.Duration {
	if d := now.Sub(taken); d > 0 {
		return d
	}
	return 0
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) statePath(name string) string { return filepath.Join(s.Dir, name+".json") }
func (s *Store) lockPath(name string) string  { return filepath.Join(s.Dir, name+".lock") }

func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid account name %q for a cache file", name)
	}
	return nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func minutesOld(taken, now time.Time) int { return int(Age(taken, now) / time.Minute) }

// Get returns the account's usage, calling the vendor at most once per Floor
// (§6.2). The account's lock is held from before the state is read until Get
// returns, so concurrent callers wait and then answer from the same attempt.
func (s *Store) Get(ctx context.Context, a adapter.Adapter, acct adapter.Account, refresh bool) (res Result) {
	name := acct.Name
	if err := checkName(name); err != nil {
		return Result{Err: err}
	}
	var cacheErr error // first lock or write failure; reported once
	if unlock, err := platform.LockFile(s.lockPath(name)); err != nil {
		cacheErr = err
	} else {
		defer unlock()
	}
	reportCache := true
	defer func() {
		if cacheErr != nil && reportCache {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: cache unavailable: %v", name, cacheErr))
		}
	}()

	st := s.read(name) // missing or corrupt = empty
	now := s.now()
	res.Reading = st.LastGood

	if a.Credential(acct).Expired {
		if st.LastGood == nil {
			res.Err = adapter.ErrTokenExpired
		} else {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: token expired; using reading from %s (%d min old)",
				name, stamp(st.LastGood.TakenAt), minutesOld(st.LastGood.TakenAt, now)))
		}
		return res
	}

	if now.Before(st.RetryUntil) {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: rate-limited until %s", name, stamp(st.RetryUntil)))
		if st.LastGood == nil {
			res.Err = fmt.Errorf("no usage reading yet; rate-limited until %s%s", stamp(st.RetryUntil), failedClause(st))
		}
		return res
	}

	if elapsed := now.Sub(st.LastAttempt); !refresh && elapsed >= 0 && elapsed < s.Floor {
		switch {
		case st.LastGood == nil && st.LastError != "":
			res.Err = fmt.Errorf("no usage reading; last call at %sZ failed: %s; next call allowed at %s",
				st.LastAttempt.UTC().Format("15:04:05"), st.LastError, stamp(st.LastAttempt.Add(s.Floor)))
		case st.LastGood == nil:
			res.Err = fmt.Errorf("no usage reading yet; next call allowed at %s", stamp(st.LastAttempt.Add(s.Floor)))
		case Age(st.LastGood.TakenAt, now) > s.Floor:
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: using reading from %s (%d min old)%s",
				name, stamp(st.LastGood.TakenAt), minutesOld(st.LastGood.TakenAt, now), failedClause(st)))
		}
		return res
	}

	// Call. The attempt is recorded first: success and failure both count.
	// Without the lock or a persisted attempt the floor cannot hold, so it
	// fails closed: no call (Ruling 27).
	if cacheErr == nil {
		st.LastAttempt = now
		cacheErr = s.write(name, st)
	}
	if cacheErr != nil {
		reportCache = false
		return cacheUnavailable(name, st.LastGood, cacheErr, now)
	}
	r, callErr := s.call(ctx, a, acct)
	if callErr == nil {
		if r.TakenAt.IsZero() {
			r.TakenAt = now
		}
		st.LastGood = &r
		st.LastError, st.LastErrorAt = "", time.Time{}
	} else {
		var he *adapter.HTTPError
		if errors.As(callErr, &he) && he.RetryAfter > 0 {
			st.RetryUntil = now.Add(he.RetryAfter)
		}
		st.LastError, st.LastErrorAt = callErr.Error(), now
	}
	cacheErr = s.write(name, st)
	res.Reading, res.Err = st.LastGood, callErr
	if res.Reading == nil && cacheErr != nil {
		res.Err = fmt.Errorf("%w; cache unavailable: %w", callErr, cacheErr)
	}
	return res
}

// cacheUnavailable is the fail-closed result when no call may be made
// because the cache cannot hold the floor: the stored reading with a
// warning, or an error naming the cache.
func cacheUnavailable(name string, stored *adapter.Reading, err error, now time.Time) Result {
	if stored == nil {
		return Result{Err: fmt.Errorf("cache unavailable: %w; no usage call made", err)}
	}
	return Result{Reading: stored, Warnings: []string{fmt.Sprintf("%s: cache unavailable: %v; using stored reading from %s (%d min old)",
		name, err, stamp(stored.TakenAt), minutesOld(stored.TakenAt, now))}}
}

// failedClause is "; last call at HH:MM:SSZ failed: <err>", or "; last call
// at HH:MM:SSZ" when the last call recorded no error, or "" with no attempt.
func failedClause(st state) string {
	if st.LastAttempt.IsZero() {
		return ""
	}
	c := "; last call at " + st.LastAttempt.UTC().Format("15:04:05") + "Z"
	if st.LastError != "" {
		c += " failed: " + st.LastError
	}
	return c
}

func (s *Store) call(ctx context.Context, a adapter.Adapter, acct adapter.Account) (adapter.Reading, error) {
	timeout := s.CallTimeout
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return a.Usage(ctx, acct)
}

// Peek returns the account's last good reading without waiting for the lock
// and without any network call. No state file is (nil, nil).
func (s *Store) Peek(acct adapter.Account) (*adapter.Reading, error) {
	if err := checkName(acct.Name); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(s.statePath(acct.Name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", s.statePath(acct.Name), err)
	}
	return st.LastGood, nil
}

func (s *Store) read(name string) state {
	var st state
	b, err := os.ReadFile(s.statePath(name))
	if err != nil {
		return state{}
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return state{}
	}
	return st
}

// write replaces the state file atomically: temp file (0600) in Dir, then rename.
func (s *Store) write(name string, st state) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, name+".json.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.statePath(name)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
