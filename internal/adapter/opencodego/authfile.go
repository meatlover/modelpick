package opencodego

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// AuthPath is where OpenCode keeps its login: $XDG_DATA_HOME/opencode/auth.json
// when XDG_DATA_HOME is absolute (a relative value is ignored, as the XDG
// spec says), else <home>/.local/share/opencode/auth.json.
func AuthPath(getenv func(string) string, home string) string {
	if x := getenv("XDG_DATA_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

var (
	errAuthShape  = errors.New("auth.json is not the expected JSON")
	errAuthNotAPI = errors.New("opencode-go entry in auth.json is not an API key")
)

// keyFromAuth returns the opencode-go API key in OpenCode's auth.json. found
// is false when the file or the entry is absent. The file also holds other
// providers' OAuth tokens, so only the opencode-go entry is decoded and no
// error carries any of the file's content.
func keyFromAuth(path string) (key string, found bool, err error) {
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
	raw, ok := entries["opencode-go"]
	if !ok {
		return "", false, nil
	}
	var e struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Type != "api" || e.Key == "" {
		return "", false, errAuthNotAPI
	}
	return e.Key, true, nil
}
