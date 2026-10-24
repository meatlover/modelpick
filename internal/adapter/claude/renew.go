package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/platform"
)

var _ adapter.Renewer = (*Adapter)(nil)

// maxReason caps the failure reason so a chatty stderr cannot flood a finding.
const maxReason = 200

// renewArgs is the cheapest non-interactive call: one word from Haiku, no
// session file, no settings, MCP servers or tools. Claude Code renews an
// expired login as a side effect of making any authenticated request.
var renewArgs = []string{"-p", "Reply with exactly one word: ok", "--model", "haiku",
	"--no-session-persistence", "--setting-sources", "", "--strict-mcp-config", "--tools", ""}

// renewDropped are variables that would make claude authenticate some other
// way than the account's own login, so they never reach the child.
var renewDropped = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
	"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"}

// Renew runs the official claude CLI against acct's config directory so that
// Claude Code renews the login itself. It runs in an empty temporary directory
// so claude finds no project files, and modelpick writes nothing of its own.
// The caller re-reads the credential to learn whether it worked, so success
// here is only "claude exited 0".
func (a *Adapter) Renew(ctx context.Context, acct adapter.Account) error {
	dir, err := os.MkdirTemp("", "modelpick-renew-")
	if err != nil {
		return fmt.Errorf("claude: renew: %w", err)
	}
	defer os.RemoveAll(dir)

	env := platform.EnvWithout(a.o.Environ(), append([]string{"CLAUDE_CONFIG_DIR"}, renewDropped...)...)
	if !IsDefaultConfigDir(acct.ConfigDir, a.o.Home) {
		abs, err := filepath.Abs(acct.ConfigDir)
		if err != nil {
			return fmt.Errorf("claude: config dir: %w", err)
		}
		env = append(env, "CLAUDE_CONFIG_DIR="+abs)
	}

	_, stderr, err := a.o.RunCmd(ctx, platform.Cmd{Name: "claude", Args: renewArgs, Env: env, Dir: dir})
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return errors.New("claude: claude not found on PATH")
	}
	return errors.New(clip(fmt.Sprintf("claude: %v: %s", err, firstLine(stderr)), maxReason))
}

func firstLine(b []byte) string {
	s, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(s)
}

// clip cuts s to at most n bytes without splitting a UTF-8 sequence.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
