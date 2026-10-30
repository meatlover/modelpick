package cli

import (
	"fmt"
	"io"
	"strings"
)

// docsLine closes the general usage.
const docsLine = "Docs: https://github.com/meatlover/modelpick/tree/main/docs"

// cmdHelp is everything printed about one command. commandHelp is the only
// place the usage lines and descriptions live: usageText, `help` and
// `<command> --help` are all built from it, and a test fails when its names
// and the cases of dispatch disagree.
type cmdHelp struct {
	names []string // every spelling dispatch accepts; the first is canonical
	usage []string // usage lines, without the leading "modelpick "
	alias string   // other spellings, as shown: "conf", "--help, -h"
	desc  string
}

var commandHelp = []cmdHelp{
	{[]string{"pick"}, []string{"pick [<profile>] [--refresh]"}, "",
		"JSON for one profile; with none, a table of every profile."},
	{[]string{"env"}, []string{"env <profile> [--refresh]"}, "",
		"The pick as sh export lines, for eval."},
	{[]string{"ls"}, []string{"ls [<account>…] [--refresh] [--json]", "ls models [--json]"}, "",
		"Usage of the named accounts (default: every account): windows, % used, resets, reading age. " +
			"`ls models` lists the models each configured account's subscription offers."},
	{[]string{"config", "conf"}, []string{"config"}, "conf",
		"The effective configuration. No network."},
	{[]string{"doctor"}, []string{"doctor [all]"}, "",
		"Validates the config and everything it depends on. No usage calls, unless `all`."},
	{[]string{"ui"}, []string{"ui [--port N] [--no-open]"}, "",
		"A local page to read usage and edit the config."},
	{[]string{"version", "--version"}, []string{"version"}, "--version",
		"The build: its tag (v0.2.0), else the commit's UTC date and hash (20261024-54fbf8)."},
	{[]string{"help", "--help", "-h"}, []string{"help [<command>]"}, "--help, -h",
		"This text; with a command, that command's usage, as does `<command> --help`."},
}

// usageText is the general usage, for `help` (stdout) and bad usage (stderr).
var usageText = buildUsageText()

func buildUsageText() string {
	var b strings.Builder
	b.WriteString("usage:\n")
	for _, h := range commandHelp {
		for i, u := range h.usage {
			fmt.Fprintf(&b, "  modelpick %s", u)
			if i == 0 && h.alias != "" {
				fmt.Fprintf(&b, "   (alias: %s)", h.alias)
			}
			b.WriteString("\n")
		}
		for _, line := range strings.Split(h.desc, "\n") {
			fmt.Fprintf(&b, "      %s\n", line)
		}
	}
	b.WriteString("\n" + docsLine + "\n")
	return b.String()
}

// helpFor returns the help of the command spelled name.
func helpFor(name string) (cmdHelp, bool) {
	for _, h := range commandHelp {
		for _, n := range h.names {
			if n == name {
				return h, true
			}
		}
	}
	return cmdHelp{}, false
}

// text is `usage: modelpick <line>`, further lines indented to line up, then
// the description.
func (h cmdHelp) text() string {
	var b strings.Builder
	for i, u := range h.usage {
		if i == 0 {
			b.WriteString("usage: modelpick " + u + "\n")
		} else {
			b.WriteString("       modelpick " + u + "\n")
		}
	}
	b.WriteString("\n" + h.desc + "\n")
	if h.alias != "" {
		b.WriteString("Alias: " + h.alias + "\n")
	}
	return b.String()
}

// wantsHelp reports whether args ask for the help of the command in args[0]:
// a later argument that is --help or -h. It wins over the command's own
// parsing, so pick never reads --help as a profile.
func wantsHelp(args []string) bool {
	if _, ok := helpFor(args[0]); !ok {
		return false
	}
	for _, a := range args[1:] {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

// runHelp is `modelpick help [<command>]`.
func runHelp(args []string, stdout, stderr io.Writer) int {
	switch len(args) {
	case 0:
		_, _ = io.WriteString(stdout, usageText)
		return 0
	case 1:
		h, ok := helpFor(args[0])
		if !ok {
			return badUsage(stderr, fmt.Sprintf("unknown command %q", args[0]))
		}
		_, _ = io.WriteString(stdout, h.text())
		return 0
	}
	return badUsage(stderr, fmt.Sprintf("help: unexpected argument %q", args[1]))
}
