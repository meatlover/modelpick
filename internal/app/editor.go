package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
)

// Snapshot is config.json as read at one moment. ReadOnly is the parse error
// of a file that does not parse ("" when it parses); Doc and Config are then
// nil or partial and no operation is accepted.
type Snapshot struct {
	Bytes    []byte
	SHA256   string
	Doc      *core.Doc
	Config   *core.Config
	ReadOnly string
}

// Conflict is an operation sent against a version of the file that is no
// longer current; SHA256 is the current one (spec §8.4 "Conflicts").
type Conflict struct{ SHA256 string }

func (c *Conflict) Error() string { return "config.json changed since it was loaded" }

const (
	backupName       = "config.before-ui.json"
	reasonReadOnly   = "config.json does not parse; fix it by hand"
	reasonBackupFail = "Could not save the safety copy; config.json is unchanged"
)

// Editor applies page operations to config.json (spec §8.4): every
// operation re-reads the file, is checked against the sha the page saw, runs
// on a fresh decode, and is written only if core.Parse accepts the result
// and the doctor finds no config error beyond the current file's (see
// newError). One mutex serialises Load and Apply.
type Editor struct {
	// OnAccountAdded, if set, is started in its own goroutine after an
	// account.add is written, so the account's first reading happens in
	// the background and never on the request path.
	OnAccountAdded func(name string)

	d        Deps
	path     string
	cacheDir string
	mu       sync.Mutex
	checks   CheckCache
	backedUp bool // the safety copy has been written by this Editor
}

// NewEditor edits the config file at path; the safety copy and the usage
// store live under the cache dir of d.
func NewEditor(d Deps, path string) *Editor {
	return &Editor{d: d, path: path, cacheDir: platform.CacheDir(d.Getenv, d.Home)}
}

// Checks is the per-account Check and Models cache the gate uses; the
// caller forgets entries on page load or focus.
func (e *Editor) Checks() *CheckCache { return &e.checks }

// Load reads the file. An unreadable file is an error; a file that does not
// parse is a Snapshot with ReadOnly set.
func (e *Editor) Load() (Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.load()
}

func (e *Editor) load() (Snapshot, error) {
	data, err := os.ReadFile(e.path)
	if err != nil {
		return Snapshot{}, err
	}
	return e.snapshot(data), nil
}

func (e *Editor) snapshot(data []byte) Snapshot {
	sum := sha256.Sum256(data)
	s := Snapshot{Bytes: data, SHA256: hex.EncodeToString(sum[:])}
	s.Doc, _ = core.ParseDoc(data)
	cfg, err := core.Parse(data, e.path, e.d.Home)
	if err != nil {
		s.ReadOnly = err.Error()
		return s
	}
	s.Config = cfg
	return s
}

// Apply runs one operation against the file whose sha the page last saw.
// The error is a *Conflict (the file changed; nothing applied), a *Refused
// (nothing written), or an I/O failure. On any error the returned Snapshot
// is the current file (zero if it could not be read).
func (e *Editor) Apply(base, op string, args json.RawMessage) (Snapshot, []Finding, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur, err := e.load()
	if err != nil {
		return Snapshot{}, nil, err
	}
	if base != cur.SHA256 {
		return cur, nil, &Conflict{SHA256: cur.SHA256}
	}
	if cur.ReadOnly != "" {
		return cur, nil, &Refused{Reason: reasonReadOnly}
	}
	account := accountOpName(op, args)
	data, findings, err := e.candidate(cur, op, args, account)
	if err != nil {
		return cur, nil, err
	}
	if err := e.write(cur.Bytes, data); err != nil {
		return cur, nil, err
	}
	if account != "" {
		e.checks.Forget(account)
		if op == "account.add" && e.OnAccountAdded != nil {
			go e.OnAccountAdded(account)
		}
	}
	return e.snapshot(data), findings, nil
}

