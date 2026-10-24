package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/meatlover/modelpick/internal/adapter/claude"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
)

// modelpickVars are the variables a successful env sets; a failed env unsets
// them so a value left by an earlier eval cannot pass for this pick.
var modelpickVars = []string{"STATUS", "PROFILE", "VENDOR", "ACCOUNT", "MODEL", "THINKING", "KEY_FILE", "KEY_LINE"}

// runEnv is `modelpick env <profile> [--refresh]`: the pick as POSIX sh for
// eval, its exit code returned.
func runEnv(args []string, stdout, stderr io.Writer, d app.Deps) int {
	profile, refresh, err := parseProfileArgs("env", args)
	if err == nil && profile == "" {
		err = errors.New("env: missing <profile>")
	}
	if err != nil {
		// There is no profile to name: the echo line is just the reason.
		_, _ = io.WriteString(stdout, failedScript("modelpick: "+err.Error()))
		badUsage(stderr, err.Error())
		return 1
	}
	res := pick(profile, refresh, d)
	_, _ = io.WriteString(stdout, envScript(res, d.Home))
	return res.ExitCode()
}

// failedScript unsets every MODELPICK_* variable, then reports msg and fails.
func failedScript(msg string) string {
	var b strings.Builder
	for _, v := range modelpickVars {
		b.WriteString("unset MODELPICK_" + v + "\n")
	}
	b.WriteString("echo " + shellQuote(msg) + " >&2\nfalse\n")
	return b.String()
}

// envScript renders res as sh for eval. It never includes a credential.
func envScript(res core.Result, home string) string {
	var b strings.Builder
	for _, e := range res.Errors {
		b.WriteString("echo " + shellQuote("modelpick: "+e.Account+": "+e.Error) + " >&2\n")
	}
	if res.Status == core.StatusFailed {
		reason := ""
		if res.Reason != nil {
			reason = *res.Reason
		}
		return b.String() + failedScript("modelpick: "+res.Profile+": "+reason)
	}
	export := func(name, value string) { b.WriteString("export " + name + "=" + shellQuote(value) + "\n") }
	export("MODELPICK_STATUS", string(res.Status))
	export("MODELPICK_PROFILE", res.Profile)
	export("MODELPICK_VENDOR", deref(res.Vendor))
	export("MODELPICK_ACCOUNT", deref(res.Account))
	export("MODELPICK_MODEL", deref(res.Model))
	if res.Thinking == nil {
		b.WriteString("unset MODELPICK_THINKING\n")
	} else {
		export("MODELPICK_THINKING", *res.Thinking)
	}
	vendorLines(&b, res, home, export)
	return b.String()
}

func vendorLines(b *strings.Builder, res core.Result, home string, export func(name, value string)) {
	ref := res.AccountRef
	switch deref(res.Vendor) {
	case "claude":
		if claude.IsDefaultConfigDir(ref.ConfigDir, home) {
			b.WriteString("unset CLAUDE_CONFIG_DIR\n")
		} else {
			export("CLAUDE_CONFIG_DIR", ref.ConfigDir)
		}
	case "codex":
		export("CODEX_HOME", ref.CodexHome)
	case "opencode-go":
		export("MODELPICK_KEY_FILE", ref.KeyFile)
		export("MODELPICK_KEY_LINE", fmt.Sprint(ref.KeyLine))
		return
	}
	b.WriteString("unset MODELPICK_KEY_FILE\nunset MODELPICK_KEY_LINE\n")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// shellQuote wraps s in single quotes, writing each ' inside as '\”.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
