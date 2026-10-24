package app

import (
	"io"
	"log"
	"testing"

	"github.com/meatlover/modelpick/internal/core"
)

// SetTestOp registers an operation for one test, for doc states no real
// operation can produce (a second identical doctor error, a Doc core.Parse
// rejects), so the editor's gate and Parse fallback stay covered. It writes
// the package-level ops map: a test that uses it must not call t.Parallel.
func SetTestOp(t *testing.T, name string, fn func(doc *core.Doc)) {
	t.Helper()
	if _, real := ops[name]; real {
		t.Fatalf("SetTestOp: %s is a real operation", name)
	}
	ops[name] = func(doc *core.Doc, _ opArgs, _ OpEnv) error {
		fn(doc)
		return nil
	}
	t.Cleanup(func() { delete(ops, name) })
}

// CapturePanicLog sends the adapter-panic log to w until the test ends.
func CapturePanicLog(t *testing.T, w io.Writer) {
	t.Helper()
	old := panicLog
	panicLog = log.New(w, "", 0)
	t.Cleanup(func() { panicLog = old })
}
