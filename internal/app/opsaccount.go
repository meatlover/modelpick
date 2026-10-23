package app

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/meatlover/modelpick/internal/core"
)

// accountField is one locator field of a vendor, with the words the page's
// form uses for it.
type accountField struct{ key, what string }

var vendorFields = map[string][]accountField{
	"claude":      {{"config_dir", "a config dir"}},
	"codex":       {{"codex_home", "a Codex home"}},
	"opencode-go": {{"key_file", "a key file"}, {"key_line", "a key line"}},
}

// opAccountAdd stores the vendor's locator fields as typed (a "~" path stays
// "~"); fields of other vendors are ignored.
func opAccountAdd(doc *core.Doc, a opArgs, _ OpEnv) error {
	_, taken := doc.Accounts[a.Name]
	if err := checkNew("account", a.Name, taken); err != nil {
		return err
	}
	fields, ok := vendorFields[a.Vendor]
	if !ok {
		return refuse("Unknown vendor %q", a.Vendor)
	}
	acct := core.DocAccount{Vendor: a.Vendor}
	for _, f := range fields {
		if err := setAccountField(&acct, a.Name, f, a.Fields[f.key]); err != nil {
			return err
		}
	}
	if doc.Accounts == nil {
		doc.Accounts = map[string]core.DocAccount{}
	}
	doc.Accounts[a.Name] = acct
	return nil
}

func setAccountField(acct *core.DocAccount, name string, f accountField, v any) error {
	if v == nil || v == "" {
		return refuse("Account %s needs %s", name, f.what)
	}
	if f.key == "key_line" {
		n, ok := v.(float64)
		if !ok || n < 1 || n != math.Trunc(n) || n > math.MaxInt32 {
			return refuse("Account %s: key line must be a whole number, 1 or more", name)
		}
		acct.KeyLine = int(n)
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return refuse("Account %s needs %s", name, f.what)
	}
	// The rule core.Parse applies, refused here in the form's words.
	if !filepath.IsAbs(s) && s != "~" && !strings.HasPrefix(s, "~/") {
		return refuse("Account %s: %s must be an absolute path or start with ~", name, strings.TrimPrefix(f.what, "a "))
	}
	switch f.key {
	case "config_dir":
		acct.ConfigDir = s
	case "codex_home":
		acct.CodexHome = s
	case "key_file":
		acct.KeyFile = s
	}
	return nil
}

// opAccountRemove refuses an account a choice still uses, naming the first.
func opAccountRemove(doc *core.Doc, a opArgs, _ OpEnv) error {
	if _, ok := doc.Accounts[a.Name]; !ok {
		return refuse("Unknown account %s", a.Name)
	}
	for _, p := range sortedNames(doc.Profiles) {
		for i, c := range doc.Profiles[p] {
			if c.Account == a.Name {
				return refuse("Account %s is used by %s", a.Name, describe(p, i))
			}
		}
	}
	delete(doc.Accounts, a.Name)
	return nil
}
