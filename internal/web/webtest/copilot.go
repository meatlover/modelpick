package webtest

import "testing"

// copilotLocation is what the accounts tab shows for a copilot account,
// which has no locator fields: it reads OpenCode's own GitHub login.
const copilotLocation = "OpenCode login (GitHub Copilot)"

// addCopilot adds a vendor "copilot" account with no fields and expects
// it listed with that location and no fields.
func addCopilot(t *testing.T, h Harness) {
	b := h.New(t)
	mustApply(t, b, "account.add", map[string]any{"name": "copilot-1", "vendor": "copilot"})
	for _, a := range b.State().Accounts {
		if a.Name != "copilot-1" {
			continue
		}
		if a.Vendor != "copilot" || a.Location != copilotLocation || len(a.Fields) != 0 {
			t.Errorf("copilot-1: vendor %q location %q fields %v, want copilot, %q and none",
				a.Vendor, a.Location, a.Fields, copilotLocation)
		}
		return
	}
	t.Error("copilot-1 not listed after account.add")
}
