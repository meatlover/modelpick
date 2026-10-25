package cli

import (
	"fmt"
	"io"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
)

// runPickAll is `modelpick pick` with no profile: one table row per profile,
// then each distinct runtime error, then a pointer to the JSON form. One Env
// serves every profile, so each account is read at most once. Exit: 1 if any
// profile failed, else 2 if any result had errors, else 0.
func runPickAll(refresh bool, stdout, stderr io.Writer, d app.Deps) int {
	cfg, err := app.LoadConfig(d)
	if err != nil {
		fmt.Fprintf(stderr, "modelpick: %v\n", err)
		return 1
	}
	env := app.NewEnv(d, cfg, refresh)
	rows := [][]string{{"PROFILE", "VENDOR", "ACCOUNT", "MODEL", "THINKING"}}
	var errLines []string
	seen := map[core.RunError]bool{}
	code := 0
	for _, p := range cfg.ProfileNames() {
		res := core.Pick(cfg, p, d.Now(), env, d.Shuffle)
		rows = append(rows, pickAllRow(res))
		for _, e := range res.Errors {
			if !seen[e] {
				seen[e] = true
				errLines = append(errLines, e.Account+": "+e.Error)
			}
		}
		code = max(code, pickAllCode(res))
	}
	writeColumns(stdout, rows, nil)
	fmt.Fprintln(stdout)
	for _, l := range errLines {
		fmt.Fprintln(stdout, l)
	}
	if len(errLines) > 0 {
		fmt.Fprintln(stdout)
	}
	fmt.Fprintln(stdout, "For JSON, name a profile: modelpick pick <profile>")
	if code == 0 && len(errLines) > 0 {
		code = 2
	}
	return code
}

// pickAllRow is a picked profile's five cells, or a failed one's two.
func pickAllRow(res core.Result) []string {
	if res.Status == core.StatusFailed || res.Vendor == nil {
		reason := ""
		if res.Reason != nil {
			reason = *res.Reason
		}
		return []string{res.Profile, reason}
	}
	thinking := "-"
	if res.Thinking != nil {
		thinking = *res.Thinking
	}
	return []string{res.Profile, *res.Vendor, *res.Account, *res.Model, thinking}
}

// pickAllCode is 1 for a failed profile and 0 otherwise; the caller turns
// errors into 2 once, after the failures are known.
func pickAllCode(res core.Result) int {
	if res.Status == core.StatusFailed {
		return 1
	}
	return 0
}
