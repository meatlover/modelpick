package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// fakeAdapter counts Usage calls and returns a configurable result. When url
// is set, Usage makes one real HTTP GET to it and reads {"used_pct": n}.
type fakeAdapter struct {
	mu      sync.Mutex
	calls   int
	expired bool
	reading adapter.Reading
	err     error
	delay   time.Duration
	url     string
}

func (f *fakeAdapter) Vendor() string { return "fake" }

func (f *fakeAdapter) Credential(adapter.Account) adapter.CredStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return adapter.CredStatus{Present: true, Expired: f.expired, Source: "test"}
}

func (f *fakeAdapter) Usage(ctx context.Context, _ adapter.Account) (adapter.Reading, error) {
	f.mu.Lock()
	f.calls++
	r, err, delay, url := f.reading, f.err, f.delay, f.url
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return adapter.Reading{}, ctx.Err()
		}
	}
	if url != "" {
		return httpUsage(ctx, url)
	}
	return r, err
}

func (f *fakeAdapter) Models(context.Context, adapter.Account) ([]adapter.Model, error) {
	return nil, nil
}

func (f *fakeAdapter) Check(context.Context, adapter.Account) []adapter.Finding { return nil }

func (f *fakeAdapter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeAdapter) set(fn func(f *fakeAdapter)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func httpUsage(ctx context.Context, url string) (adapter.Reading, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return adapter.Reading{}, err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return adapter.Reading{}, err
	}
	defer resp.Body.Close()
	var body struct {
		UsedPct float64 `json:"used_pct"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return adapter.Reading{}, err
	}
	return adapter.Reading{
		TakenAt: time.Now(),
		Windows: []adapter.Window{{Label: "session", Length: 5 * time.Hour, UsedPct: body.UsedPct}},
	}, nil
}

// clock is an injectable, advanceable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func (c *clock) at(t time.Time) *clock   { c.t = t; return c }
func newClock() *clock                   { return (&clock{}).at(t0) }
func reading(pct float64, taken time.Time) adapter.Reading {
	return adapter.Reading{TakenAt: taken, Windows: []adapter.Window{{Label: "session", Length: 5 * time.Hour, UsedPct: pct}}}
}

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

var acct = adapter.Account{Name: "work", Vendor: "fake"}

func newStore(t *testing.T, c *clock) *Store {
	t.Helper()
	return &Store{Dir: t.TempDir(), Floor: 15 * time.Minute, Now: c.now, CallTimeout: 10 * time.Second}
}

func writeStateFile(t *testing.T, s *Store, st state) {
	t.Helper()
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, acct.Name+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func usedPct(t *testing.T, r Result) float64 {
	t.Helper()
	if r.Reading == nil || len(r.Reading.Windows) == 0 {
		t.Fatalf("no reading: %+v", r)
	}
	return r.Reading.Windows[0].UsedPct
}

func hasWarning(r Result, sub string) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestFloorBlocksSecondCall(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{reading: reading(10, t0)}

	r := s.Get(context.Background(), f, acct, false)
	if r.Err != nil || usedPct(t, r) != 10 {
		t.Fatalf("first Get: %+v", r)
	}
	c.advance(time.Minute)
	f.set(func(f *fakeAdapter) { f.reading = reading(20, c.t) })
	r = s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 1 {
		t.Fatalf("1 min later: calls = %d, want 1", got)
	}
	if r.Err != nil || usedPct(t, r) != 10 || len(r.Warnings) != 0 {
		t.Fatalf("1 min later: want cached 10%% without warnings, got %+v", r)
	}

	c.advance(15 * time.Minute) // 16 min after the first call
	r = s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 2 {
		t.Fatalf("16 min later: calls = %d, want 2", got)
	}
	if r.Err != nil || usedPct(t, r) != 20 {
		t.Fatalf("16 min later: want fresh 20%%, got %+v", r)
	}
}

func TestRefreshBypassesFloorNotRetryAfter(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{reading: reading(10, t0)}

	s.Get(context.Background(), f, acct, false)
	c.advance(time.Minute)
	r := s.Get(context.Background(), f, acct, true)
	if got := f.callCount(); got != 2 {
		t.Fatalf("refresh 1 min later: calls = %d, want 2", got)
	}
	if r.Err != nil {
		t.Fatalf("refresh: %v", r.Err)
	}

	// A 429 with Retry-After: 30m.
	c.advance(time.Minute)
	f.set(func(f *fakeAdapter) { f.err = &adapter.HTTPError{Status: 429, RetryAfter: 30 * time.Minute} })
	r = s.Get(context.Background(), f, acct, true)
	var he *adapter.HTTPError
	if !errors.As(r.Err, &he) || he.Status != 429 {
		t.Fatalf("429 call: want HTTPError 429, got %v", r.Err)
	}
	if got := f.callCount(); got != 3 {
		t.Fatalf("429 call: calls = %d, want 3", got)
	}

	c.advance(time.Minute)
	r = s.Get(context.Background(), f, acct, true)
	if got := f.callCount(); got != 3 {
		t.Fatalf("refresh inside Retry-After: calls = %d, want 3 (no call)", got)
	}
	retryUntil := t0.Add(2*time.Minute + 30*time.Minute).Format(time.RFC3339)
	if !hasWarning(r, "work: rate-limited until "+retryUntil) {
		t.Fatalf("want rate-limited warning until %s, got %q", retryUntil, r.Warnings)
	}
	if usedPct(t, r) != 10 {
		t.Fatalf("want last good reading, got %+v", r)
	}

	// Once Retry-After has passed, refresh calls again.
	c.advance(30 * time.Minute)
	f.set(func(f *fakeAdapter) { f.err = nil; f.reading = reading(30, c.t) })
	r = s.Get(context.Background(), f, acct, true)
	if got := f.callCount(); got != 4 || r.Err != nil || usedPct(t, r) != 30 {
		t.Fatalf("after Retry-After: calls = %d, result %+v", got, r)
	}
}

func TestRetryAfterWithoutReadingIsAnError(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{err: &adapter.HTTPError{Status: 429, RetryAfter: 30 * time.Minute}}

	s.Get(context.Background(), f, acct, false)
	c.advance(20 * time.Minute) // past the floor, inside Retry-After
	r := s.Get(context.Background(), f, acct, true)
	if got := f.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if r.Reading != nil || r.Err == nil {
		t.Fatalf("want no reading and an error, got %+v", r)
	}
	for _, want := range []string{"rate-limited until " + t0.Add(30*time.Minute).Format(time.RFC3339), "HTTP 429"} {
		if !strings.Contains(r.Err.Error(), want) {
			t.Fatalf("error %q does not mention %q", r.Err, want)
		}
	}
}

func TestFailureCountsAsAttempt(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{reading: reading(10, t0)}

	s.Get(context.Background(), f, acct, false) // good reading at 12:00
	c.advance(20 * time.Minute)
	f.set(func(f *fakeAdapter) { f.err = errors.New("connection reset") })
	r := s.Get(context.Background(), f, acct, false) // failing call at 12:20
	if r.Err == nil || usedPct(t, r) != 10 {
		t.Fatalf("failing call: want error plus last good reading, got %+v", r)
	}
	wantNow := "work: using reading from 2026-10-22T12:00:00Z (20 min old); this call failed: connection reset"
	if len(r.Warnings) != 1 || r.Warnings[0] != wantNow {
		t.Fatalf("failing call: warnings = %q, want [%q]", r.Warnings, wantNow)
	}

	c.advance(5 * time.Minute)
	r = s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 2 {
		t.Fatalf("5 min after failure: calls = %d, want 2", got)
	}
	if r.Err != nil || usedPct(t, r) != 10 {
		t.Fatalf("5 min after failure: want last good reading, no error, got %+v", r)
	}
	want := "work: using reading from 2026-10-22T12:00:00Z (25 min old); last call at 12:20:00Z failed: connection reset"
	if len(r.Warnings) != 1 || r.Warnings[0] != want {
		t.Fatalf("warnings = %q, want [%q]", r.Warnings, want)
	}
}

func TestFailureWithoutReadingNamesTheFailure(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{err: errors.New("connection reset")}

	r := s.Get(context.Background(), f, acct, false)
	if r.Reading != nil || r.Err == nil || r.Err.Error() != "connection reset" {
		t.Fatalf("failing call: %+v", r)
	}
	c.advance(5 * time.Minute)
	r = s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if r.Reading != nil || r.Err == nil || !strings.Contains(r.Err.Error(), "failed: connection reset") {
		t.Fatalf("floor-blocked after failure: want error naming the failure, got %+v", r)
	}
}

func TestFloorBlockedWithoutReadingOrErrorIsAnError(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	// An attempt with no outcome recorded, e.g. a process killed mid-call.
	writeStateFile(t, s, state{LastAttempt: t0})
	f := &fakeAdapter{reading: reading(10, t0)}

	c.advance(5 * time.Minute)
	r := s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
	want := "no usage reading yet; next call allowed at 2026-10-22T12:15:00Z"
	if r.Reading != nil || r.Err == nil || r.Err.Error() != want {
		t.Fatalf("want error %q, got %+v", want, r)
	}
}

func TestExpiredTokenDoesNotUseFloor(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{expired: true, reading: reading(10, t0)}

	r := s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 0 {
		t.Fatalf("expired: calls = %d, want 0", got)
	}
	if r.Err != adapter.ErrTokenExpired || r.Reading != nil {
		t.Fatalf("expired: want ErrTokenExpired and no reading, got %+v", r)
	}

	f.set(func(f *fakeAdapter) { f.expired = false })
	r = s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 1 {
		t.Fatalf("not expired: calls = %d, want 1 immediately", got)
	}
	if r.Err != nil || usedPct(t, r) != 10 {
		t.Fatalf("not expired: %+v", r)
	}

	// Expired again later: the last good reading, with its age.
	c.advance(42 * time.Minute)
	f.set(func(f *fakeAdapter) { f.expired = true })
	r = s.Get(context.Background(), f, acct, true)
	if got := f.callCount(); got != 1 {
		t.Fatalf("expired with reading: calls = %d, want 1", got)
	}
	want := "work: token expired; using reading from 2026-10-22T12:00:00Z (42 min old)"
	if r.Err != nil || usedPct(t, r) != 10 || len(r.Warnings) != 1 || r.Warnings[0] != want {
		t.Fatalf("expired with reading: want warning %q, got %+v", want, r)
	}
}

// TestResultFlags checks the structured state Get reports for callers that
// must not parse warning text: TokenExpired and RetryUntil.
func TestResultFlags(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{expired: true}

	if r := s.Get(context.Background(), f, acct, false); !r.TokenExpired || r.Reading != nil {
		t.Fatalf("expired, no reading: want TokenExpired, got %+v", r)
	}
	f.set(func(f *fakeAdapter) { f.expired = false; f.reading = reading(10, t0) })
	if r := s.Get(context.Background(), f, acct, false); r.TokenExpired || !r.RetryUntil.IsZero() {
		t.Fatalf("good call: want no flags, got %+v", r)
	}
	f.set(func(f *fakeAdapter) { f.expired = true })
	if r := s.Get(context.Background(), f, acct, false); !r.TokenExpired || r.Reading == nil {
		t.Fatalf("expired, cached reading: want TokenExpired with the reading, got %+v", r)
	}

	// A 429 with Retry-After: 30m, then a Get inside the window.
	c.advance(20 * time.Minute)
	until := c.t.Add(30 * time.Minute)
	f.set(func(f *fakeAdapter) {
		f.expired = false
		f.err = &adapter.HTTPError{Status: 429, RetryAfter: 30 * time.Minute}
	})
	if r := s.Get(context.Background(), f, acct, false); !r.RetryUntil.Equal(until) || r.TokenExpired {
		t.Fatalf("429 call: want RetryUntil %v, got %+v", until, r)
	}
	c.advance(time.Minute)
	if r := s.Get(context.Background(), f, acct, true); !r.RetryUntil.Equal(until) || r.Reading == nil {
		t.Fatalf("inside Retry-After: want RetryUntil %v with the reading, got %+v", until, r)
	}
	c.advance(30 * time.Minute)
	f.set(func(f *fakeAdapter) { f.err = nil })
	if r := s.Get(context.Background(), f, acct, false); !r.RetryUntil.IsZero() {
		t.Fatalf("after Retry-After: want RetryUntil zero, got %+v", r)
	}
}

func TestCorruptStateIsIgnored(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	if err := os.WriteFile(filepath.Join(s.Dir, "work.json"), []byte(`{"last_go`), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeAdapter{reading: reading(10, t0)}
	r := s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if r.Err != nil || usedPct(t, r) != 10 {
		t.Fatalf("%+v", r)
	}
}

func TestStateFileIsPrivateAndHoldsNoTemp(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{reading: reading(10, t0)}
	s.Get(context.Background(), f, acct, false)

	fi, err := os.Stat(filepath.Join(s.Dir, "work.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state file mode = %o, want 600", perm)
	}
	entries, _ := os.ReadDir(s.Dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "work.json,work.lock" {
		t.Fatalf("cache dir holds %q, want only work.json and work.lock", names)
	}
	got, err := s.Saved(acct)
	if err != nil || got.Reading == nil || got.Reading.Windows[0].UsedPct != 10 {
		t.Fatalf("Saved = %+v, %v", got, err)
	}
}

// TestUnusableCacheMakesNoCall checks Ruling 27: when the lock cannot be
// taken or the attempt cannot be persisted, the floor fails closed — no
// vendor call; the stored reading if there is one, else an error naming the
// cache.
func TestUnusableCacheMakesNoCall(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only directory")
	}
	old := reading(10, t0.Add(-time.Hour))
	for _, c := range []struct {
		name       string
		lockExists bool // lock taken, but the attempt cannot be written
		stored     bool
	}{
		{name: "no lock, no reading"},
		{name: "no lock, stored reading", stored: true},
		{name: "attempt not persisted, no reading", lockExists: true},
		{name: "attempt not persisted, stored reading", lockExists: true, stored: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t, newClock())
			if c.stored {
				writeStateFile(t, s, state{LastGood: &old, LastAttempt: old.TakenAt})
			}
			if c.lockExists {
				if err := os.WriteFile(filepath.Join(s.Dir, "work.lock"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(s.Dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(s.Dir, 0o700) })

			f := &fakeAdapter{reading: reading(20, t0)}
			r := s.Get(context.Background(), f, acct, false)
			if got := f.callCount(); got != 0 {
				t.Fatalf("calls = %d, want 0 (fail closed)", got)
			}
			if !c.stored {
				if r.Reading != nil || r.Err == nil ||
					!strings.HasPrefix(r.Err.Error(), "cache unavailable: ") ||
					!strings.HasSuffix(r.Err.Error(), "; no usage call made") {
					t.Fatalf("want error naming the cache, got %+v", r)
				}
				return
			}
			if r.Err != nil || usedPct(t, r) != 10 {
				t.Fatalf("want the stored reading, got %+v", r)
			}
			wantSuffix := "; using stored reading from 2026-10-22T11:00:00Z (60 min old)"
			if len(r.Warnings) != 1 || !strings.HasPrefix(r.Warnings[0], "work: cache unavailable: ") ||
				!strings.HasSuffix(r.Warnings[0], wantSuffix) {
				t.Fatalf("want one cache-unavailable warning ending %q, got %q", wantSuffix, r.Warnings)
			}
		})
	}
}

// TestAnsweredWithoutLockSetsCacheErr: the lock cannot be taken, but no
// call is due (the reading is within the floor), so Get answers from the
// stored state; CacheErr still names the lock failure, beside the warning,
// for a caller (doctor all) that reports it.
func TestAnsweredWithoutLockSetsCacheErr(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only directory")
	}
	s := newStore(t, newClock())
	recent := reading(10, t0.Add(-time.Minute))
	writeStateFile(t, s, state{LastGood: &recent, LastAttempt: recent.TakenAt})
	if err := os.Chmod(s.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.Dir, 0o700) })

	f := &fakeAdapter{reading: reading(20, t0)}
	r := s.Get(context.Background(), f, acct, false)
	if got := f.callCount(); got != 0 || r.Err != nil || usedPct(t, r) != 10 {
		t.Fatalf("calls %d, result %+v: want the stored reading, no call", got, r)
	}
	if r.CacheErr == nil || !hasWarning(r, "work: cache unavailable: ") {
		t.Fatalf("CacheErr %v, warnings %q: want the lock failure in both", r.CacheErr, r.Warnings)
	}
}

