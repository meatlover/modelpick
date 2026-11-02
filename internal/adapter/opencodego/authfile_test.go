package opencodego

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
)

const (
	fakeAuthKey   = "FAKE-AUTH-KEY-never-real-dd44"
	fakeRefresh   = "FAKE-REFRESH-never-real-ee55"
	fakeAccess    = "FAKE-ACCESS-never-real-ff66"
	fakeBadEntry  = "FAKE-BADENTRY-never-real-gg77"
	authAcctName  = "go"
	authAcctVen   = "opencode-go"
	notJSONText   = "auth.json is not the expected JSON"
	notAPIKeyText = "opencode-go entry in auth.json is not an API key"
)

func writeAuth(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAuthPath(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "XDG_DATA_HOME" {
				return v
			}
			return ""
		}
	}
	for _, tc := range []struct{ name, xdg, want string }{
		{"absolute xdg", "/x", "/x/opencode/auth.json"},
		{"unset", "", "/h/.local/share/opencode/auth.json"},
		{"relative xdg ignored", "rel", "/h/.local/share/opencode/auth.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AuthPath(env(tc.xdg), "/h"); got != tc.want {
				t.Fatalf("AuthPath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAuthFileCredential(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	for _, tc := range []struct {
		name    string
		path    string
		body    string
		present bool
		errText string
	}{
		{name: "absent file", path: missing},
		{name: "other providers only", body: `{"anthropic":{"type":"oauth","access":"` + fakeAccess + `"}}`},
		{name: "api key", body: `{"opencode-go":{"type":"api","key":"` + fakeAuthKey + `"}}`, present: true},
		{name: "array", body: `[]`, errText: notJSONText},
		{name: "null", body: `null`, errText: notJSONText},
		{name: "not json", body: `not json`, errText: notJSONText},
		{name: "oauth entry", body: `{"opencode-go":{"type":"oauth","access":"x"}}`, errText: notAPIKeyText},
		{name: "oauth entry with a key", body: `{"opencode-go":{"type":"oauth","key":"` + fakeBadEntry + `"}}`, errText: notAPIKeyText},
		{name: "empty key", body: `{"opencode-go":{"type":"api","key":""}}`, errText: notAPIKeyText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.path
			if p == "" {
				p = writeAuth(t, tc.body)
			}
			a := New(Options{AuthFile: p})
			st := a.Credential(adapter.Account{Name: authAcctName, Vendor: authAcctVen})
			if st.Source != "OpenCode login in "+p {
				t.Errorf("Source = %q", st.Source)
			}
			if st.Present != tc.present {
				t.Errorf("Present = %v, want %v", st.Present, tc.present)
			}
			switch {
			case tc.errText == "" && st.Err != nil:
				t.Errorf("Err = %v, want nil", st.Err)
			case tc.errText != "" && (st.Err == nil || st.Err.Error() != tc.errText):
				t.Errorf("Err = %v, want %q", st.Err, tc.errText)
			}
		})
	}
}

func TestUsageFromAuthFile(t *testing.T) {
	f := newFixture(t, serveBody(200, `{"usage":{"rolling":{"status":"ok","percent":3}}}`))
	p := writeAuth(t, `{"opencode-go":{"type":"api","key":"`+fakeAuthKey+`"}}`)
	f.a.o.AuthFile = p
	f.acct = adapter.Account{Name: authAcctName, Vendor: authAcctVen}
	if _, err := f.a.Usage(context.Background(), f.acct); err != nil {
		t.Fatal(err)
	}
	if f.hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", f.hits.Load())
	}
	if got := f.lastReq.Header.Get("Authorization"); got != "Bearer "+fakeAuthKey {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestAuthFileErrorsNeverContainSecrets(t *testing.T) {
	body := `{"anthropic":{"type":"oauth","refresh":"` + fakeRefresh + `","access":"` + fakeAccess + `"},` +
		`"opencode-go":{"type":"oauth","access":"` + fakeBadEntry + `"}}`
	f := newFixture(t, serveBody(200, "{}"))
	f.a.o.AuthFile = writeAuth(t, body)
	f.acct = adapter.Account{Name: authAcctName, Vendor: authAcctVen}

	var texts []string
	st := f.a.Credential(f.acct)
	if st.Err == nil {
		t.Fatal("want a credential error")
	}
	texts = append(texts, st.Err.Error())
	_, err := f.a.Usage(context.Background(), f.acct)
	if err == nil {
		t.Fatal("want a usage error")
	}
	texts = append(texts, err.Error())
	found := f.a.Check(context.Background(), f.acct)
	if len(found) != 1 {
		t.Fatalf("findings = %+v", found)
	}
	for _, fd := range found {
		texts = append(texts, fd.Message)
	}
	for _, text := range texts {
		for _, s := range []string{fakeRefresh, fakeAccess, fakeBadEntry} {
			if strings.Contains(text, s) {
				t.Errorf("text %q contains a secret", text)
			}
		}
	}
	if f.hits.Load() != 0 {
		t.Errorf("hits = %d, want 0", f.hits.Load())
	}
}

func TestCheckAbsentAuthFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent.json")
	a := New(Options{AuthFile: p, LookPath: func(string) (string, error) { return "/bin/opencode", nil }})
	got := a.Check(context.Background(), adapter.Account{Name: authAcctName, Vendor: authAcctVen})
	want := "no credential: OpenCode login in " + p
	if len(got) != 1 || got[0].Severity != adapter.SeverityError || got[0].Message != want {
		t.Fatalf("findings = %+v, want one error %q", got, want)
	}
}

func TestAuthFileUnreadable(t *testing.T) {
	p := t.TempDir() // a directory: ReadFile fails with something other than ErrNotExist
	st := New(Options{AuthFile: p}).Credential(adapter.Account{Name: authAcctName, Vendor: authAcctVen})
	if st.Present || st.Err == nil {
		t.Fatalf("status = %+v, want absent with an error", st)
	}
	if msg := st.Err.Error(); msg == notJSONText || msg == notAPIKeyText {
		t.Fatalf("Err = %q, want the read error", msg)
	}
}

func TestAuthFileUnknownPath(t *testing.T) {
	a := New(Options{})
	st := a.Credential(adapter.Account{Name: authAcctName, Vendor: authAcctVen})
	if st.Present || st.Source != "OpenCode login" || st.Err == nil || st.Err.Error() != "OpenCode login path unknown" {
		t.Errorf("status = %+v", st)
	}
}
