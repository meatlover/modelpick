# Architecture guide

For people changing modelpick. It describes the code as it is; the ideas are
in [how modelpick decides](concepts.md).

## Shape

modelpick is a short-lived CLI plus a local web UI. Its edges are the vendor
HTTP APIs, the vendors' credential stores (Keychain, files), child processes
(`claude`, `codex`, `opencode`, `security`, the browser opener), the cache
directory and, for the UI, HTTP on `127.0.0.1`. Its centre is the pick rule,
which knows none of those.

```
cmd/modelpick ──> internal/cli ──┬──> internal/app ──┬──> internal/core ──┬──> internal/adapter
                                 │                   │                    └──> internal/pathx
                                 │                   ├──> internal/usage ──┬──> internal/adapter
                                 │                   │                     └──> internal/platform
                                 │                   └──> internal/adapter/{claude,codex,opencodego}
                                 │                                         ├──> internal/adapter
                                 │                                         └──> internal/platform
                                 └──> internal/web/live ──┬──> internal/web
                                                          ├──> internal/app
                                                          └──> internal/core, internal/adapter
```

Arrows point toward what changes less. `internal/archtest` enforces the
graph: it runs `go list` over the module and fails on any edge not listed in
its `allowed` map, and on any import of a third-party module. `cmd/...` and
`internal/cli` are the only unrestricted packages; `internal/app/apptest`, a
test fake, is exempt too (the map's comment says what would remove that).

| Package | Knows about | Responsibility |
|---|---|---|
| `cmd/modelpick` | nothing | `os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, app.DefaultDeps(cli.Version)))`. |
| `internal/cli` | the terminal | Argument parsing, the help table, output formats (JSON, tables, `sh`), exit codes, starting the UI server. `cli.Version` is the one home of the release number. |
| `internal/app` | the outside world, through `Deps` | Wiring shared by the CLI and the UI: `app.Deps` (env, home, clock, adapters), `app.DefaultDeps`, the usage environment `app.Env`, `app.Doctor` and `app.DoctorAll`, the config editor the UI applies operations through, and `app.UsageError`, which turns vendor failures into advice. |
| `internal/core` | config JSON | Config loading and validation, schedules, window ordering, caps, the pick rule, the result JSON. No network, no credentials. |
| `internal/usage` | the filesystem | The per-account state file, the lock and the call floor (`usage.Store`). |
| `internal/adapter` | nothing | Vendor-neutral types (`Account`, `Window`, `Reading`, `CredStatus`, `Finding`, `Model`), the `Adapter` and `Renewer` interfaces, sentinel errors, `HTTPError`, `RenewError`, `ParseRetryAfter`. |
| `internal/adapter/<vendor>` | that vendor's HTTP API, credential format and tool | One `Adapter` each, with `Renewer` for Claude and Codex. |
| `internal/platform` | the OS | Config and cache paths, `flock`, atomic writes, the macOS Keychain (`/usr/bin/security`), running a child process, opening the browser. |
| `internal/pathx` | nothing | `pathx.ExpandHome` (`~` and `~/…`). |
| `internal/web` | HTTP | The UI's HTTP surface: the `web.Backend` interface, wire types, the Host/Origin/token guard, security headers, the embedded page (`internal/web/static`). Imports no module package. |
| `internal/web/live` | the app | The real `web.Backend`: the page's view of the config, saved readings and doctor, and its edits through the app editor. |
| `internal/web/demo` | nothing | An in-memory `web.Backend` for development, served by `cmd/modelpick-ui-demo`. |
| `internal/web/webtest` | HTTP | The back-end contract tests run against both `live` and `demo`. |
| `internal/app/apptest` | the app | Fake adapters for tests and for `cmd/modelpick-e2e-server`, the browser tests' server. |
| `internal/live` | everything | Opt-in smoke test against real accounts, build tag `live`. |
| `internal/archtest` | the repository | The architecture and repository gates (below). |

## A pick, end to end

1. `cli.Main` dispatches on the first argument; `cli.runPick` parses
   `<profile> [--refresh]`. A bad argument still prints a `failed` result on
   stdout (a launcher reads stdout) and the usage on stderr.
2. `core.Load` reads and validates the whole config: unknown keys, references,
   times, caps, relative paths are all errors naming the field. Defaults come
   from the embedded `internal/core/defaults.json`.
3. `core.Pick(cfg, profile, now, env, shuffle)` walks the positions in order.
   For a plain choice, `core.Pick` applies, in order:
   - any `skip_during` schedule matching `now` skips it, and so does an
     `allow_during` list none of whose schedules matches;
   - `env.Credential` (local, no network) missing or unreadable skips it and
     records an error;
   - no `cap`: picked;
   - otherwise `env.Usage` gives a reading (or none); none skips it;
   - `core.ApplyResets` zeroes windows whose reset time has passed,
     `core.OrderWindows` orders the windows that apply to the model, and
     `core.EffectiveCaps` gives one cap per window; any window at or over its
     cap (fixed, or the pace line from `core.PaceAllowed`) skips it;
   - else picked.

   At a load balancing group, the schedule and credential steps run on every
   member in file order, then the group's algorithm (`core.Algorithms`; only
   `random`) tries the survivors with the cap steps in shuffled order. Skipped
   members are recorded in file order.