func TestCallTimeoutApplies(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	s.CallTimeout = 20 * time.Millisecond
	f := &fakeAdapter{reading: reading(10, t0), delay: 5 * time.Second}
	start := time.Now()
	r := s.Get(context.Background(), f, acct, false)
	if !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %+v", r)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("Get took %v; CallTimeout not applied", el)
	}
}

func TestInvalidAccountNameIsRefused(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := &fakeAdapter{reading: reading(10, t0)}
	for _, name := range []string{"", ".", "..", "../x", "a/b"} {
		r := s.Get(context.Background(), f, adapter.Account{Name: name, Vendor: "fake"}, false)
		if r.Err == nil || r.Reading != nil {
			t.Fatalf("name %q: want error, got %+v", name, r)
		}
	}
	if got := f.callCount(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestAgeNeverNegative(t *testing.T) {
	if got := Age(t0.Add(time.Minute), t0); got != 0 {
		t.Fatalf("future reading: Age = %v, want 0", got)
	}
	if got := Age(t0, t0.Add(90*time.Second)); got != 90*time.Second {
		t.Fatalf("Age = %v, want 1m30s", got)
	}
}

// TestNoReadingImpliesError checks Ruling 10 on every path through Get: a
// Result never has Reading == nil together with Err == nil.
func TestNoReadingImpliesError(t *testing.T) {
	good := reading(10, t0.Add(-time.Hour))
	type tc struct {
		name    string
		st      *state // nil = no state file
		f       *fakeAdapter
		refresh bool
		roDir   bool
		lockOK  bool // with roDir: the lock file exists, so only the attempt write fails
		acct    string
	}
	cases := []tc{
		{name: "expired, no reading", f: &fakeAdapter{expired: true}},
		{name: "expired, reading", st: &state{LastGood: &good}, f: &fakeAdapter{expired: true}},
		{name: "rate-limited, no reading", st: &state{RetryUntil: t0.Add(time.Minute)}, f: &fakeAdapter{}},
		{name: "rate-limited, no reading, earlier error", st: &state{RetryUntil: t0.Add(time.Minute), LastError: "HTTP 429"}, f: &fakeAdapter{}},
		{name: "rate-limited, reading", st: &state{RetryUntil: t0.Add(time.Minute), LastGood: &good}, f: &fakeAdapter{}},
		{name: "floor, no reading, no error", st: &state{LastAttempt: t0.Add(-time.Minute)}, f: &fakeAdapter{}},
		{name: "floor, no reading, earlier error", st: &state{LastAttempt: t0.Add(-time.Minute), LastError: "boom"}, f: &fakeAdapter{}},
		{name: "floor, reading", st: &state{LastAttempt: t0.Add(-time.Minute), LastGood: &good}, f: &fakeAdapter{}},
		{name: "call ok", f: &fakeAdapter{reading: reading(10, t0)}},
		{name: "call ok, refresh", st: &state{LastAttempt: t0.Add(-time.Minute)}, f: &fakeAdapter{reading: reading(10, t0)}, refresh: true},
		{name: "call fails, no reading", f: &fakeAdapter{err: errors.New("boom")}},
		{name: "call fails, reading", st: &state{LastGood: &good}, f: &fakeAdapter{err: errors.New("boom")}},
		{name: "call 429, no reading", f: &fakeAdapter{err: &adapter.HTTPError{Status: 429, RetryAfter: time.Minute}}},
		{name: "call times out", f: &fakeAdapter{delay: time.Second}},
		{name: "cache unwritable, call ok", roDir: true, f: &fakeAdapter{reading: reading(10, t0)}},
		{name: "cache unwritable, call fails", roDir: true, f: &fakeAdapter{err: errors.New("boom")}},
		{name: "cache unwritable, stored reading", roDir: true, st: &state{LastGood: &good}, f: &fakeAdapter{reading: reading(10, t0)}},
		{name: "attempt not persisted, no reading", roDir: true, lockOK: true, f: &fakeAdapter{reading: reading(10, t0)}},
		{name: "attempt not persisted, stored reading", roDir: true, lockOK: true, st: &state{LastGood: &good}, f: &fakeAdapter{reading: reading(10, t0)}},
		{name: "invalid account name", acct: "../x", f: &fakeAdapter{}},
	}
	a0 := acct
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.roDir && os.Geteuid() == 0 {
				t.Skip("root can write a read-only directory")
			}
			s := newStore(t, newClock())
			s.CallTimeout = 50 * time.Millisecond
			if c.st != nil {
				writeStateFile(t, s, *c.st)
			}
			if c.lockOK {
				if err := os.WriteFile(filepath.Join(s.Dir, a0.Name+".lock"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.roDir {
				if err := os.Chmod(s.Dir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(s.Dir, 0o700) })
			}
			a := acct
			if c.acct != "" {
				a.Name = c.acct
			}
			r := s.Get(context.Background(), c.f, a, c.refresh)
			if r.Reading == nil && r.Err == nil {
				t.Fatalf("Reading == nil and Err == nil: %+v", r)
			}
		})
	}
}

// TestConcurrentProcessesMakeOneCall starts five processes against one cold
// cache at the same instant; the server sees exactly one request and every
// process answers with that request's reading.
func TestConcurrentProcessesMakeOneCall(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := count.Add(1)
		time.Sleep(300 * time.Millisecond)
		fmt.Fprintf(w, `{"used_pct": %d}`, 10*n)
	}))
	defer srv.Close()

	dir := t.TempDir()
	startAt := time.Now().Add(1500 * time.Millisecond)
	const n = 5
	type out struct {
		text string
		err  error
	}
	outs := make([]out, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(),
			"MODELPICK_HELPER=1",
			"MODELPICK_HELPER_DIR="+dir,
			"MODELPICK_HELPER_URL="+srv.URL,
			"MODELPICK_HELPER_START="+strconv.FormatInt(startAt.UnixNano(), 10),
		)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, err := cmd.Output()
			outs[i] = out{string(b), err}
		}(i)
	}
	wg.Wait()

	var pcts []string
	for i, o := range outs {
		if o.err != nil {
			t.Fatalf("helper %d: %v\n%s", i, o.err, o.text)
		}
		var pct string
		var waited time.Duration
		sc := bufio.NewScanner(strings.NewReader(o.text))
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "USED_PCT "); ok {
				pct = v
			}
			if v, ok := strings.CutPrefix(sc.Text(), "GET_MS "); ok {
				ms, _ := strconv.Atoi(v)
				waited = time.Duration(ms) * time.Millisecond
			}
		}
		if pct == "" {
			t.Fatalf("helper %d printed no reading:\n%s", i, o.text)
		}
		// Every helper must have overlapped the one in-flight call; one that
		// arrived after it finished would not exercise the lock at all.
		if waited < 100*time.Millisecond {
			t.Fatalf("helper %d's Get took %v: it did not overlap the call, so contention was not exercised", i, waited)
		}
		pcts = append(pcts, pct)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("server saw %d requests, want 1", got)
	}
	for _, p := range pcts {
		if p != pcts[0] {
			t.Fatalf("helpers disagree on UsedPct: %q", pcts)
		}
	}
}

