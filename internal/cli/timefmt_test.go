package cli

import (
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/app/apptest"
)

func TestFmtLeft(t *testing.T) {
	now := apptest.Now
	h, m := time.Hour, time.Minute
	cases := []struct {
		d    time.Duration
		want string
	}{
		{h + 50*m, "in 1h 50m"}, {4 * h, "in 4h"}, {12 * m, "in 12m"},
		{0, "in 0m"}, {-5 * m, "in 0m"}, {23*h + 59*m + 59*time.Second, "in 23h 59m"},
		{60 * h, "in 2d 12h"}, {59*h + 31*m, "in 2d 12h"}, {72 * h, "in 3d"}, {192 * h, "in 8d"},
	}
	for _, c := range cases {
		if got := fmtLeft(now.Add(c.d), now); got != c.want {
			t.Errorf("fmtLeft(+%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFmtReset(t *testing.T) {
	now := apptest.Now
	h := time.Hour
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(h + 50*time.Minute), "Thu 13:50Z"},
		{now.Add(60 * h), "Sun 00:00Z"},
		{now.Add(7*24*h - time.Minute), "Thu 11:59Z"},
		{now.Add(7 * 24 * h), "Oct 29 12:00Z"},
		{now.Add(8 * 24 * h), "Oct 30 12:00Z"},
		{time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), "Nov 05 09:00Z"},
	}
	for _, c := range cases {
		if got := fmtReset(c.at, now); got != c.want {
			t.Errorf("fmtReset(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}
