package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/meatlover/modelpick/internal/adapter"
)

type modelsCache struct {
	Models []struct {
		Slug       string `json:"slug"`
		Visibility string `json:"visibility"`
		Levels     []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

// Models reads <codex_home>/models_cache.json, skipping hidden models.
func (a *Adapter) Models(_ context.Context, acct adapter.Account) ([]adapter.Model, error) {
	if acct.CodexHome == "" {
		return nil, errors.New("codex: no codex_home configured")
	}
	path := filepath.Join(filepath.Clean(acct.CodexHome), "models_cache.json")
	blob, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("codex: %s not found; run codex once to create it", path)
	}
	if err != nil {
		return nil, fmt.Errorf("codex: read models cache: %w", err)
	}
	var c modelsCache
	if err := json.Unmarshal(blob, &c); err != nil {
		return nil, fmt.Errorf("codex: %w: models_cache.json is not the expected JSON", adapter.ErrShape)
	}
	out := []adapter.Model{}
	for _, m := range c.Models {
		if m.Slug == "" || m.Visibility == "hide" {
			continue
		}
		mod := adapter.Model{ID: m.Slug}
		for _, l := range m.Levels {
			if l.Effort != "" {
				mod.Thinking = append(mod.Thinking, l.Effort)
			}
		}
		out = append(out, mod)
	}
	return out, nil
}
