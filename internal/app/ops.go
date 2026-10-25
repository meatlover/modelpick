package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

// Refused is an operation the editor will not apply. Reason is shown to the
// user after "Not applied."; Uses is the number of choices that block a
// schedule removal (the page then offers to remove it anyway).
type Refused struct {
	Reason string
	Uses   int
}

func (r *Refused) Error() string { return r.Reason }

// OpEnv is what operations need beyond the file: the account's model list
// (first model and thinking defaults, offered checks) and the window labels
// of its saved reading for a model, used to name cap positions in refusals
// (nil or empty: positions are numbered).
type OpEnv struct {
	Models  func(account string) ([]adapter.Model, error)
	Windows func(account, model string) []string
}

// opArgs is every argument any operation takes, with the page's field names
// (the same JSON the demo back end reads).
type opArgs struct {
	Name    string          `json:"name"`
	Vendor  string          `json:"vendor"`
	Fields  map[string]any  `json:"fields"`
	Profile string          `json:"profile"`
	Index   int             `json:"index"`
	From    int             `json:"from"`
	To      int             `json:"to"`
	Expect  json.RawMessage `json:"expect"`
	Field   string          `json:"field"`
	Value   json.RawMessage `json:"value"`
	Force   bool            `json:"force"`
	Cells   [][2]int        `json:"cells"`
	On      bool            `json:"on"`
	// WithChoices lets account.remove take the account's choices with it.
	WithChoices bool `json:"with_choices"`
	// Indexes and Expects are group.create's chosen choices.
	Indexes []int             `json:"indexes"`
	Expects []json.RawMessage `json:"expects"`
	// Group is the flat index (before the op) of any member of a group:
	// choice.add adds a member to it, choice.move makes the moved choice a
	// member of it. nil: a plain choice.
	Group *int `json:"group"`
}

type opFunc func(doc *core.Doc, a opArgs, env OpEnv) error

var ops = map[string]opFunc{
	"settings.default_cap": opDefaultCap,
	"profile.add":          opProfileAdd,
	"profile.remove":       opProfileRemove,
	"choice.add":           opChoiceAdd,
	"choice.remove":        opChoiceRemove,
	"choice.move":          opChoiceMove,
	"choice.set":           opChoiceSet,
	"group.create":         opGroupCreate,
	"group.dissolve":       opGroupDissolve,
	"group.move":           opGroupMove,
	"schedule.add":         opScheduleAdd,
	"schedule.remove":      opScheduleRemove,
	"schedule.paint":       opSchedulePaint,
	"account.add":          opAccountAdd,
	"account.remove":       opAccountRemove,
}

// ApplyOp applies one page operation (spec §8.4) to doc. A refusal is a
// *Refused and leaves doc as it was. The result is not validated here: the
// editor encodes it and runs core.Parse and the doctor gate.
func ApplyOp(doc *core.Doc, op string, args json.RawMessage, env OpEnv) error {
	fn, ok := ops[op]
	if !ok {
		return refuse("Unknown operation %s", op)
	}
	var a opArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			return refuse("Bad arguments for %s", op)
		}
	}
	return fn(doc, a, env)
}

func refuse(format string, args ...any) error {
	return &Refused{Reason: fmt.Sprintf(format, args...)}
}

// capital upper-cases the kind word that opens a refusal.
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// checkNew refuses a name that breaks the name rule, is reserved or is taken.
func checkNew(kind, name string, taken bool) error {
	if !nameRE.MatchString(name) {
		return refuse("%s name %q: use a–z, 0–9 and -, starting with a letter or digit", capital(kind), name)
	}
	if kind == "account" && name == core.ReservedAccountName {
		return refuse("%s is a reserved account name", core.ReservedAccountName)
	}
	if taken {
		return refuse("%s %s already exists", capital(kind), name)
	}
	return nil
}

func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// rawText is a JSON value as the user sent it, for a refusal.
func rawText(raw json.RawMessage) string { return strings.TrimSpace(string(raw)) }

func opDefaultCap(doc *core.Doc, a opArgs, _ OpEnv) error {
	text := rawText(a.Value)
	var v float64
	if text == "null" || json.Unmarshal(a.Value, &v) != nil {
		return refuse("Global default cap: %s is not a number", text)
	}
	if v < 0 || v > 100 {
		return refuse("Global default cap: %s is outside 0–100%%", text)
	}
	if doc.Settings == nil {
		doc.Settings = map[string]float64{}
	}
	doc.Settings["default_cap"] = v
	return nil
}
