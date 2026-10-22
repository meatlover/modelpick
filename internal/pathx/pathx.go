// Package pathx holds pure path helpers shared by core and platform. It
// imports nothing from the module.
package pathx

import (
	"path/filepath"
	"strings"
)

// ExpandHome expands a leading "~" or "~/" to home, then cleans the path.
// Other paths are only cleaned. The empty path stays empty.
func ExpandHome(p, home string) string {
	if p == "" {
		return ""
	}
	if p == "~" {
		return filepath.Clean(home)
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return filepath.Clean(p)
}
