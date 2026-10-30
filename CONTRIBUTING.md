# Contributing

modelpick is plain Go (1.24 or newer) with no third-party modules, and a web
page with no build step. The [architecture guide](docs/architecture.md)
describes the packages, the layering the tests enforce, and how to add a
vendor.

## The gate

```sh
bash scripts/check.sh
```

is the single gate. It runs `gofmt -l`, `go vet` (also with the `live` tag,
so the live smoke test keeps compiling), `go test ./...`, the race detector on
the browser tests' server and fakes, the browser tests, and a build for every
line of `scripts/targets` (linux and darwin, amd64 and arm64). CI runs it on
every branch push and pull request, and the release job runs it on every tag.

## Hooks

Run this once after cloning:

```sh
git config core.hooksPath .githooks
```

The pre-commit hook then runs `bash scripts/check.sh` and refuses a commit
that fails. When every staged path is outside `internal/`, `cmd/`, `e2e/`,
`scripts/`, `.githooks/`, `go.mod` and `go.sum` (a docs-only commit), the hook
skips the browser tests and prints a line saying so; everything else still
runs, and CI always runs the full script. The hook sets
`MODELPICK_CHECK_SKIP_E2E=1` for that; nothing else should.

`git config modelpick.autodeploy true` turns on an auto-deploy in that clone:
after a merge or pull (including `git pull --rebase`) on `main`, the
post-merge and post-rewrite hooks run `bash scripts/deploy-local.sh`, which
builds the three commands to `cmd/<name>/<name>` and installs `modelpick` to
`~/.local/bin/modelpick` (or `$MODELPICK_INSTALL_DIR`). It only installs: it
never restarts a running `modelpick ui`, which then shows that a newer
`modelpick` is installed. Run `bash scripts/deploy-local.sh` to do it by hand.

## Browser tests

The browser tests (`e2e/`, Playwright driving Chromium, development only)
need Node.js 18 or later. Install the locked packages and the browser once:

```sh
cd e2e && npm ci && npx playwright install chromium
```

On a fresh Linux machine Chromium also needs system libraries; install it with
`npx playwright install --with-deps chromium` instead (it runs the system
package manager, so it may ask for sudo).

`bash scripts/e2e.sh` runs them alone. It builds the test server
`cmd/modelpick-e2e-server` (the live back end over fake vendors, in a temp
directory) and the demo, and opens every JS unit test page
(`internal/web/static/test/*.test.html`). Without Node, the packages or
Chromium, it and `check.sh` fail and print the command above.

CI runs them a second time with `E2E_OP_DELAY_MS=400`, which answers every
UI op 400 ms late: a test that acts again before the page has redrawn after
an op, or an edit the page builds on a card drawn before an earlier op was
answered, fails there. `E2E_OP_DELAY_MS=400 bash scripts/e2e.sh` runs that
pass locally; `check.sh` does not.

`go run ./cmd/modelpick-ui-demo` serves the page over an in-memory demo back
end, for working on the UI without any account.

## Live smoke test

An opt-in test against your real accounts:

```sh
go test -tags live ./internal/live/ -v
```

It makes real usage calls, through the same floor as the CLI (never forced
past it). It does not renew an expired login unless `MODELPICK_LIVE_RENEW=1`
is set, because a Claude renewal spends a few tokens. It fails if a vendor's
response is not in a shape modelpick recognises; other outcomes (no reading,
an expired token, an older reading) are logged, with window lengths and
percents only. `check.sh` and CI never run it.

## Docs

The docs describe what the code does; when they disagree, the code wins and
the docs change. Tests in `internal/cli/docs_test.go` and
`internal/core/docs_test.go` check the parts that drift: every command in the
help table has a section in [the reference](docs/reference.md), every config
key and default is in [the configuration guide](docs/configuration.md), every
`doctor` message has a row in the
[troubleshooting table](docs/operations.md#troubleshooting), every `pick`
field and status is in the reference, the
[worked example](docs/concepts.md#a-worked-example) gives the stated result,
every relative link and anchor resolves, no doc pins a concrete version, and
every Go name in the architecture guide exists.

## Releases

[docs/release.md](docs/release.md) has the steps. Changes go under
`## Unreleased` in [CHANGELOG.md](CHANGELOG.md).

## Security

Report a vulnerability as described in [SECURITY.md](SECURITY.md), not in a
public issue.
