package cli

import (
	"fmt"
	"io"

	"github.com/meatlover/modelpick/internal/app"
)

// runDoctor is `modelpick doctor`: validates the config and everything it
// depends on. It never calls Usage and never takes the store lock (Saved only).
func runDoctor(args []string, stdout, stderr io.Writer, d app.Deps) int {
	if len(args) > 0 {
		return badUsage(stderr, fmt.Sprintf("doctor: unexpected argument %q", args[0]))
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		return writeReport(stdout, []app.Finding{app.ConfigFinding(err)})
	}
	return writeReport(stdout, app.Doctor(cfg, d, &app.CheckCache{}))
}

func writeReport(w io.Writer, findings []app.Finding) int {
	errs, warns := 0, 0
	for _, f := range findings {
		label := "WARN  "
		if f.Severity == "error" {
			label = "ERROR "
			errs++
		} else {
			warns++
		}
		fmt.Fprintf(w, "%s %s: %s\n", label, f.Subject, f.Message)
	}
	if len(findings) == 0 {
		fmt.Fprintln(w, "no problems found")
		return 0
	}
	fmt.Fprintf(w, "%d error(s), %d warning(s)\n", errs, warns)
	if errs > 0 {
		return 1
	}
	return 0
}
