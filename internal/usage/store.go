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
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

// DefaultCallTimeout bounds one usage call when Store.CallTimeout is zero.
const DefaultCallTimeout = 10 * time.Second

// DefaultRenewTimeout bounds one login renewal when Store.RenewTimeout is
// zero. It is separate from, and longer than, the usage-call limit.
const DefaultRenewTimeout = 60 * time.Second

// maxReasonBytes caps the reason a failed renewal is recorded and shown with.
const maxReasonBytes = 200

// Result is the outcome of Get. Reading is the freshest reading available
// (possibly cached, possibly old); Err is set whenever Reading is nil, and
// also when a call failed but an older reading is returned.
type Result struct {
	Reading  *adapter.Reading
	Err      error
	Warnings []string
	// TokenExpired: the local credential is expired and Reading holds no
	// reading taken in this Get (none was renewed and called).
	TokenExpired bool
	// RetryUntil: a server Retry-After that has not elapsed; zero = none.
	RetryUntil time.Time
	// CacheErr is the first failure to take the account's lock or to write
	// its state file; nil when the cache worked. The same text is in Warnings
	// (or Err), so a caller that only prints those can ignore it.
	CacheErr error
}

// Store reads and writes per-account usage state under Dir.
type Store struct {
	Dir         string
	Floor       time.Duration
	Now         func() time.Time
	CallTimeout time.Duration // 10s

	RenewTimeout time.Duration // 0 = DefaultRenewTimeout
	NoRenew      bool          // true = never renew an expired login (the live smoke test)
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
func (s *Store) Get(ctx context.Context, a adapter.Adapter, acct adapter.Account, refresh bool) Result {
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

	st := s.read(name) // missing or corrupt = empty
	now := s.now()
	renewer, canRenew := a.(adapter.Renewer)
	if r, answered := s.withoutCall(a, acct, st, now, refresh, canRenew && !s.NoRenew); answered {
		return withCacheWarning(r, name, cacheErr)
	}

	// Call. The attempt is recorded first: success and failure both count.
	// Without the lock or a persisted attempt the floor cannot hold, so it
	// fails closed: no call (Ruling 27).
	prevAttempt := st.LastAttempt
	if cacheErr == nil {
		st.LastAttempt = now
		cacheErr = s.write(name, st)
	}
	if cacheErr != nil {
		return cacheUnavailable(name, st.LastGood, cacheErr, now)
	}
	if canRenew && !s.NoRenew && a.Credential(acct).Expired {
		if r, answered, writeErr := s.renew(ctx, renewer, a, acct, st, prevAttempt, now); answered {
			return withCacheWarning(r, name, writeErr)
		}
	}
	r, writeErr := s.callAndRecord(ctx, a, acct, st, prevAttempt, now)
	return withCacheWarning(r, name, writeErr)
}

// withCacheWarning adds the "cache unavailable" warning when err is set.
func withCacheWarning(res Result, name string, err error) Result {
	if err != nil {
		res.CacheErr = err
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: cache unavailable: %v", name, err))
	}
	return res
}

// withoutCall answers from the stored state when no call may be made: an
// expired token, an unexpired Retry-After, or the floor. answered is false
// when a call is due. With renewable, an expired token with no reading or a
// reading older than Floor is not answered here: it goes through the same
// Retry-After and floor checks as a live token, and then Get renews it.
func (s *Store) withoutCall(a adapter.Adapter, acct adapter.Account, st state, now time.Time, refresh, renewable bool) (res Result, answered bool) {
	name := acct.Name
	res.Reading = st.LastGood

	expired := a.Credential(acct).Expired
	res.TokenExpired = expired
	if expired && !(renewable && (st.LastGood == nil || Age(st.LastGood.TakenAt, now) > s.Floor)) {
		if st.LastGood == nil {
			res.Err = adapter.ErrTokenExpired
		} else {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: token expired; using reading from %s (%d min old)",
				name, stamp(st.LastGood.TakenAt), minutesOld(st.LastGood.TakenAt, now)))
		}
		return res, true
	}

	if now.Before(st.RetryUntil) {
		res.RetryUntil = st.RetryUntil
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: rate-limited until %s", name, stamp(st.RetryUntil)))
		if st.LastGood == nil {
			res.Err = fmt.Errorf("no usage reading yet; rate-limited until %s%s", stamp(st.RetryUntil), failedClause(st))
		}
		return res, true
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
		return res, true
	}
	return Result{}, false
}

