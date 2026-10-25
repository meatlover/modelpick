package demo

import (
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/web"
	"github.com/meatlover/modelpick/internal/web/webtest"
)

// TestConformance runs the shared back-end suite against the demo.
func TestConformance(t *testing.T) {
	var d *Demo
	webtest.Run(t, webtest.Harness{
		New: func(*testing.T) web.Backend {
			d = New(func() time.Time { return t0 }, core.AlgorithmNames())
			d.delay = 0
			return d
		},
		HandEdit: func(*testing.T) { d.HandEdit() },
		Break:    func(*testing.T) { d.SetBroken("config: version: must be 1") },
		Fix:      func(*testing.T) { d.SetBroken("") },
		BadThinking: func(*testing.T) (string, int) {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.profiles[0].Choices[0].Thinking = str("ultra")
			return d.profiles[0].Name, 0
		},
		// The demo holds no credential; this stands for one.
		Secrets:     []string{"sk-demo-never-held"},
		Refreshable: "claude-main",
		Algorithms:  core.AlgorithmNames(),
		UsageCalls:  func() int { return 0 }, // the demo calls no vendor
		MissingTool: func(*testing.T) []string {
			d.SetToolMissing(true)
			return []string{"go-1", "go-2", "go-3"}
		},
	})
}
