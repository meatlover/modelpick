package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/core"
)

// jsonBlock matches a fenced json block whose body is a config document, i.e.
// starts with `{` and then a "version" key (on the same or the next line).
var jsonBlock = regexp.MustCompile("(?s)```json\n(\\{\\s*\"version\".*?)\n```")

func TestDocsExampleMatchesFile(t *testing.T) {
	doc, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := jsonBlock.FindAllStringSubmatch(string(doc), -1)
	if len(blocks) == 0 {
		t.Fatal("docs/configuration.md has no ```json config block")
	}
	for i, b := range blocks {
		if _, err := core.Parse([]byte(b[1]), "docs/configuration.md", "/home/u"); err != nil {
			t.Errorf("config block %d does not parse: %v", i+1, err)
		}
	}
	// The running example must be the shipped example, byte for byte.
	example, err := os.ReadFile("../../examples/config.json")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(example))
	found := false
	for _, b := range blocks {
		found = found || strings.TrimSpace(b[1]) == want
	}
	if !found {
		t.Error("no config block in docs/configuration.md equals examples/config.json")
	}
}
