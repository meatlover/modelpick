package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// keychainTimeout bounds one `security` lookup so a stuck Keychain prompt
// cannot hang the caller.
const keychainTimeout = 20 * time.Second

const keychainBase = "Claude Code-credentials"

// KeychainService returns the Keychain service name Claude Code uses for
// configDir: the bare name for the default dir (home/.claude), otherwise the
// name plus the first 8 hex digits of sha256 of the cleaned absolute path.
func KeychainService(configDir, home string) string {
	if IsDefaultConfigDir(configDir, home) {
		return keychainBase
	}
	dir := filepath.Clean(configDir)
	sum := sha256.Sum256([]byte(dir))
	return keychainBase + "-" + hex.EncodeToString(sum[:])[:8]
}

func credentialsFile(configDir string) string {
	return filepath.Join(filepath.Clean(configDir), ".credentials.json")
}

// credential is the in-memory login. token never leaves this package except
// in the Authorization header of the usage request.
type credential struct {
	token        string
	expires      time.Time // access token's expiry; zero = unknown
	loginExpires time.Time // refresh token's expiry; zero = unknown
}

type credFile struct {
	ClaudeAiOauth *struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"` // epoch milliseconds
		// RefreshTokenExpiresAt is the refresh token's expiry, fixed at
		// sign-in. Only the date is read; the refresh token itself is not.
		RefreshTokenExpiresAt int64 `json:"refreshTokenExpiresAt"` // epoch milliseconds, optional
	} `json:"claudeAiOauth"`
}

// errCredShape is returned for a credential blob that is not the expected JSON.
// It deliberately carries no detail: a JSON error can quote the input.
var errCredShape = errors.New("credential is not the expected claudeAiOauth JSON")

func parseCredential(blob []byte) (credential, error) {
	var f credFile
	if err := json.Unmarshal(blob, &f); err != nil {
		return credential{}, errCredShape
	}
	if f.ClaudeAiOauth == nil || f.ClaudeAiOauth.AccessToken == "" {
		return credential{}, errors.New("credential has no claudeAiOauth.accessToken")
	}
	c := credential{token: f.ClaudeAiOauth.AccessToken}
	if ms := f.ClaudeAiOauth.ExpiresAt; ms > 0 {
		c.expires = time.UnixMilli(ms)
	}
	if ms := f.ClaudeAiOauth.RefreshTokenExpiresAt; ms > 0 {
		c.loginExpires = time.UnixMilli(ms)
	}
	return c, nil
}

// load finds the account's credential: the Keychain item first, then
// <config_dir>/.credentials.json. The returned status never holds the token.
func (a *Adapter) load(ctx context.Context, acct adapter.Account) (credential, adapter.CredStatus) {
	service := KeychainService(acct.ConfigDir, a.o.Home)
	kcSource := "keychain: " + service
	kctx, cancel := context.WithTimeout(ctx, keychainTimeout)
	secret, found, err := a.o.Keychain(kctx, service)
	cancel()
	if err != nil {
		return credential{}, adapter.CredStatus{Source: kcSource, Err: err}
	}
	if found {
		return a.status(kcSource, []byte(secret))
	}

	path := credentialsFile(acct.ConfigDir)
	blob, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return credential{}, adapter.CredStatus{Source: fmt.Sprintf("not found in keychain: %s or file: %s", service, path)}
	}
	if err != nil {
		return credential{}, adapter.CredStatus{Source: "file: " + path, Err: err}
	}
	return a.status("file: "+path, blob)
}

func (a *Adapter) status(source string, blob []byte) (credential, adapter.CredStatus) {
	c, err := parseCredential(blob)
	if err != nil {
		return credential{}, adapter.CredStatus{Source: source, Err: err}
	}
	expired := !c.expires.IsZero() && !a.o.Now().Before(c.expires)
	return c, adapter.CredStatus{Present: true, Expired: expired, LoginExpires: c.loginExpires, Source: source}
}

// Credential reports where the login is and whether it has expired. Local
// only: it may run `security`, never the network.
func (a *Adapter) Credential(acct adapter.Account) adapter.CredStatus {
	_, st := a.load(context.Background(), acct)
	return st
}

// IsDefaultConfigDir reports whether configDir is Claude Code's default
// directory, home/.claude, after cleaning.
func IsDefaultConfigDir(configDir, home string) bool {
	return filepath.Clean(configDir) == filepath.Join(home, ".claude")
}
