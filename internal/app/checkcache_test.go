package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

// Ruling 4 (task 11): a doctor run on page load or focus re-runs the local
// Check part but keeps the model lists until an account operation.
func TestForgetChecksKeepsModels(t *testing.T) {
	cfg := docConfig("", codexA+`}`)
	claude, codex := apptest.DoctorFakes()
	cache := &app.CheckCache{}
	runDoctorWith(t, cfg, claude, codex, cache)
	cache.ForgetChecks()
	runDoctorWith(t, cfg, claude, codex, cache)
	if codex.CheckCalls["codex-main"] != 2 || codex.ModelCalls["codex-main"] != 1 {
		t.Fatalf("after ForgetChecks: Check %d (want 2), Models %d (want 1)",
			codex.CheckCalls["codex-main"], codex.ModelCalls["codex-main"])
	}
	cache.Forget("codex-main")
	runDoctorWith(t, cfg, claude, codex, cache)
	if codex.ModelCalls["codex-main"] != 2 {
		t.Fatalf("after Forget: Models %d, want 2", codex.ModelCalls["codex-main"])
	}
}

// blocking holds Models for the account "slow" until release is closed.
type blocking struct {
	*apptest.Fake
	entered chan struct{}
	release chan struct{}
}

func (b blocking) Models(ctx context.Context, acct adapter.Account) ([]adapter.Model, error) {
	if acct.Name == "slow" {
		close(b.entered)
		<-b.release
	}
	return b.Fake.Models(ctx, acct)
}

// A slow model listing for one account must not hold up another account's
// lookup (the cache's mutex is not held across vendor work).
func TestCheckCacheSlowAccountDoesNotBlockOthers(t *testing.T) {
	f := apptest.NewFake("claude")
	f.ModelList["fast"] = []adapter.Model{{ID: "m"}}
	b := blocking{Fake: f, entered: make(chan struct{}), release: make(chan struct{})}
	cache := &app.CheckCache{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cache.Models(adapter.Account{Name: "slow", Vendor: "claude"}, b)
	}()
	<-b.entered
	got := make(chan []adapter.Model, 1)
	go func() {
		ms, _ := cache.Models(adapter.Account{Name: "fast", Vendor: "claude"}, b)
		got <- ms
	}()
	select {
	case ms := <-got:
		if len(ms) != 1 || ms[0].ID != "m" {
			t.Errorf("fast account models: %v", ms)
		}
	case <-time.After(2 * time.Second):
		t.Error("fast account waited behind the slow one")
	}
	close(b.release)
	<-done
}

// A listing cut short by its ctx is not cached: the next call lists again.
func TestModelsCtxCancelledListingIsNotCached(t *testing.T) {
	fk := apptest.NewFake("codex")
	fk.ModelDelay = 5 * time.Second
	fk.ModelList["a"] = []adapter.Model{{ID: "m"}}
	cache := &app.CheckCache{}
	acct := adapter.Account{Name: "a"}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for fk.ModelCallCount("a") == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	if _, err := cache.ModelsCtx(ctx, acct, fk); err == nil {
		t.Fatal("cancelled listing returned no error")
	}
	fk.ModelDelay = 0
	ms, err := cache.Models(acct, fk)
	if err != nil || len(ms) != 1 {
		t.Fatalf("second listing: %v, %v", ms, err)
	}
	if fk.ModelCallCount("a") != 2 {
		t.Errorf("Models calls %d, want 2 (cancelled result must not be cached)", fk.ModelCallCount("a"))
	}
	cache.Models(acct, fk)
	if fk.ModelCallCount("a") != 2 {
		t.Errorf("a finished listing was not cached: %d calls", fk.ModelCallCount("a"))
	}
}

// A panicking adapter fails its own listing: logged, an error, not cached.
func TestModelsPanicIsContained(t *testing.T) {
	var log strings.Builder
	app.CapturePanicLog(t, &log)
	fk := apptest.NewFake("codex")
	fk.ModelPanic = map[string]bool{"a": true}
	cache := &app.CheckCache{}
	acct := adapter.Account{Name: "a"}
	for i := 0; i < 2; i++ {
		if _, err := cache.Models(acct, fk); err == nil {
			t.Fatal("panicking listing returned no error")
		}
	}
	if fk.ModelCallCount("a") != 2 {
		t.Errorf("a panic was cached: %d calls, want 2", fk.ModelCallCount("a"))
	}
	if !strings.Contains(log.String(), "panicked") {
		t.Errorf("panic not logged: %q", log.String())
	}
}