// renew asks the adapter to renew the expired login of a call that is due
// and whose attempt is already recorded. answered is false when the login is
// renewed and the usage call should follow; it and the renewal are one
// attempt. A renewal that fails, or leaves the credential expired, is an
// attempt like a failed call: LastError records it and the stored reading is
// returned. One that fails because the caller's ctx ended is not an attempt:
// the state goes back to prevAttempt, as in callAndRecord.
func (s *Store) renew(ctx context.Context, r adapter.Renewer, a adapter.Adapter, acct adapter.Account, st state, prevAttempt, now time.Time) (res Result, answered bool, writeErr error) {
	name := acct.Name
	timeout := s.RenewTimeout
	if timeout <= 0 {
		timeout = DefaultRenewTimeout
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := r.Renew(rctx, acct)
	if err != nil && ctx.Err() != nil {
		st.LastAttempt = prevAttempt
		return Result{Reading: st.LastGood, Err: fmt.Errorf("renewal: %w", ctx.Err()), TokenExpired: true}, true, s.write(name, st)
	}
	var reason string
	switch {
	case err != nil:
		reason = capReason(hideTokens(err.Error()))
	case a.Credential(acct).Expired:
		reason = "the login is still expired after renewal"
	default:
		return Result{}, false, nil
	}
	st.LastError, st.LastErrorAt = "renewal failed: "+reason, now
	writeErr = s.write(name, st)
	res = Result{Reading: st.LastGood, TokenExpired: true}
	if st.LastGood == nil {
		res.Err = &adapter.RenewError{Reason: reason}
		if writeErr != nil {
			res.Err = fmt.Errorf("%w; cache unavailable: %w", res.Err, writeErr)
		}
		return res, true, writeErr
	}
	res.Warnings = append(res.Warnings, fmt.Sprintf("%s: renewal failed: %s; using reading from %s (%d min old)",
		name, reason, stamp(st.LastGood.TakenAt), minutesOld(st.LastGood.TakenAt, now)))
	return res, true, writeErr
}

// tokenLike matches what could be a credential in a renewal failure's text:
// an "Authorization: Bearer" value, an sk- key, a JWT, or any other run of
// 32 or more key characters. It errs towards hiding: a long request id is
// hidden too.
var tokenLike = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)\S+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]*)?`),
	regexp.MustCompile(`[A-Za-z0-9_+/=-]{32,}`),
}

// hideTokens replaces each token-like string in a renewal failure's text
// with [redacted]. The text comes from claude's stderr or codex's answer,
// and it is stored in the cache, warned with and shown on the page.
func hideTokens(reason string) string {
	reason = tokenLike[0].ReplaceAllString(reason, "${1}[redacted]")
	for _, re := range tokenLike[1:] {
		reason = re.ReplaceAllString(reason, "[redacted]")
	}
	return reason
}

// capReason cuts a renewal failure's text to maxReasonBytes, on a rune
// boundary.
func capReason(reason string) string {
	if len(reason) <= maxReasonBytes {
		return reason
	}
	cut := maxReasonBytes
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return reason[:cut]
}

// callAndRecord makes the one usage call and stores its outcome. writeErr is
// the failure to store it, if any; the result is still returned. A call
// that fails because the caller's ctx ended (Ctrl-C, the server closing)
// is not an attempt: the state goes back to prevAttempt with no error
// recorded, so the next run calls again. The store's own CallTimeout is
// an attempt like any failure.
func (s *Store) callAndRecord(ctx context.Context, a adapter.Adapter, acct adapter.Account, st state, prevAttempt, now time.Time) (res Result, writeErr error) {
	name := acct.Name
	r, callErr := s.call(ctx, a, acct)
	if callErr != nil && ctx.Err() != nil {
		st.LastAttempt = prevAttempt
		return Result{Reading: st.LastGood, Err: callErr}, s.write(name, st)
	}
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
			res.RetryUntil = st.RetryUntil
		}
		st.LastError, st.LastErrorAt = callErr.Error(), now
	}
	writeErr = s.write(name, st)
	res.Reading, res.Err = st.LastGood, callErr
	if callErr != nil && st.LastGood != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: using reading from %s (%d min old); this call failed: %v",
			name, stamp(st.LastGood.TakenAt), minutesOld(st.LastGood.TakenAt, now), callErr))
	}
	if res.Reading == nil && writeErr != nil {
		res.Err = fmt.Errorf("%w; cache unavailable: %w", callErr, writeErr)
	}
	return res, writeErr
}

// cacheUnavailable is the fail-closed result when no call may be made
// because the cache cannot hold the floor: the stored reading with a
// warning, or an error naming the cache.
func cacheUnavailable(name string, stored *adapter.Reading, err error, now time.Time) Result {
	if stored == nil {
		return Result{Err: fmt.Errorf("cache unavailable: %w; no usage call made", err), CacheErr: err}
	}
	return Result{Reading: stored, CacheErr: err, Warnings: []string{fmt.Sprintf("%s: cache unavailable: %v; using stored reading from %s (%d min old)",
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

// Saved is what the state file holds, for display: no lock, no call.
type Saved struct {
	Reading     *adapter.Reading
	RetryUntil  time.Time
	LastError   string
	LastAttempt time.Time
}

// Saved reads the account's state file without the lock and without any
// network call. No state file is the zero Saved and a nil error.
func (s *Store) Saved(acct adapter.Account) (Saved, error) {
	st, err := s.readFile(acct.Name)
	if err != nil {
		return Saved{}, err
	}
	return Saved{Reading: st.LastGood, RetryUntil: st.RetryUntil, LastError: st.LastError, LastAttempt: st.LastAttempt}, nil
}

// readFile loads the state file; a missing file is the empty state.
func (s *Store) readFile(name string) (state, error) {
	if err := checkName(name); err != nil {
		return state{}, err
	}
	b, err := os.ReadFile(s.statePath(name))
	if errors.Is(err, os.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return state{}, fmt.Errorf("%s: %w", s.statePath(name), err)
	}
	return st, nil
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

// write replaces the state file atomically (platform.WriteFileAtomic; a new
// file is 0600).
func (s *Store) write(name string, st state) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(s.statePath(name), b)
}

// Remove deletes the account's state file and then its lock file, for an
// account that no longer exists. It holds the lock while it removes the state
// file, so a Get of the same account is not mid-write; the lock file goes
// last. A file already missing is not an error, and a cache dir that does not
// exist is not created. It returns the first failure, after trying both.
func (s *Store) Remove(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	lock := s.lockPath(name)
	if _, err := os.Stat(lock); errors.Is(err, os.ErrNotExist) {
		return ignoreMissing(os.Remove(s.statePath(name)))
	}
	unlock, err := platform.LockFile(lock)
	if err != nil {
		return err
	}
	defer unlock()
	stateErr := ignoreMissing(os.Remove(s.statePath(name)))
	if err := ignoreMissing(os.Remove(lock)); err != nil && stateErr == nil {
		stateErr = err
	}
	return stateErr
}

func ignoreMissing(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