// candidate applies op to a fresh decode of the current bytes (so nothing a
// refused operation did can leak) and runs the gate; it returns the bytes to
// write and their findings.
func (e *Editor) candidate(cur Snapshot, op string, args json.RawMessage, account string) ([]byte, []Finding, error) {
	doc, err := core.ParseDoc(cur.Bytes)
	if err != nil { // cur parsed a moment ago from the same bytes
		return nil, nil, err
	}
	if err := ApplyOp(doc, op, args, e.opEnv(cur.Config)); err != nil {
		return nil, nil, err
	}
	data := doc.Encode()
	// No path: the candidate is not the file, so the error must not name it.
	cand, err := core.Parse(data, "", e.d.Home)
	if err != nil {
		return nil, nil, &Refused{Reason: capital(err.Error())}
	}
	cand.Path = e.path
	if op == "account.add" && account != "" {
		e.checks.Forget(account) // a stale entry from a refused add of this name
	}
	before := doctor(cur.Config, e.d, &e.checks)
	after := doctor(cand, e.d, &e.checks)
	if f, ok := newError(before, after); ok {
		return nil, nil, &Refused{Reason: findingReason(f)}
	}
	return data, after.list, nil
}

// newError is the first error finding of after in excess of before. Errors
// are compared as a multiset of (subject, message) with a choice's position
// dropped from the subject, so moving, inserting or removing choices does not
// turn an existing error into a new one, while a second copy of it is new.
// Findings from an adapter's Check (binary, credential) are machine state,
// not config validity, and never block an edit.
func newError(before, after findings) (Finding, bool) {
	type key struct{ subject, message string }
	gated := func(fs findings, f Finding) bool { return f.Severity == sevError && !fs.fromCheck[f] }
	count := map[key]int{}
	for _, f := range before.list {
		if gated(before, f) {
			count[key{gateSubject(f.Subject), f.Message}]++
		}
	}
	for _, f := range after.list {
		if !gated(after, f) {
			continue
		}
		k := key{gateSubject(f.Subject), f.Message}
		if count[k] == 0 {
			return f, true
		}
		count[k]--
	}
	return Finding{}, false
}

// gateSubject drops the choice position: "profile p, choice 3" -> "profile p".
func gateSubject(subject string) string {
	if strings.HasPrefix(subject, "profile ") {
		if i := strings.Index(subject, ", choice "); i >= 0 {
			return subject[:i]
		}
	}
	return subject
}

// findingReason is "<subject>: <message>", capitalised unless the subject is
// an account name (wording rule: a fragment starting with a name keeps it).
func findingReason(f Finding) string {
	s := f.Subject + ": " + f.Message
	for _, kind := range []string{"profile ", "schedule ", "config"} {
		if strings.HasPrefix(f.Subject, kind) {
			return capital(s)
		}
	}
	return s
}

// accountOpName is the account an account.add or account.remove names; ""
// for every other operation.
func accountOpName(op string, args json.RawMessage) string {
	if op != "account.add" && op != "account.remove" {
		return ""
	}
	var a struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(args, &a) // bad arguments are ApplyOp's refusal
	return a.Name
}

// write saves the safety copy once per Editor, then replaces the file.
func (e *Editor) write(old, data []byte) error {
	if !e.backedUp {
		if err := e.backup(old); err != nil {
			return &Refused{Reason: reasonBackupFail}
		}
		e.backedUp = true
	}
	return platform.WriteFileAtomic(e.path, data)
}

func (e *Editor) backup(old []byte) error {
	if err := os.MkdirAll(e.cacheDir, 0o700); err != nil {
		return err
	}
	p := filepath.Join(e.cacheDir, backupName)
	if err := platform.WriteFileAtomic(p, old); err != nil {
		return err
	}
	return os.Chmod(p, 0o600) // WriteFileAtomic keeps an older copy's mode
}

// opEnv lists models through the check cache (once per account until
// forgotten) and names cap positions by the saved reading's window labels.
func (e *Editor) opEnv(cfg *core.Config) OpEnv {
	env := NewEnv(e.d, cfg, false)
	store := NewStore(e.d, cfg)
	return OpEnv{
		Models: func(account string) ([]adapter.Model, error) {
			acct, a, err := env.AdapterFor(account)
			if err != nil {
				return nil, UsageError(acct.Vendor, err)
			}
			r := e.checks.check(account, acct, a)
			if r.modelsErr != nil {
				return nil, UsageError(acct.Vendor, r.modelsErr)
			}
			return r.models, nil
		},
		Windows: func(account, model string) []string {
			s, err := store.Saved(cfg.Accounts[account])
			if err != nil || s.Reading == nil {
				return nil
			}
			var labels []string
			for _, w := range core.OrderWindows(s.Reading.Windows, model) {
				labels = append(labels, w.Label)
			}
			return labels
		},
	}
}
