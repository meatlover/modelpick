package copilot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/meatlover/modelpick/internal/adapter"
)

// maxToolErr bounds how much of the tool's own error line a message carries.
const maxToolErr = 200

// Models runs `opencode models github-copilot` and returns one model per
// non-empty output line. Copilot models take no thinking level here.
func (a *Adapter) Models(ctx context.Context, _ adapter.Account) ([]adapter.Model, error) {
	out, err := a.o.Run(ctx, "opencode", "models", "github-copilot")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			msg := fmt.Sprintf("copilot: opencode models exited with status %d", ee.ExitCode())
			if line := lastLine(ee.Stderr); line != "" {
				msg += ": " + line
			}
			return nil, errors.New(msg)
		}
		return nil, fmt.Errorf("copilot: run opencode models: %w", err)
	}
	models := []adapter.Model{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		if id := strings.TrimSpace(ansiEscape.ReplaceAllString(string(line), "")); id != "" {
			models = append(models, adapter.Model{ID: id})
		}
	}
	return models, nil
}

// ansiEscape matches a terminal colour or cursor sequence.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// lastLine is the last non-blank line of b: a tool's final stderr line is
// usually its error. It is shown in doctor and on the page, so colour codes
// and other control characters are dropped, anything token-like is hidden,
// and it is cut to maxToolErr runes.
func lastLine(b []byte) string {
	text := ansiEscape.ReplaceAllString(string(b), "")
	text = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, text)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	line := adapter.HideTokens(strings.TrimSpace(lines[len(lines)-1]))
	if r := []rune(line); len(r) > maxToolErr {
		line = string(r[:maxToolErr]) + "…"
	}
	return line
}
