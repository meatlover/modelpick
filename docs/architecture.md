# Architecture guide

For people changing modelpick. The design reasoning is in
[the design spec](superpowers/specs/2026-10-22-modelpick-design.md); this guide
describes the code as it is.

## Shape

modelpick is a short-lived CLI. Its edges are the vendor HTTP APIs, the
vendors' credential stores (Keychain, files), child processes (`claude`,
`opencode`, `security`) and the cache directory. Its centre is the pick rule,
which knows none of those.

```
cmd/modelpick ──> internal/cli ──┬──> internal/core ──────┬──> internal/adapter
                                 │                        └──> internal/pathx
                                 ├──> internal/usage ─────┬──> internal/adapter
                                 │                        └──> internal/platform ──> internal/pathx
                                 └──> internal/adapter/{claude,codex,opencodego}
                                                          ├──> internal/adapter
                                                          └──> internal/platform
```

Arrows point toward what changes less. `internal/archtest` enforces the
graph: it runs `go list` over the module and fails on any edge not listed in
`allowed`, and on any import of a third-party module. `cmd/...` and
`internal/cli` are the only unrestricted packages.

| Package | Knows about | Responsibility |
|---|---|---|
| `cmd/modelpick` | nothing | `os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, cli.DefaultDeps()))`. |
| `internal/cli` | everything below | Argument parsing, wiring, output formats, exit codes. `Deps` is the whole outside world (env, home, clock, adapters), so every command is testable without the real one. |
| `internal/core` | config JSON | Config loading and validation, schedules, window ordering, caps, the pick rule, the result JSON. No network, no credentials. |
| `internal/usage` | the filesystem | The per-account state file, the lock and the call floor. |
| `internal/adapter` | nothing | Vendor-neutral types (`Account`, `Window`, `Reading`, `CredStatus`, `Finding`, `Model`), the `Adapter` interface, sentinel errors, `HTTPError`, `ParseRetryAfter`. |
| `internal/adapter/<vendor>` | that vendor's HTTP API and credential format | One `Adapter` each. |
| `internal/platform` | the OS | Config and cache paths, `flock`, the macOS Keychain (`/usr/bin/security`), running a child process. |
| `internal/pathx` | nothing | `ExpandHome` (`~` and `~/…`). |
| `internal/web` | HTTP | The local web UI's HTTP surface: auth, routes, the embedded page. Imports no module package yet. |
| `internal/web/demo` | nothing | In-memory demo back end for the UI, development only; reached through `cmd/modelpick-ui-demo` (unrestricted like every `cmd/...`). May import `internal/web`. |
| `internal/live` | everything | Opt-in smoke test against real accounts, build tag `live`. |

## A pick, end to end

1. `cli.runPick` parses `<profile> [--refresh]`. A bad argument still prints a
   `failed` result on stdout (a launcher reads stdout) and the usage on
   stderr.
2. `core.Load` reads and validates the whole config: unknown keys, references,
   times, caps, relative paths are all errors naming the field. Defaults come
   from the embedded `internal/core/defaults.json`.
3. `core.Pick(cfg, profile, now, env)` walks the choices in order. For each:
   - any `skip_during` schedule matching `now` skips it;
   - `env.Credential` (local, no network) missing or unreadable skips it and
     records an error;
   - no `cap`: picked;
   - otherwise `env.Usage` gives a reading (or none); none skips it;
   - `ApplyResets` zeroes windows whose reset time has passed, `OrderWindows`
     orders the windows that apply to the model, `EffectiveCaps` gives one cap
     per window; any window at or over its cap skips it;
   - else picked.
4. The first picked choice becomes the `Result`; `ok` or, if any error was
   recorded on the way, `ok_with_errors`. No pick is `failed`.
   `Result.JSON()` writes the fields in a fixed order; `ExitCode()` maps
   status to 0 / 2 / 1.

`core.Env` is the only way core learns about the world. The real one,
`cli.env`, memoises credentials and usage per account, so each account is read
at most once per pick, and only when a capped choice reaches it.

## Usage store

