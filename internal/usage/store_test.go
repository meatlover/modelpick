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
	got, err := s.Peek(acct)
	if err != nil || got == nil || got.Windows[0].UsedPct != 10 {
		t.Fatalf("Peek = %+v, %v", got, err)
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

func TestPeek(t *testing.T) {
	c := newClock()
	s := newStore(t, c)
	if r, err := s.Peek(acct); r != nil || err != nil {
		t.Fatalf("no state: Peek = %+v, %v; want nil, nil", r, err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "work.json"), []byte(`{"last_go`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Peek(acct); err == nil {
		t.Fatal("corrupt state: want an error")
	}
	good := reading(33, t0)
	writeStateFile(t, s, state{LastGood: &good, LastAttempt: t0})
	got, err := s.Peek(acct)
	if err != nil || got == nil || got.Windows[0].UsedPct != 33 {
		t.Fatalf("Peek = %+v, %v", got, err)
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