4. The first picked choice becomes the `core.Result`: `ok` or, if any error was
   recorded on the way, `ok_with_errors`. No pick is `failed`.
   `Result.JSON()` writes the fields in a fixed order; `Result.ExitCode()` maps
   status to 0 / 2 / 1.

`core.Env` is the only way core learns about the world. The real one,
`app.Env`, memoises credentials and usage per account, so each account is read
at most once per pick, and only when a capped choice reaches it.

## Usage store

`usage.Store.Get(ctx, adapter, account, refresh)` is where the floor lives:

```
take <account>.lock (flock, blocking)
read <account>.json                      missing/corrupt = empty
credential expired, not renewable or reading younger than the floor
                           -> last good reading (warning) or ErrTokenExpired; no call
now < retry_until?         -> last good reading or error; no call (even with refresh)
!refresh && attempt < floor ago
                           -> last good reading (warning if older than floor) or error; no call
write last_attempt = now   -> if this or the lock failed: no call (fail closed)
credential expired?        -> Renewer.Renew (60 s); failure is recorded as the attempt's error
one adapter.Usage call (10 s timeout)
write last_good or last_error (+ retry_until from Retry-After, capped at 24 h)
release lock
```

Recording the attempt before the call is what makes the floor hold for failed
calls and for concurrent processes: the second process waits on the lock,
then sees the first one's attempt. `usage.Store.Saved` reads the stored state
with no lock and no call; `doctor` uses it.

## Adapters

```go
type Adapter interface {
    Vendor() string                                           // "claude" | "codex" | "opencode-go"
    Credential(acct Account) CredStatus                       // local only, no network
    Usage(ctx context.Context, acct Account) (Reading, error) // exactly one HTTP call, no caching
    Models(ctx context.Context, acct Account) ([]Model, error)
    Check(ctx context.Context, acct Account) []Finding        // doctor: binary on PATH, credential, expiry
}

type Renewer interface {
    Renew(ctx context.Context, acct Account) error // the vendor's own tool renews the login
}
```

Rules every adapter follows:

- **Secrets stay inside the adapter.** `CredStatus.Source` describes where the
  credential is, never its value. Error text never includes a response body
  or a credential.
- **One call, no state.** Caching and the floor belong to `usage`.
- **Translate, don't judge.** Map the vendor response to `[]Window` with the
  window length if known (0 = unknown), percent used, reset time and model
  scope. Anything not understood is `ErrShape` or a `Reading.Notes` entry,
  never a guessed 0%.
