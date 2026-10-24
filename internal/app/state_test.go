package app_test

import (
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/usage"
)

func TestLastAttemptFailed(t *testing.T) {
	taken := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	rd := &adapter.Reading{TakenAt: taken}
	tests := []struct {
		name string
		s    usage.Saved
		want bool
	}{
		{"no error", usage.Saved{Reading: rd, LastAttempt: taken.Add(time.Minute)}, false},
		{"error, no reading", usage.Saved{LastError: "boom"}, true},
		{"error, attempt after reading", usage.Saved{Reading: rd, LastError: "boom", LastAttempt: taken.Add(time.Minute)}, true},
		{"error, attempt before reading", usage.Saved{Reading: rd, LastError: "boom", LastAttempt: taken.Add(-time.Minute)}, false},
	}
	for _, tc := range tests {
		if got := app.LastAttemptFailed(tc.s); got != tc.want {
			t.Errorf("%s: LastAttemptFailed = %v, want %v", tc.name, got, tc.want)
		}
	}
}