// TestHelperProcess is the body of one process in
// TestConcurrentProcessesMakeOneCall; it does nothing in a normal run.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("MODELPICK_HELPER") != "1" {
		t.Skip("helper process only")
	}
	startNs, err := strconv.ParseInt(os.Getenv("MODELPICK_HELPER_START"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(time.Unix(0, startNs)))
	s := &Store{Dir: os.Getenv("MODELPICK_HELPER_DIR"), Floor: 15 * time.Minute, CallTimeout: 10 * time.Second}
	f := &fakeAdapter{url: os.Getenv("MODELPICK_HELPER_URL")}
	begin := time.Now()
	r := s.Get(context.Background(), f, acct, false)
	took := time.Since(begin)
	if r.Err != nil {
		t.Fatalf("Get: %v", r.Err)
	}
	fmt.Printf("USED_PCT %v\nGET_MS %d\n", r.Reading.Windows[0].UsedPct, took.Milliseconds())
}

func TestSaved(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	if got, err := s.Saved(acct); err != nil || got.Reading != nil || !got.RetryUntil.IsZero() || got.LastError != "" || !got.LastAttempt.IsZero() {
		t.Fatalf("no state: Saved = %+v, %v; want zero, nil", got, err)
	}
	good := reading(33, t0)
	writeStateFile(t, s, state{LastGood: &good, LastAttempt: t0, LastError: "HTTP 429", RetryUntil: t0.Add(time.Hour)})
	got, err := s.Saved(acct)
	if err != nil || got.Reading == nil || got.Reading.Windows[0].UsedPct != 33 ||
		!got.RetryUntil.Equal(t0.Add(time.Hour)) || got.LastError != "HTTP 429" || !got.LastAttempt.Equal(t0) {
		t.Fatalf("Saved = %+v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "work.json"), []byte(`{"last_go`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Saved(acct); err == nil {
		t.Fatal("corrupt state: want an error")
	}
	if _, err := s.Saved(adapter.Account{Name: "a/b"}); err == nil {
		t.Fatal("bad name: want an error")
	}
}

// A call cut short by the caller's ctx (Ctrl-C during a first reading) is
// not an attempt: the state stays as it was, so the next run calls again
// instead of showing "context canceled" until the floor passes. The
// store's own CallTimeout is still a failed attempt.
func TestCallerCancelIsNotRecorded(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	earlier := reading(10, t0.Add(-30*time.Minute))
	writeStateFile(t, s, state{LastGood: &earlier, LastAttempt: t0.Add(-30 * time.Minute)})
	f := &fakeAdapter{reading: reading(20, t0), delay: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for f.callCount() == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	if r := s.Get(ctx, f, acct, false); !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("cancelled Get: %+v, want context.Canceled", r)
	}
	saved, err := s.Saved(acct)
	if err != nil || saved.LastError != "" || !saved.LastAttempt.Equal(t0.Add(-30*time.Minute)) {
		t.Fatalf("after a cancel: saved %+v, %v; want the state from before the call", saved, err)
	}
	f.set(func(f *fakeAdapter) { f.delay = 0 })
	if r := s.Get(context.Background(), f, acct, false); r.Err != nil || usedPct(t, r) != 20 || f.callCount() != 2 {
		t.Fatalf("next Get: %+v, calls %d; want a new call", r, f.callCount())
	}

	s.CallTimeout = 10 * time.Millisecond
	f.set(func(f *fakeAdapter) { f.delay = 5 * time.Second })
	c.advance(20 * time.Minute)
	s.Get(context.Background(), f, acct, false)
	if saved, _ := s.Saved(acct); !strings.Contains(saved.LastError, "deadline exceeded") || !saved.LastAttempt.Equal(c.t) {
		t.Fatalf("after the store's timeout: saved %+v; want the failed attempt recorded", saved)
	}
}

// TestResultNamesTheCacheFailure checks that Result.CacheErr carries the
// first lock or write failure, and is nil when the cache works.
func TestResultNamesTheCacheFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only directory")
	}
	s := newStore(t, newClock())
	f := &fakeAdapter{reading: reading(20, t0)}
	if r := s.Get(context.Background(), f, acct, false); r.CacheErr != nil {
		t.Fatalf("working cache: CacheErr = %v, want nil", r.CacheErr)
	}
	if err := os.Chmod(s.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.Dir, 0o700) })
	r := s.Get(context.Background(), f, acct, true)
	if r.CacheErr == nil {
		t.Fatalf("unwritable cache: CacheErr = nil, result %+v", r)
	}
	if !hasWarning(r, "cache unavailable: "+r.CacheErr.Error()) {
		t.Errorf("warnings %q do not carry CacheErr %v", r.Warnings, r.CacheErr)
	}
}

// renewingAdapter is a fakeAdapter that can renew: Renew counts calls, can
// sleep (delay, honouring ctx) and fail (err); on success the credential is
// no longer expired unless stayExpired.
type renewingAdapter struct {
	*fakeAdapter
	rmu         sync.Mutex
	renews      int
	renewDelay  time.Duration
	renewErr    error
	stayExpired bool
}

func (r *renewingAdapter) Renew(ctx context.Context, _ adapter.Account) error {
	r.rmu.Lock()
	r.renews++
	delay, err, stay := r.renewDelay, r.renewErr, r.stayExpired
	r.rmu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	if !stay {
		r.fakeAdapter.set(func(f *fakeAdapter) { f.expired = false })
	}
	return nil
}

func (r *renewingAdapter) renewCount() int {
	r.rmu.Lock()
	defer r.rmu.Unlock()
	return r.renews
}

func (r *renewingAdapter) setRenew(fn func(r *renewingAdapter)) {
	r.rmu.Lock()
	defer r.rmu.Unlock()
	fn(r)
}

// expiredRenewer is an expired renewable account whose stored reading (10%)
// is age old at t0; renewal then a call yields 20%.
func expiredRenewer(t *testing.T, s *Store, age time.Duration) *renewingAdapter {
	t.Helper()
	if age >= 0 {
		old := reading(10, t0.Add(-age))
		writeStateFile(t, s, state{LastGood: &old, LastAttempt: t0.Add(-age)})
	}
	return &renewingAdapter{fakeAdapter: &fakeAdapter{expired: true, reading: reading(20, t0)}}
}

func TestRenewThenCall(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, 16*time.Minute)
	r := s.Get(context.Background(), f, acct, false)
	if f.renewCount() != 1 || f.callCount() != 1 || r.Err != nil || usedPct(t, r) != 20 || r.TokenExpired || len(r.Warnings) != 0 {
		t.Fatalf("renews %d calls %d result %+v; want 1, 1, 20%%, not expired, no warnings", f.renewCount(), f.callCount(), r)
	}
}

func TestRenewNotWhenReadingFresh(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		s := newStore(t, newClock())
		f := expiredRenewer(t, s, 15*time.Minute)
		r := s.Get(context.Background(), f, acct, refresh)
		want := "work: token expired; using reading from 2026-10-22T11:45:00Z (15 min old)"
		if f.renewCount() != 0 || f.callCount() != 0 || usedPct(t, r) != 10 || !r.TokenExpired || len(r.Warnings) != 1 || r.Warnings[0] != want {
			t.Fatalf("refresh=%v: renews %d calls %d result %+v; want none, 10%%, expired, warning %q", refresh, f.renewCount(), f.callCount(), r, want)
		}
	}
}

func TestRenewWithoutReading(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, -1)
	r := s.Get(context.Background(), f, acct, false)
	if f.renewCount() != 1 || f.callCount() != 1 || r.Err != nil || usedPct(t, r) != 20 {
		t.Fatalf("renews %d calls %d result %+v; want 1, 1, 20%%", f.renewCount(), f.callCount(), r)
	}
}

func TestRenewFailureIsAnAttempt(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.renewErr = errors.New("exit status 1: boom") })
	r := s.Get(context.Background(), f, acct, false)
	want := "work: renewal failed: exit status 1: boom; using reading from 2026-10-22T11:30:00Z (30 min old)"
	if f.renewCount() != 1 || f.callCount() != 0 || usedPct(t, r) != 10 || !r.TokenExpired || len(r.Warnings) != 1 || r.Warnings[0] != want {
		t.Fatalf("renews %d calls %d result %+v; want 1, 0, 10%%, expired, warning %q", f.renewCount(), f.callCount(), r, want)
	}
	if saved, _ := s.Saved(acct); saved.LastError != "renewal failed: exit status 1: boom" || !saved.LastAttempt.Equal(t0) {
		t.Fatalf("saved %+v; want the failure recorded as an attempt", saved)
	}
	c.advance(5 * time.Minute)
	s.Get(context.Background(), f, acct, false)
	if f.renewCount() != 1 {
		t.Fatalf("5 min later without refresh: renews = %d, want still 1", f.renewCount())
	}
	s.Get(context.Background(), f, acct, true)
	if f.renewCount() != 2 {
		t.Fatalf("with refresh: renews = %d, want 2", f.renewCount())
	}
}

func TestRenewFailureWithoutReading(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, -1)
	f.setRenew(func(r *renewingAdapter) { r.renewErr = errors.New("exit status 1: boom") })
	r := s.Get(context.Background(), f, acct, false)
	var re *adapter.RenewError
	if r.Reading != nil || !errors.As(r.Err, &re) || re.Reason != "exit status 1: boom" || !errors.Is(r.Err, adapter.ErrTokenExpired) {
		t.Fatalf("result %+v; want no reading and a RenewError with the reason", r)
	}
}

// TestRenewFailureHidesTokens: the reason comes from claude's stderr or
// codex's answer, so a token-like string in it is replaced before it is
// stored, warned with or returned.
func TestRenewFailureHidesTokens(t *testing.T) {
	secrets := []string{
		"sk-ant-oat01-AbCdEf123456_-xyz",
		"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJl",
		"Qx7mZp2Lw9Rt4Kv8Nb3Hc6Jd1Fg5Ys0Ua7Ie2Wo9",
		"tok_9f8e7d6c5b4a",
	}
	msg := "exit status 1: bad key " + secrets[0] + ", jwt " + secrets[1] + " id " + secrets[2] + ", Authorization: Bearer " + secrets[3]
	check := func(where, got string) {
		t.Helper()
		for _, sec := range secrets {
			if strings.Contains(got, sec[:12]) {
				t.Errorf("%s %q shows %q", where, got, sec[:12])
			}
		}
		if !strings.Contains(got, "exit status 1: bad key [redacted]") {
			t.Errorf("%s %q lost the rest of the reason", where, got)
		}
	}

	s := newStore(t, newClock())
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.renewErr = errors.New(msg) })
	r := s.Get(context.Background(), f, acct, false)
	if len(r.Warnings) != 1 {
		t.Fatalf("warnings %q; want one", r.Warnings)
	}
	check("warning", r.Warnings[0])
	saved, _ := s.Saved(acct)
	check("saved error", saved.LastError)

	s = newStore(t, newClock())
	f = expiredRenewer(t, s, -1)
	f.setRenew(func(r *renewingAdapter) { r.renewErr = errors.New(msg) })
	r = s.Get(context.Background(), f, acct, false)
	if r.Err == nil {
		t.Fatal("no error without a reading")
	}
	check("error", r.Err.Error())
}

func TestRenewStillExpired(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.stayExpired = true })
	r := s.Get(context.Background(), f, acct, false)
	want := "work: renewal failed: the login is still expired after renewal; using reading from 2026-10-22T11:30:00Z (30 min old)"
	if f.renewCount() != 1 || f.callCount() != 0 || len(r.Warnings) != 1 || r.Warnings[0] != want {
		t.Fatalf("renews %d calls %d result %+v; want warning %q", f.renewCount(), f.callCount(), r, want)
	}
}

func TestRenewRespectsRetryAfter(t *testing.T) {
	s := newStore(t, newClock())
	old := reading(10, t0.Add(-30*time.Minute))
	writeStateFile(t, s, state{LastGood: &old, LastAttempt: t0.Add(-30 * time.Minute), RetryUntil: t0.Add(10 * time.Minute)})
	f := &renewingAdapter{fakeAdapter: &fakeAdapter{expired: true, reading: reading(20, t0)}}
	r := s.Get(context.Background(), f, acct, false)
	if f.renewCount() != 0 || f.callCount() != 0 || !r.TokenExpired || r.RetryUntil.IsZero() {
		t.Fatalf("renews %d calls %d result %+v; want none, expired, RetryUntil set", f.renewCount(), f.callCount(), r)
	}
}

func TestRenewCallerCancel(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.renewDelay = 5 * time.Second })
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	r := s.Get(ctx, f, acct, false)
	if !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("result %+v; want context.Canceled", r)
	}
	saved, err := s.Saved(acct)
	if err != nil || saved.LastError != "" || !saved.LastAttempt.Equal(t0.Add(-30*time.Minute)) {
		t.Fatalf("saved %+v, %v; want the state from before the Get", saved, err)
	}
}

// A renewal that dies with its own error because the caller's ctx ended
// reports the ctx, not the child's "signal: killed".
func TestRenewCallerCancelReportsCtx(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.renewErr = errors.New("signal: killed") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := s.Get(ctx, f, acct, false)
	if !errors.Is(r.Err, context.Canceled) {
		t.Fatalf("result %+v; want context.Canceled", r)
	}
}

func TestRenewTimeout(t *testing.T) {
	s := newStore(t, newClock())
	s.RenewTimeout = 50 * time.Millisecond
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.renewDelay = 5 * time.Second })
	s.Get(context.Background(), f, acct, false)
	saved, _ := s.Saved(acct)
	if !strings.HasPrefix(saved.LastError, "renewal failed: ") || !strings.Contains(saved.LastError, "context deadline exceeded") || !saved.LastAttempt.Equal(t0) {
		t.Fatalf("saved %+v; want a recorded renewal timeout", saved)
	}
}

func TestNoRenew(t *testing.T) {
	s := newStore(t, newClock())
	s.NoRenew = true
	f := expiredRenewer(t, s, 30*time.Minute)
	r := s.Get(context.Background(), f, acct, false)
	want := "work: token expired; using reading from 2026-10-22T11:30:00Z (30 min old)"
	if f.renewCount() != 0 || f.callCount() != 0 || !r.TokenExpired || len(r.Warnings) != 1 || r.Warnings[0] != want {
		t.Fatalf("renews %d calls %d result %+v; want today's expired answer", f.renewCount(), f.callCount(), r)
	}
}

func TestRenewFloorZero(t *testing.T) {
	s := newStore(t, newClock())
	s.Floor = 0
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.renewErr = errors.New("boom") })
	for i := 0; i < 3; i++ {
		r := s.Get(context.Background(), f, acct, false)
		if usedPct(t, r) != 10 || !hasWarning(r, "work: renewal failed: boom; using reading from") {
			t.Fatalf("Get %d: %+v; want 10%% with the renewal warning", i, r)
		}
	}
	if f.renewCount() != 3 {
		t.Fatalf("renews = %d, want 3", f.renewCount())
	}
}

func TestRenewReasonIsCapped(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, -1)
	f.setRenew(func(r *renewingAdapter) { r.renewErr = errors.New(strings.Repeat("é", 300)) })
	r := s.Get(context.Background(), f, acct, false)
	var re *adapter.RenewError
	if !errors.As(r.Err, &re) || len(re.Reason) > 200 || !strings.HasPrefix(re.Reason, "é") {
		t.Fatalf("result %+v; want a reason of at most 200 bytes", r)
	}
}

func TestRenewOnceUnderContention(t *testing.T) {
	s := newStore(t, newClock())
	f := expiredRenewer(t, s, 30*time.Minute)
	f.setRenew(func(r *renewingAdapter) { r.renewDelay = 200 * time.Millisecond })
	var wg sync.WaitGroup
	results := make([]Result, 5)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = s.Get(context.Background(), f, acct, false)
		}()
	}
	wg.Wait()
	if f.renewCount() != 1 || f.callCount() != 1 {
		t.Fatalf("renews %d calls %d; want 1, 1", f.renewCount(), f.callCount())
	}
	for i, r := range results {
		if r.Err != nil || usedPct(t, r) != 20 {
			t.Fatalf("Get %d: %+v; want 20%%", i, r)
		}
	}
}

func TestRemoveDeletesStateAndLockOnly(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	for _, n := range []string{"a.json", "a.lock", "b.json", "b.lock"} {
		if err := os.WriteFile(filepath.Join(s.Dir, n), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Remove("a"); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[string]bool{"a.json": false, "a.lock": false, "b.json": true, "b.lock": true} {
		if _, err := os.Stat(filepath.Join(s.Dir, n)); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", n, err == nil, want)
		}
	}
}

func TestRemoveMissingFilesIsNotAnError(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "nope")}
	if err := s.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir); err == nil {
		t.Error("Remove created the cache dir")
	}
}

func TestRemoveRejectsBadName(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if err := s.Remove("../x"); err == nil {
		t.Fatal("Remove accepted a name with a path separator")
	}
}