- **Errors are classified**: `ErrUnauthorized` for 401, `ErrTokenExpired`,
  `ErrShape`, `*HTTPError{Status, RetryAfter}` for the rest, `*RenewError`
  for a failed renewal. `app.UsageError` turns 401, expiry and renewal
  failures into advice naming the vendor's tool.
- **Renewal never writes a vendor file.** `Renew` runs the vendor's tool and
  lets it renew its own login; the store then re-reads the credential.
- Outside dependencies come in through `Options` (HTTP client, clock, Keychain
  reader, process runner, `LookPath`, base URL), so tests use fakes and an
  `httptest` server.

The shared HTTP client (`app.DefaultDeps` builds it) has a 10-second timeout
and refuses redirects, so an auth header can never be forwarded to another
host.

| Vendor | Credential | Usage → windows | Models |
|---|---|---|---|
| claude | Keychain `Claude Code-credentials` for `~/.claude`, else `…-<sha256(abs path)[:8]>`; fallback `<config_dir>/.credentials.json`; `claudeAiOauth.{accessToken, expiresAt, refreshTokenExpiresAt}` | `limits[]` (session = 5h, weekly = 7d, model-scoped entries) | Built-in list (`internal/adapter/claude/models.go`) |
| codex | `<codex_home>/auth.json` `tokens.{access_token, account_id}`; expiry from the JWT `exp` | `rate_limit.primary_window` / `secondary_window`, length from `limit_window_seconds` | `<codex_home>/models_cache.json` |
| opencode-go | Line `key_line` of `key_file` | `usage.{rolling, weekly, monthly}` = 5h, 7d, 30d | `opencode models opencode-go` |

Each vendor package has a `testdata/usage_2026-10-22.json`: a real response,
redacted, that its parser test reads.

### Adding a vendor

1. Create `internal/adapter/<vendor>/` implementing `adapter.Adapter` (and
   `adapter.Renewer` if its tool can renew a login); import only
   `internal/adapter` and `internal/platform` (archtest checks this).
2. Add a redacted real response under its `testdata/` and a parser test
   against it, plus tests for 401, 429 with `Retry-After`, a malformed body and
   a missing credential.
3. Add the vendor's account fields to `adapter.Account` (they must be
   non-secret: they are copied into `account_ref`), their decoding to
   `core.DocAccount` and their validation to the config parser in
   `internal/core/config.go`.
4. Register it in `app.DefaultDeps` and add its tool name to the advice map in
   `internal/app/advice.go`.
5. Document the fields in `docs/configuration.md`, its doctor messages in the
   troubleshooting table of `docs/operations.md` (a test fails until each has
   a row), and its endpoint and headers in `docs/operations.md` and
   `SECURITY.md`.

## Errors and output

- `pick` always prints the result JSON on stdout, whatever happened.
  `errors` are failures in this run; `warnings` never change the status.
- `doctor` prints `ERROR`/`WARN` lines and a count, and exits 1 on any error.
  Findings are deduplicated on subject, severity and message, and the same
  adapter finding on several accounts is reported once, naming them all.
- `ls` prints a table, or `--json`; its `STATE` comes from the store's
  structured result (`TokenExpired`, `RetryUntil`, the error), not from
  parsing messages.

## Tests and gates

