package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/meatlover/modelpick/internal/app"
)

// runDoctor is `modelpick doctor [all]`: validates the config and everything
// it depends on. Plain doctor never calls Usage and never takes the store
// lock (Saved only); `doctor all` also makes one usage call per account that
// has no fresh reading.
func runDoctor(args []string, stdout, stderr io.Writer, d app.Deps) int {
	all := len(args) > 0 && args[0] == "all"
	if len(args) > 0 && !all {
		return badUsage(stderr, fmt.Sprintf("doctor: unexpected argument %q", args[0]))
	}
	if len(args) > 1 {
		return badUsage(stderr, fmt.Sprintf("doctor: unexpected argument %q", args[1]))
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		return writeReport(stdout, []app.Finding{app.ConfigFinding(errors.New(configErrText(err, d)))})
	}
	if all {
		ctx := d.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		return writeReport(stdout, app.DoctorAll(ctx, cfg, d, &app.CheckCache{}))
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