`usage.Store.Get(ctx, adapter, account, refresh)` is where the floor lives:

```
take <account>.lock (flock, blocking)
read <account>.json                      missing/corrupt = empty
credential expired?        -> last good reading (warning) or ErrTokenExpired; no call
now < retry_until?         -> last good reading or error; no call (even with refresh)
!refresh && attempt < floor ago
                           -> last good reading (warning if older than floor) or error; no call
write last_attempt = now   -> if this or the lock failed: no call (fail closed)
one adapter.Usage call (10 s timeout)
write last_good or last_error (+ retry_until from Retry-After, capped at 24 h)
release lock
```

Recording the attempt before the call is what makes the floor hold for failed
calls and for concurrent processes: the second process waits on the lock,
then sees the first one's attempt. `Peek` reads the last good reading with no
lock and no call; `doctor` uses it.

## Adapters

```go
type Adapter interface {
    Vendor() string                                           // "claude" | "codex" | "opencode-go"
    Credential(acct Account) CredStatus                       // local only, no network
    Usage(ctx context.Context, acct Account) (Reading, error) // exactly one HTTP call, no caching
    Models(ctx context.Context, acct Account) ([]Model, error)
    Check(ctx context.Context, acct Account) []Finding        // doctor: binary on PATH, credential, expiry
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
  `ErrShape`, `*HTTPError{Status, RetryAfter}` for the rest. `cli.usageError`
  turns the first two into advice naming the vendor's tool.
- Outside dependencies come in through `Options` (HTTP client, clock, Keychain
  reader, process runner, `LookPath`, base URL), so tests use fakes and an
  `httptest` server.

The shared HTTP client (`cli.newHTTPClient`) has a 10-second timeout and
refuses redirects, so an auth header can never be forwarded to another host.

| Vendor | Credential | Usage → windows | Models |
|---|---|---|---|
| claude | Keychain `Claude Code-credentials` for `~/.claude`, else `…-<sha256(abs path)[:8]>`; fallback `<config_dir>/.credentials.json`; `claudeAiOauth.{accessToken, expiresAt}` | `limits[]` (session = 5h, weekly = 7d, model-scoped entries) | Built-in list (`models.go`) |
| codex | `<codex_home>/auth.json` `tokens.{access_token, account_id}`; expiry from the JWT `exp` | `rate_limit.primary_window` / `secondary_window`, length from `limit_window_seconds` | `<codex_home>/models_cache.json` |
| opencode-go | Line `key_line` of `key_file` | `usage.{rolling, weekly, monthly}` = 5h, 7d, 30d | `opencode models opencode-go` |

Each vendor package has a `testdata/usage_2026-10-22.json`: a real response,
redacted, that its parser test reads.

### Adding a vendor

1. Create `internal/adapter/<vendor>/` implementing `Adapter`; import only
   `internal/adapter` and `internal/platform` (archtest checks this).
2. Add a redacted real response under its `testdata/` and a parser test
   against it, plus tests for 401, 429 with `Retry-After`, a malformed body and
   a missing credential.
3. Add the vendor's account fields to `adapter.Account` (they must be
   non-secret: they are copied into `account_ref`), and their validation to
   `core.Load`.
4. Register it in `cli.DefaultDeps` and add its tool name to `cli.toolNames`.
5. Document the fields in `docs/configuration.md`.

## Errors and output

- `pick` always prints the result JSON on stdout, whatever happened.
  `errors` are failures in this run; `warnings` never change the status.
- `doctor` prints `ERROR`/`WARN` lines and a count, and exits 1 on any error.
  Findings are deduplicated on subject, severity and message.
- `ls` prints a table, or `--json`; its `STATE` comes from the store's
  structured result (`TokenExpired`, `RetryUntil`, the error), not from
  parsing messages.

## Tests and gates

| Check | Where | Runs |
|---|---|---|
| Unit tests per package | `*_test.go` | `go test ./...` |
| Golden pick results A–H (the spec's §7.1 examples) | `internal/core/testdata/pick_*.json` | `go test ./...` |
| `examples/config.json` equals the config guide's example | `internal/cli/docs_test.go` | `go test ./...` |
| Import layering, no third-party modules | `internal/archtest/arch_test.go` | `go test ./...` |
| No import cycles: the layer map is a DAG, and so is the real package graph (the compiler refuses one too; the gate names it) | `internal/archtest/cycle_test.go` | `go test ./...` |
| Function length: at most 60 lines per function, method or function literal, 80 in tests; no exceptions list | `internal/archtest/funclen_test.go` | `go test ./...` |
| JavaScript length: at most 60 lines per function or arrow body in `internal/web/static/**/*.js`; unbalanced braces fail (no regex literals: use `new RegExp("…")`) | `internal/archtest/jsfunclen_test.go` | `go test ./...` |
| CI workflow keeps `permissions: contents: read` | `internal/archtest/ci_test.go` | `go test ./...` |
| Live endpoints | `internal/live`, tag `live` | by hand only |

`scripts/check.sh` is the single gate: `gofmt -l`, `go vet`, `go test`, then a
build for linux/darwin × amd64/arm64. The pre-commit hook
(`.githooks/pre-commit`, enabled with `git config core.hooksPath .githooks`)
and CI (`.github/workflows/ci.yml`, Ubuntu only, on branch pushes and pull
requests) both run it; a release tag runs it in `release.yml` instead. macOS
is covered by the pre-commit hook on the owner's Mac, not by CI: a macOS
runner minute costs about 10× a Linux one while the repository is private.
The hook skips the browser tests (`MODELPICK_CHECK_SKIP_E2E=1`, set only by
the hook) when no staged path is under `internal/`, `cmd/`, `e2e/`,
`scripts/`, `.githooks/` or is `go.mod`/`go.sum`
(`scripts/e2e-needed.sh` decides); CI and the release job never skip.

## Known limitations

Each line says what would remove it and why it is deferred.

- **`core.Load` reads the file**, though core is otherwise pure. It is only
  `os.ReadFile` plus the pure `core.Parse`; removable by moving it to `cli`.
  Deferred because it is the one I/O call in core and nothing depends on it
  being elsewhere.
- **archtest ignores test-only imports.** Removable by also checking
  `TestImports`/`XTestImports` from `go list`; deferred because no test
  currently crosses a layer.
- **The Claude Keychain item is read twice per account per pick**
  (`Credential`, then `Usage`), and up to three times per account for
  `doctor`, whose `CheckCache` also reads `Credential` for the login expiry
  once per cached Check. Removable by passing the loaded credential from
  `Credential` to `Usage` and `Check`; deferred because it costs one local
  `security` call and changes the adapter interface.
- **State files are keyed by account name only**, so renaming an account
  resets its floor, and two names differing only in case collide on a
  case-insensitive filesystem. Removable by keying on a hash of the account's
  locator fields; deferred until the first report. Removing an account in
  the UI deletes its `<name>.json` and `<name>.lock`; a hand-edited removal or
  rename leaves them, and they are safe to delete.
- **The Claude User-Agent version is probed once per process** with the first
  call's context. Harmless for a short-lived CLI; removable by probing
  without a request context if modelpick ever runs as a server (v2 web UI).
- **The Keychain exit-status handling is tested on macOS only**, because the
  file is built for darwin only, and CI is Linux only, so only the
  pre-commit hook on a Mac runs it. Removable by a macOS CI job once the
  repository is public (free runners); deferred for cost while it is
  private.
- **Codex JWT `exp` and OpenCode Go percents are not range-checked.** A
  nonsensical value is used as given. Removable by clamping in each parser;
  deferred because no real response has needed it.
- **Empty `opencode models` output counts as an empty model list**, not an
  error, so `doctor` then warns that each configured model is not offered.

## Not planned

`rotate` (switching an OpenCode Go key or a login in place) was dropped on
2026-10-30. Launchers give each process its own account through the
environment instead (`modelpick env`, mmm-modelpick sets `CLAUDE_CONFIG_DIR`,
`CODEX_HOME` or `OPENCODE_AUTH_CONTENT`), so nothing needs swapping in place.
