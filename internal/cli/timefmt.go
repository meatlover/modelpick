package cli

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// fmtLeft is the time until resetsAt: under a day, floored to minutes
// ("in 1h 50m", "in 4h", "in 12m"); from a day, rounded to the nearest hour
// ("in 2d 12h", "in 3d"). A time already passed is "in 0m". It ports fmtLeft
// in internal/web/static/js/time.js.
func fmtLeft(resetsAt, now time.Time) string {
	d := resetsAt.Sub(now)
	if d < 0 {
		d = 0
	}
	if d < 24*time.Hour {
		m := int(d / time.Minute)
		return "in " + joinUnits(m/60, "h", m%60, "m", m < 60)
	}
	hours := int(math.Round(d.Hours()))
	return "in " + joinUnits(hours/24, "d", hours%24, "h", false)
}

// joinUnits is "<a><au> <b><bu>", leaving out a zero a, and a zero b unless
// zeroB says to keep it.
func joinUnits(a int, au string, b int, bu string, zeroB bool) string {
	var parts []string
	if a != 0 {
		parts = append(parts, strconv.Itoa(a)+au)
	}
	if b != 0 || zeroB {
		parts = append(parts, strconv.Itoa(b)+bu)
	}
	return strings.Join(parts, " ")
}

// fmtReset is the reset time in UTC: "Thu 13:50Z" less than 7 days from now,
// else "Oct 29 12:00Z". It ports fmtReset in time.js, with the zone suffix.
func fmtReset(resetsAt, now time.Time) string {
	if resetsAt.Sub(now) < 7*24*time.Hour {
		return resetsAt.UTC().Format("Mon 15:04") + "Z"
	}
	return resetsAt.UTC().Format("Jan 02 15:04") + "Z"
}
