// Package web is the HTTP surface of the v2 page: wire types, the Backend
// seam, authentication and static files. It imports no module package.
package web

import "encoding/json"

// State is GET /api/state.
type State struct {
	SHA256     string                         `json:"sha256"`
	Path       string                         `json:"path"`
	ReadOnly   string                         `json:"read_only"`
	DefaultCap float64                        `json:"default_cap"`
	Accounts   []Account                      `json:"accounts"`
	Models     map[string][]Model             `json:"models"`
	Windows    map[string]map[string][]string `json:"windows"`
	Schedules  []Schedule                     `json:"schedules"`
	Profiles   []Profile                      `json:"profiles"`
}

// Account is one configured account.
type Account struct {
	Name     string         `json:"name"`
	Vendor   string         `json:"vendor"`
	Fields   map[string]any `json:"fields"`
	Location string         `json:"location"`
	Source   string         `json:"source"`
}

// Model is one model an account offers.
type Model struct {
	ID       string   `json:"id"`
	Thinking []string `json:"thinking"`
}

// Schedule holds, per UTC day (0 = Sunday) and hour, the covered fraction 0-1.
type Schedule struct {
	Name  string         `json:"name"`
	Cells [7][24]float64 `json:"cells"`
}

// Profile is an ordered list of choices.
type Profile struct {
	Name    string   `json:"name"`
	Choices []Choice `json:"choices"`
}

// Choice is one account/model pick. Cap nil means uncapped (JSON null); an
// empty non-nil slice means every position uses the default cap.
type Choice struct {
	Account     string   `json:"account"`
	Model       string   `json:"model"`
	Thinking    *string  `json:"thinking"`
	Cap         []any    `json:"cap"`
	SkipDuring  []string `json:"skip_during"`
	AllowDuring []string `json:"allow_during"`
}

// UsageRow is one account's usage reading.
type UsageRow struct {
	Account           string        `json:"account"`
	Vendor            string        `json:"vendor"`
	State             string        `json:"state"`
	ReadingAgeSeconds int           `json:"reading_age_seconds"`
	CanRefresh        bool          `json:"can_refresh"`
	Windows           []UsageWindow `json:"windows"`
}

// UsageWindow is one usage window of a row.
type UsageWindow struct {
	Label    string  `json:"label"`
	Length   string  `json:"length"`
	UsedPct  float64 `json:"used_pct"`
	ResetsAt string  `json:"resets_at"`
	Scope    *string `json:"scope"`
}

// Doctor is GET /api/doctor.
type Doctor struct {
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
	Findings []Finding `json:"findings"`
}

// Finding is one doctor finding.
type Finding struct {
	Severity string `json:"severity"`
	Subject  string `json:"subject"`
	Message  string `json:"message"`
}

// OpRequest is the POST /api/op body.
type OpRequest struct {
	Base string          `json:"base"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

// OpResult is a successful op's answer.
type OpResult struct {
	SHA256   string    `json:"sha256"`
	Findings []Finding `json:"findings"`
}

// Backend is what the page talks to.
type Backend interface {
	State() State
	Usage() []UsageRow
	Refresh(account string) (UsageRow, error)
	Doctor() Doctor
	Apply(req OpRequest) (OpResult, error)
}

// Conflict reports that the config changed under the page (HTTP 409).
type Conflict struct{ SHA256 string }

func (c Conflict) Error() string { return "config changed: " + c.SHA256 }

// Refused reports an op the backend will not perform (HTTP 422). Uses is the
// number of references that block a schedule removal; 0 omits it.
type Refused struct {
	Reason string
	Uses   int
}

func (r Refused) Error() string { return r.Reason }
