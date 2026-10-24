#!/usr/bin/env bash
# The browser tests (e2e/, Playwright driving Chromium). check.sh runs this
# after the Go tests; nothing here is optional, so a missing tool fails.
set -euo pipefail
cd "$(dirname "$0")/.."

fix="cd e2e && npm ci && npx playwright install chromium"
missing() {
  echo "e2e: $1; install the browser tests' tools with:" >&2
  echo "  $fix" >&2
  echo "(on a fresh Linux machine, which lacks Chromium's system libraries," >&2
  echo "end with: npx playwright install --with-deps chromium)" >&2
  exit 1
}

command -v node >/dev/null 2>&1 || missing "node is not on PATH (install Node.js 18 or later first)"
[ -d e2e/node_modules/@playwright/test ] || missing "e2e/node_modules/@playwright/test is missing"
# The installed package must be the locked one (a stale or partial install
# runs another Playwright); then Playwright names its Chromium.
probe="$(cd e2e && node -e '
  const want = require("./package-lock.json").packages["node_modules/@playwright/test"].version;
  const have = require("@playwright/test/package.json").version;
  if (have !== want) {
    process.stdout.write("installed @playwright/test is " + have + ", package-lock.json wants " + want);
  } else {
    process.stdout.write("chromium " + require("@playwright/test").chromium.executablePath());
  }
')" || missing "Playwright cannot name its Chromium"
case "$probe" in
  "chromium "*) chromium="${probe#chromium }" ;;
  *) missing "$probe" ;;
esac
[ -x "$chromium" ] || missing "Chromium is not installed at $chromium"

bin="$(mktemp -d)"
cleanup() {
  # What a Playwright that died left behind: every server runs from $bin,
  # and the tests' temp dirs and browser profiles are under $bin/tmp.
  pkill -f "$bin/" 2>/dev/null || true
  rm -rf "$bin"
}
trap cleanup EXIT

go build -o "$bin/modelpick-e2e-server" ./cmd/modelpick-e2e-server
go build -o "$bin/modelpick-ui-demo" ./cmd/modelpick-ui-demo
mkdir "$bin/tmp"
export E2E_SERVER="$bin/modelpick-e2e-server" E2E_DEMO="$bin/modelpick-ui-demo" TMPDIR="$bin/tmp"
# --no-install: never fetch a Playwright that is not the locked one.
(cd e2e && npx --no-install playwright test)
