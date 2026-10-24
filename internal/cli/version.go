package cli

// Version is the modelpick version. A release commit (the one a vN.N.N tag
// points at) holds that release's number; between releases it holds the
// next number with -dev. -ldflags "-X github.com/meatlover/modelpick/internal/cli.Version=…"
// still overrides it.
var Version = "0.3.0-dev"