| Check | Where | Runs |
|---|---|---|
| Unit tests per package | `*_test.go` | `go test ./...` |
| Golden pick results A–H and one from a load balancing group | `internal/core/testdata/pick_*.json` | `go test ./...` |
| Docs agree with the code: commands, config keys and defaults, doctor messages, pick fields, the worked example, links, version pins, Go names in this guide | `internal/cli/docs_test.go`, `internal/core/docs_test.go` | `go test ./...` |
| Import layering, no third-party modules | `internal/archtest/arch_test.go` | `go test ./...` |
| No import cycles: the layer map is a DAG, and so is the real package graph | `internal/archtest/cycle_test.go` | `go test ./...` |
| Function length: at most 60 lines per function, method or function literal, 80 in tests; no exceptions list | `internal/archtest/funclen_test.go` | `go test ./...` |
| JavaScript length: at most 60 lines per function or arrow body in `internal/web/static/**/*.js`; unbalanced braces fail (no regex literals: use `new RegExp("…")`) | `internal/archtest/jsfunclen_test.go` | `go test ./...` |
| The page's static files: every JS import resolves to a file and export, and nothing the CSP would block | `internal/archtest/static_test.go` | `go test ./...` |
| `cli.Version` is the only place a version is spelled | `internal/archtest/version_test.go` | `go test ./...` |
| CI workflow: read-only token, branches only, Linux only | `internal/archtest/ci_test.go` | `go test ./...` |
| Release script and workflow: each gate refuses, the archive's contents, one reader of `scripts/targets`, workflow permissions | `internal/archtest/release_test.go` | `go test ./...` |
| `scripts/deploy-local.sh` and the hooks that call it | `internal/archtest/deploy_test.go` | `go test ./...` |
| The pre-commit hook's browser-test skip and its classifier | `internal/archtest/hook_test.go` | `go test ./...` |
| The UI back-end contract, on both back ends | `internal/web/webtest` | `go test ./...` |
| Browser tests (Playwright) | `e2e/`, `internal/web/static/test/` | `bash scripts/e2e.sh` |
| Live endpoints | `internal/live`, tag `live` | by hand only |

`scripts/check.sh` is the single gate: `gofmt -l`, `go vet` (also with the
`live` tag), `go test`, the race detector on the browser tests' server and
fakes, the browser tests, then a build for linux/darwin × amd64/arm64. The
pre-commit hook and CI (`.github/workflows/ci.yml`, Ubuntu and macOS, on
branch pushes and pull requests) both run it; a release tag runs it in
`release.yml` instead. [CONTRIBUTING.md](../CONTRIBUTING.md) has the
details.

## Known limitations

Each line says what would remove it and why it is deferred.

- **`core.Load` reads the file**, though core is otherwise pure. It is only
  `os.ReadFile` plus the pure `core.Parse`; removable by moving it to `app`.
  Deferred because it is the one I/O call in core and nothing depends on it
  being elsewhere.
- **archtest ignores test-only imports.** Removable by also checking
  `TestImports`/`XTestImports` from `go list`; deferred because no test
  currently crosses a layer.
- **The Claude Keychain item is read twice per account per pick**
  (`Credential`, then `Usage`), and up to three times per account for
  `doctor`, whose `app.CheckCache` also reads `Credential` for the login
  expiry once per cached Check. Removable by passing the loaded credential
  from `Credential` to `Usage` and `Check`; deferred because it costs one
  local `security` call and changes the adapter interface.
- **State files are keyed by account name only**, so renaming an account
  resets its floor, and two names differing only in case collide on a
  case-insensitive filesystem. Removable by keying on a hash of the account's
  locator fields; deferred until the first report. Removing an account in
  the UI deletes its `<name>.json` and `<name>.lock`; a hand-edited removal or
  rename leaves them, and they are safe to delete.
- **The Claude User-Agent version is probed once per process** with its own
  5-second context. Harmless for a short-lived CLI; a long-running
  `modelpick ui` keeps the version it found first until restarted.
- **The Keychain exit-status handling is tested on macOS only**, because the
  file is built for darwin only. CI's macOS job runs it.
- **Codex JWT `exp` and OpenCode Go percents are not range-checked.** A
  nonsensical value is used as given. Removable by clamping in each parser;
  deferred because no real response has needed it.
- **Empty `opencode models` output counts as an empty model list**, not an
  error, so `doctor` then warns that each configured model is not offered.

## Not planned

`rotate` (switching an OpenCode Go key or a login in place) was dropped on
2026-10-30. A launcher gives each process its own account through the
environment instead (`modelpick env` sets `CLAUDE_CONFIG_DIR` or `CODEX_HOME`,
and names the OpenCode Go key), so nothing needs swapping in place.
