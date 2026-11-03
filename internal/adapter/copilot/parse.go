package copilot

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

const premiumKey = "premium_interactions"

type usageJSON struct {
	QuotaResetDate string                        `json:"quota_reset_date"`
	QuotaSnapshots map[string]*quotaSnapshotJSON `json:"quota_snapshots"`
}

type quotaSnapshotJSON struct {
	PercentRemaining *float64 `json:"percent_remaining"`
	Unlimited        bool     `json:"unlimited"`
}

func shapeErr(detail string) error {
	return fmt.Errorf("copilot: %w: %s", adapter.ErrShape, detail)
}

// parseUsage maps the premium_interactions snapshot to one general window.
// Error messages never quote the body.
func parseUsage(body []byte) (adapter.Reading, error) {
	var u usageJSON
	if err := json.Unmarshal(body, &u); err != nil {
		return adapter.Reading{}, shapeErr("response is not the expected JSON object")
	}
	s := u.QuotaSnapshots[premiumKey]
	if s == nil {
		return adapter.Reading{}, shapeErr("no quota_snapshots." + premiumKey)
	}
	w := adapter.Window{Label: premiumKey}
	var r adapter.Reading
	switch {
	case s.Unlimited:
		r.Notes = append(r.Notes, premiumKey+" unlimited")
	case s.PercentRemaining == nil:
		return adapter.Reading{}, shapeErr(premiumKey + " has no percent_remaining")
	default:
		w.UsedPct = math.Min(100, math.Max(0, 100-*s.PercentRemaining))
	}
	if resets, err := time.Parse("2006-01-02", u.QuotaResetDate); err == nil {
		w.ResetsAt = resets.UTC()
		// The calendar month ending at the reset, so the window start lands
		// on the previous monthly reset.
		w.Length = w.ResetsAt.Sub(w.ResetsAt.AddDate(0, -1, 0))
	} else {
		r.Notes = append(r.Notes, "quota_reset_date missing or unparseable; window length unknown")
	}
	r.Windows = []adapter.Window{w}
	return r, nil
}
