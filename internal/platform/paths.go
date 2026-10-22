// Package platform holds OS-facing helpers. It imports nothing from the module.
package platform

import (
	"path/filepath"
)

// ConfigPath returns $MODELPICK_CONFIG, else $XDG_CONFIG_HOME/modelpick/config.json,
// else ~/.config/modelpick/config.json.
func ConfigPath(getenv func(string) string, home string) string {
	if p := getenv("MODELPICK_CONFIG"); p != "" {
		return p
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "modelpick", "config.json")
	}
	return filepath.Join(home, ".config", "modelpick", "config.json")
}

// CacheDir returns $XDG_CACHE_HOME/modelpick, else ~/.cache/modelpick.
func CacheDir(getenv func(string) string, home string) string {
	if x := getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "modelpick")
	}
	return filepath.Join(home, ".cache", "modelpick")
}
