package adapter

import (
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	for _, c := range []struct {
		in   string
		want time.Duration
	}{
		{"120", 120 * time.Second},
		{" 120 ", 120 * time.Second},
		{"0", 0},
		{"", 0},
		{"-5", 0},
		{"abc", 0},
		{"1.5", 0},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0},
		{"86400", 24 * time.Hour},
		{"86401", 24 * time.Hour},
		{"10000000000", 24 * time.Hour},          // overflows time.Duration if multiplied naively
		{"99999999999999999999", 24 * time.Hour}, // overflows int64
	} {
		if got := ParseRetryAfter(c.in); got != c.want {
			t.Errorf("ParseRetryAfter(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
