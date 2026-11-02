package live

import (
	"testing"

	"github.com/meatlover/modelpick/internal/core"
)

func TestWireAccountsOpencodeLogin(t *testing.T) {
	doc := &core.Doc{Accounts: map[string]core.DocAccount{
		"go-a": {Vendor: "opencode-go"},
		"go-1": {Vendor: "opencode-go", KeyFile: "~/k", KeyLine: 1},
		"go-2": {Vendor: "opencode-go", KeyFile: "~/k", KeyLine: 2},
	}}
	got := map[string]string{}
	for _, a := range wireAccounts(doc, map[string]string{"go-a": "OpenCode login in /x/auth.json"}) {
		got[a.Name] = a.Location + "|" + a.Source
		if a.Name == "go-a" && len(a.Fields) != 0 {
			t.Errorf("go-a fields: %v", a.Fields)
		}
	}
	want := map[string]string{
		"go-a": "OpenCode login|OpenCode login in /x/auth.json",
		"go-1": "~/k|key line 1 of 2",
		"go-2": "~/k|key line 2 of 2",
	}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("%s = %q, want %q", n, got[n], w)
		}
	}
}
