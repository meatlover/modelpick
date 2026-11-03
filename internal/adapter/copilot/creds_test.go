package copilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
)

func TestCredentialFromAuthFile(t *testing.T) {
	const secret = "gho_SECRETSECRET"
	cases := []struct {
		name    string
		content *string // nil = no file
		present bool
		err     bool
		token   string
		srcHas  string
	}{
		{"refresh used", p(`{"github-copilot":{"type":"oauth","refresh":"gho_SECRETSECRET","access":"acc","expires":0}}`), true, false, secret, ""},
		{"access fallback", p(`{"github-copilot":{"type":"oauth","refresh":"","access":"gho_SECRETSECRET"}}`), true, false, secret, ""},
		{"both empty", p(`{"github-copilot":{"type":"oauth","refresh":"","access":""}}`), false, false, "", "opencode auth login"},
		{"entry missing", p(`{"opencode-go":{"type":"api","key":"k"}}`), false, false, "", "opencode auth login"},
		{"file missing", nil, false, false, "", "opencode auth login"},
		{"wrong type", p(`{"github-copilot":{"type":"api","key":"gho_SECRETSECRET"}}`), false, false, "", "opencode auth login"},
		{"malformed", p(`{"github-copilot": gho_SECRETSECRET`), false, true, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			if c.content != nil {
				if err := os.WriteFile(path, []byte(*c.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a := New(Options{AuthFile: path})
			cred, st := a.load(adapter.Account{Vendor: "copilot"})
			if st.Present != c.present || (st.Err != nil) != c.err || st.Expired || !st.LoginExpires.IsZero() {
				t.Fatalf("status = %+v", st)
			}
			if cred.token != c.token {
				t.Fatalf("token mismatch")
			}
			if c.srcHas != "" && !strings.Contains(st.Source, c.srcHas) {
				t.Fatalf("Source = %q, want %q in it", st.Source, c.srcHas)
			}
			text := st.Source
			if st.Err != nil {
				text += st.Err.Error()
			}
			if strings.Contains(text, "SECRET") {
				t.Fatalf("secret leaked: %q", text)
			}
			if got := a.Credential(adapter.Account{}); got.Present != c.present {
				t.Fatalf("Credential.Present = %v", got.Present)
			}
		})
	}
}

func TestCredentialNoAuthPath(t *testing.T) {
	st := New(Options{}).Credential(adapter.Account{})
	if st.Present || st.Err == nil {
		t.Fatalf("status = %+v, want an error", st)
	}
}

func p(s string) *string { return &s }
