package copilot

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"github.com/meatlover/modelpick/internal/adapter"
)

const loginHint = "run `opencode auth login` and pick GitHub Copilot"

var (
	errAuthShape  = errors.New("auth.json is not the expected JSON")
	errAuthNotOAu = errors.New("github-copilot entry in auth.json is not an OAuth login")
)

// credential is the in-memory GitHub token. It never leaves this package
// except in the Authorization header of the usage request.
type credential struct{ token string }

// load reads the github-copilot entry of OpenCode's auth.json. The returned
// status never holds the token. Only that entry is decoded, and no error
// carries any of the file's content.
func (a *Adapter) load(_ adapter.Account) (credential, adapter.CredStatus) {
	if a.o.AuthFile == "" {
		return credential{}, adapter.CredStatus{Source: "OpenCode login", Err: errors.New("OpenCode login path unknown")}
	}
	source := "OpenCode login in " + a.o.AuthFile
	absent := adapter.CredStatus{Source: source + ": no GitHub Copilot login; " + loginHint}
	tok, found, err := tokenFromAuth(a.o.AuthFile)
	if err != nil {
		return credential{}, adapter.CredStatus{Source: source, Err: err}
	}
	if !found {
		return credential{}, absent
	}
	return credential{token: tok}, adapter.CredStatus{Present: true, Source: source}
}

// Credential reports whether a GitHub token exists. The token has no known
// expiry: a revoked one shows up as HTTP 401 from the usage call.
func (a *Adapter) Credential(acct adapter.Account) adapter.CredStatus {
	_, st := a.load(acct)
	return st
}

// tokenFromAuth returns the GitHub OAuth token: refresh, else access. found
// is false when the file, the entry or both tokens are absent.
func tokenFromAuth(path string) (token string, found bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err // *PathError: operation, path and errno only
	}
	var entries map[string]json.RawMessage
	if json.Unmarshal(b, &entries) != nil || entries == nil { // null decodes to a nil map
		return "", false, errAuthShape
	}
	raw, ok := entries["github-copilot"]
	if !ok {
		return "", false, nil
	}
	var e struct {
		Type    string `json:"type"`
		Refresh string `json:"refresh"`
		Access  string `json:"access"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Type != "oauth" {
		return "", false, errAuthNotOAu
	}
	if e.Refresh != "" {
		return e.Refresh, true, nil
	}
	return e.Access, e.Access != "", nil
}
