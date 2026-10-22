package codex

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

func authFile(codexHome string) string {
	return filepath.Join(filepath.Clean(codexHome), "auth.json")
}

// credential is the in-memory login. token and accountID never leave this
// package except in the headers of the usage request.
type credential struct {
	token     string
	accountID string
	expires   time.Time // zero = unknown
}

type authJSON struct {
	Tokens *struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

// errCredShape carries no detail: a JSON error can quote the input.
var errCredShape = errors.New("auth.json is not the expected JSON")

func parseCredential(blob []byte) (credential, error) {
	var f authJSON
	if err := json.Unmarshal(blob, &f); err != nil {
		return credential{}, errCredShape
	}
	if f.Tokens == nil || f.Tokens.AccessToken == "" {
		return credential{}, errors.New("auth.json has no tokens.access_token")
	}
	if f.Tokens.AccountID == "" {
		return credential{}, errors.New("auth.json has no tokens.account_id")
	}
	return credential{
		token:     f.Tokens.AccessToken,
		accountID: f.Tokens.AccountID,
		expires:   jwtExpiry(f.Tokens.AccessToken),
	}, nil
}

// jwtExpiry reads the exp claim of an unverified JWT. Anything undecodable is
// the zero time, which means "unknown", i.e. not expired.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}
	}
	var c struct {
		Exp json.Number `json:"exp"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return time.Time{}
	}
	f, err := c.Exp.Float64()
	if err != nil || f <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(f), 0)
}

// load reads <codex_home>/auth.json. The returned status never holds a secret.
func (a *Adapter) load(acct adapter.Account) (credential, adapter.CredStatus) {
	if acct.CodexHome == "" {
		return credential{}, adapter.CredStatus{Source: "no codex_home configured"}
	}
	path := authFile(acct.CodexHome)
	source := "file: " + path
	blob, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return credential{}, adapter.CredStatus{Source: fmt.Sprintf("not found: %s", path)}
	}
	if err != nil {
		return credential{}, adapter.CredStatus{Source: source, Err: err}
	}
	c, err := parseCredential(blob)
	if err != nil {
		return credential{}, adapter.CredStatus{Source: source, Err: err}
	}
	expired := !c.expires.IsZero() && !a.o.Now().Before(c.expires)
	return c, adapter.CredStatus{Present: true, Expired: expired, Source: source}
}

// Credential reports where the login is and whether it has expired. Local only.
func (a *Adapter) Credential(acct adapter.Account) adapter.CredStatus {
	_, st := a.load(acct)
	return st
}
