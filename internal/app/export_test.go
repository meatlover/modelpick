package app

import (
	"testing"

	"github.com/meatlover/modelpick/internal/core"
)

// SetTestOp registers an operation for one test, for doc states no real
// operation can produce (a second identical doctor error, a Doc core.Parse
// rejects), so the editor's gate and Parse fallback stay covered.
func SetTestOp(t *testing.T, name string, fn func(doc *core.Doc)) {
	t.Helper()
	ops[name] = func(doc *core.Doc, _ opArgs, _ OpEnv) error {
		fn(doc)
		return nil
	}
	t.Cleanup(func() { delete(ops, name) })
}
