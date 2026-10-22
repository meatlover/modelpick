package claude

import (
	"context"

	"github.com/meatlover/modelpick/internal/adapter"
)

// thinkingLevels are the effort levels every Claude model accepts.
var thinkingLevels = []string{"low", "medium", "high", "xhigh", "max"}

// modelIDs is the embedded model list; Claude has no model-listing call that
// works with a Claude Code login.
var modelIDs = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"}

// Models returns the embedded list. Each call returns fresh slices.
func (a *Adapter) Models(_ context.Context, _ adapter.Account) ([]adapter.Model, error) {
	out := make([]adapter.Model, len(modelIDs))
	for i, id := range modelIDs {
		out[i] = adapter.Model{ID: id, Thinking: append([]string(nil), thinkingLevels...)}
	}
	return out, nil
}
