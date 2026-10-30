#!/usr/bin/env bash
# The single gate: CI runs this script, and it must exit 0 before every commit.
# MODELPICK_CHECK_SKIP_E2E=1 skips the browser tests (scripts/e2e.sh) and says
# so. Only .githooks/pre-commit sets it, and only when no staged path can
# affect them (scripts/e2e-needed.sh); CI and the release job run the full gate.
set -euo pipefail
cd "$(dirname "$0")/.."

unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt: these files need formatting:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go vet ./...
# The live smoke test (internal/live, build tag live) calls real vendors, so
# it never runs here; vetting it with the tag keeps it compiling.
go vet -tags live ./...
go test ./...
# The e2e server's fakes are changed by stdin commands while handlers call
# them; only the race detector sees an unguarded access.
go test -race ./cmd/modelpick-e2e-server ./internal/app/apptest
# The browser tests; a missing Node, package or Chromium fails here too.
if [ "${MODELPICK_CHECK_SKIP_E2E:-}" = 1 ]; then
  echo "check.sh: browser tests skipped (MODELPICK_CHECK_SKIP_E2E=1)"
else
  bash scripts/e2e.sh
fi

# scripts/targets is the one list of build targets; release-build.sh reads it too.
while read -r target; do
  GOOS="${target%/*}" GOARCH="${target#*/}" go build ./...
  echo "build ok: $target"
done < scripts/targets
