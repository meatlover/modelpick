// Package web is the HTTP surface of the v2 page: wire types, the Backend
// seam, authentication and static files. It imports no module package.
//
// Back-end contract: what the page relies on from every Backend (final
// review of the UI branch, items 1–18; internal/web/webtest checks it on
// the demo and the live back end).
//
//  1. UsageRow.State is one of "ok", "expired token", "rate-limited until
//     <RFC 3339>", "rate-limited (no retry time given)", "error: <text>"
//     (the v1 ls states) or "no reading yet" (nothing saved, nothing
//     blocking a first reading).
//  2. CanRefresh is false only while rate-limited or the token is expired
//     and cannot be renewed;
//     true with no reading.
//  3. ReadingAgeSeconds null means no reading (and no windows).
//  4. State.Windows[account][model] is absent without a saved reading (the
//     page then numbers the cap boxes); it is recomputed on every State, so
//     a Refresh or account.add shows on the next reload.
//  5. A Refused reason is shown verbatim as a fragment; a schedule.remove
//     with Uses > 0 opens a confirm; a refresh refusal (unknown account,
//     read-only config) leads with the account name.
//  6. Doctor subjects: an account name, "profile <p>", "profile <p>,
//     choice <n>" (1-based), "schedule <name>" or "config"; only "error"
//     reddens; Errors and Warnings count Findings. A finding with
//     `accounts` covers each named account; its subject is their names
//     joined by `, `.
//  7. choice ops compare expect after normalising null vs [] lists and 90
//     vs 90.0; a single identical choice elsewhere is used (spec §8.4).
//  8. skip_during/allow_during [] means none: the key is dropped.
//  9. Choice.Cap: null uncapped; elements 90, "pace+10" or "default";
//     trailing "default" elements are trimmed on the way out and accepted
//     on the way in (owner, round 5; supersedes "store as sent").
//  10. While ReadOnly != "", every op is refused; State still has non-null
//     lists and a sha256.
//  11. No null list; only cap, thinking, scope, resets_at,
//     reading_age_seconds and load_balance may be null (the server also
//     normalises).
//  12. Accounts, schedules, profiles and usage rows are sorted by name.
//  13. Conflict is HTTP 409 (the page reloads); 200 carries the new
//     sha256; refresh answers the account's full row whatever the call's
//     outcome, which its state carries (expired token, rate-limited until,
//     error: …).
//  14. GET /api/ping answers 204 (also with ?t=<token> and no cookie);
//     any 401 reads as stopped.
//  15. Account.Fields holds locator keys only (config_dir, codex_home,
//     key_file, key_line as a number), as written; Location and Source are
//     display strings. No response ever holds a credential.
//  16. A model whose Thinking is [] takes a null choice thinking.
//  17. Times are RFC 3339 UTC; resets_at is "" when unknown.
//  18. `POST /api/doctor/all` runs every check, including at most one usage
//     call per account without a fresh reading; nothing else on the page
//     causes a usage call except Refresh and the background first reading
//     of an account that has none.
//  19. Load balancing groups (spec §8). A profile's choices stay one flat
//     list in file order; a member has group > 0 (ids 1, 2, … in order,
//     contiguous) and load_balance its algorithm; a plain choice has group
//     0 and load_balance null. No group has fewer than 2 members. State
//     .algorithms is core.AlgorithmNames() (sorted). Choices are labelled
//     "2" or "2a", "2b", … by position; refusals and doctor subjects use
//     the label, except "Profile p has no choice N" (N = index+1, no label
//     exists) and group.move's "Profile p has no position N".
//  20. Group ops. Indexes are flat, taken before the op. A choice's expect
//     is the choice as the state gave it: plain when group is absent/0/null
//     or load_balance absent/""/null, else a member, which matches only a
//     member of a group with that algorithm (ids are not identity; this
//     holds for the "one identical choice elsewhere" fallback too). A
//     group's expect is {"load_balance", "choices": [<choice>, …]}, found
//     at index or as the one identical group elsewhere.
//     - member_of: choice.set, choice.remove and choice.move on a member
//     send member_of, the member's group as the page saw it (a group's
//     expect, resolved the same way from the member's index; refused when
//     it resolves to no group). A member's expect then matches only inside
//     that group, at index or as the one identical member there. Without
//     member_of a member's expect matches only at index. A plain expect
//     ignores member_of and may fall back to any plain choice.
//     - group.create {profile, indexes, expects}: 2+ plain choices; the
//     group, algorithm random, takes the place of the first in file order.
//     - group.dissolve {profile, index, expect}: members turn plain in place.
//     - group.move {profile, index, expect, to}: to counts positions
//     without the group (0 … that count); the group goes before it.
//     - choice.add {profile, group?, group_expect?}: with group (any
//     member's index; group_expect required) a member is appended to that
//     group on the first unused account of the last member's vendor, else
//     of any vendor, else the last member's; without, a plain choice goes
//     before a trailing position that always wins (an uncapped choice, or
//     a group with an uncapped member), else last.
//     - choice.move {profile, from, to, expect, member_of?, group?,
//     group_expect?}: member_of is the moved member's own group; group and
//     group_expect the destination's. Without group the card ends plain and
//     may not land strictly inside a group; with it, it joins that group
//     and lands inside it or at an edge.
//     - choice.remove, choice.move, group ops and account.remove
//     with_choices leave a one-member group plain in place, and drop an
//     empty one.
//
// Errors other than Conflict and Refused are internal: the server answers
// 500 "internal error" (422 "<account>: refresh failed" for a refresh) and
// logs the detail to stderr, since it can hold a path or a vendor's words.
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
	// Algorithms are the load balancing algorithms a group can name,
	// sorted (core.AlgorithmNames).
	Algorithms []string `json:"algorithms"`
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
	// Group is 0 for a plain choice; the members of one load balancing
	// group share an id, 1, 2, … in file order (contract 19).
	Group int `json:"group"`
	// LoadBalance is the member's group's algorithm; null when plain.
	LoadBalance *string `json:"load_balance"`
}

// UsageRow is one account's usage reading.
type UsageRow struct {
	Account           string        `json:"account"`
	Vendor            string        `json:"vendor"`
	State             string        `json:"state"`
	ReadingAgeSeconds *int          `json:"reading_age_seconds"`
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

// Doctor is GET /api/doctor and POST /api/doctor/all.
type Doctor struct {
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
	Findings []Finding `json:"findings"`
}

// Finding is one doctor finding. Accounts is set only on a grouped finding
// (one cause on two or more accounts; contract 6).
type Finding struct {
	Severity string   `json:"severity"`
	Subject  string   `json:"subject"`
	Message  string   `json:"message"`
	Accounts []string `json:"accounts,omitempty"`
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
	Doctor() Doctor    // local checks only: never a vendor usage call
	DoctorAll() Doctor // Doctor after the usage calls of contract 18
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
