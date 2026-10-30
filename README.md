# modelpick

If you pay for several LLM coding subscriptions (Claude, ChatGPT/Codex,
OpenCode Go, often more than one account of each), you switch between them by
hand when one runs low. modelpick makes that decision: for this kind of work,
right now, which vendor, account, model and thinking level should I use? It
answers in JSON. Whoever calls it (a person, a script, an agent launcher) acts
on the answer; modelpick never launches or reconfigures a tool.

The answer depends on a **profile** you define (an ordered list of choices),
the live usage of every rate-limit window of each account, and schedules
(for example a provider's peak-price hours) during which a choice must not be
used.

## Install

```
go install github.com/meatlover/modelpick/cmd/modelpick@latest
```

Linux and macOS, amd64 and arm64. The [installation guide](docs/install.md)
covers prebuilt binaries, building from a checkout, first setup and
uninstalling.

## Documentation

- [Installation guide](docs/install.md): requirements, install, first setup, uninstall.
- [Configuration guide](docs/configuration.md): every field of the config file.
- [Admin guide](docs/admin.md): files, network calls, the call floor, the UI, launchers, troubleshooting.
- [Architecture guide](docs/architecture.md): packages, layering, the pick and store flow, adding a vendor.
- [Releasing](docs/release.md): cutting a release, what the release job checks, downloading a build.
- [Changelog](CHANGELOG.md): what changed in each release.

## Commands

```
modelpick pick [<profile>] [--refresh]           JSON for one profile; with none, a table of every profile
modelpick env <profile> [--refresh]              the pick as sh export lines, for eval
modelpick ls [<account>…] [--refresh] [--json]   usage of the named accounts (default: every account): windows, % used, resets, reading age
modelpick ls models [--json]                     models each configured account's subscription offers
modelpick config                                 the effective configuration. No network; alias: modelpick conf.
modelpick doctor [all]                           validates the config and everything it depends on. No usage calls, unless `all`.
modelpick ui [--port N] [--no-open]              a local page to read usage and edit the config
modelpick help [<command>]                       this list, or one command's usage. Also: <command> --help.
modelpick version                                 the build: its tag (v0.2.0), else the commit's UTC date and hash (20261024-54fbf8). Alias: --version.
```

`modelpick ui` serves a page on `127.0.0.1`, prints its URL and opens it.
The page shows saved readings. It calls a vendor only from its Refresh
buttons and its Diagnose button (`doctor all`), which skip the floor like
`--refresh`, and for one background first reading per run of an account that
has none, which keeps the floor. None of them passes a server `Retry-After`.
When the login has expired, each asks Claude Code or Codex to renew it first.
Refresh is enabled for an expired account that can be renewed. Every edit applies to the config file at once, and only
if it adds no `doctor` error. The first edit rewrites a hand-written file in a
canonical layout; before it, the file is copied to
`~/.cache/modelpick/config.before-ui.json`. One instance runs per user: a
second `modelpick ui` opens the running one.
See the [admin guide](docs/admin.md#the-ui-modelpick-ui).

`--refresh` forces a usage call even when the last reading is younger than the
floor. It still honours a server `Retry-After` that has not elapsed.

An expired login is renewed only when the last good reading is older than the
floor. `--refresh` and the page's Refresh do not renew for a fresher reading.
A failed renewal counts as an attempt: it is retried at most once per
`min_call_interval_minutes`, and the old reading shows with `renewal failed:
<reason>`. `ls`, `pick`, `env` and `doctor` exit 130 on Ctrl-C, which also
cancels a renewal.

`modelpick doctor all` does what `doctor` does and also checks that each
account's usage can be read. An account whose saved reading is younger than
`min_call_interval_minutes` is used as is; for any other account it calls the
vendor once, like Refresh, ignoring the floor but not a server `Retry-After`;
an expired login is renewed first. A reading that fails shows as a warning, unless the vendor answered with a
rate limit (`Retry-After`), which is not reported.

`doctor` and `ls models` make no usage calls, but they do list each account's
models locally: Claude from a list built into modelpick, Codex from
`<codex_home>/models_cache.json`, and OpenCode Go by running
`opencode models opencode-go`, which may contact OpenCode.

## Configuration

The config file is, in order: `$MODELPICK_CONFIG`,
`$XDG_CONFIG_HOME/modelpick/config.json`, `~/.config/modelpick/config.json`
(the same on macOS). Start from [`examples/config.json`](examples/config.json)
and read the [configuration guide](docs/configuration.md). `modelpick doctor`
tells you what is wrong with a setup.

Usage readings are cached, one state file per account, under
`$XDG_CACHE_HOME/modelpick/`, else `~/.cache/modelpick/`. The state files hold
no credentials.

## Call floor

modelpick makes at most one usage call per account per 15 minutes
(`settings.min_call_interval_minutes`). Within that window, and whenever a call
fails, it uses the last good reading and says so in `warnings`.
Concurrent picks on a cold cache make one call between them. The floor needs a
working cache: if an account's lock cannot be taken or the attempt cannot be
written, modelpick makes no usage call for it. It uses the last stored reading
it can read and warns `cache unavailable`; with none, that account's usage is
unknown and the error names the cache.

## `pick` status and exit code

| `status` | Exit | Meaning |
|---|---|---|
| `ok` | 0 | A model was picked and nothing failed along the way. |
| `ok_with_errors` | 2 | A model was picked, but something failed during this pick (a usage call or credential read failed), so at least one choice was skipped for a reason other than its own usage or schedule. |
| `failed` | 1 | No model: nothing available, unknown profile, config error, or any other failure. |

The JSON always has the fields `status, profile, vendor, account, model,
thinking, account_ref, load_balance, reason, skipped, errors, warnings`; the
last three are lists, never `null`. `load_balance` is `null` unless the pick
came from a [load balancing group](docs/configuration.md#load-balancing-groups),
then `{"algorithm": "random", "choice": 2, "member": "2b"}`.

## Terms and credentials

Read this before you use modelpick.

- modelpick reads each tool's stored login (Claude Code's Keychain item or
  credentials file, Codex's `auth.json`, your OpenCode Go key file) **in
  memory**, only to call that vendor's usage endpoint.
- It **stores no credentials**: not in its cache, its output or its logs.
- It **never writes any vendor file** and never handles a refresh token. When a
  usage call is due and a Claude or Codex login has expired, it asks the
  vendor's own tool to renew it: `claude -p` with a tiny prompt (a few tokens
  of that Claude subscription, about 10 seconds) or `codex app-server` (no
  quota, about 3 seconds). The tool renews its own login. If the refresh
  token itself has expired, log in again (`/login` in Claude Code,
  `codex login`).

All three usage endpoints are undocumented and may change without notice; a
changed response is reported as “usage unknown”, never as 0%.

Anthropic's terms say third-party tools “may not collect, store, or
intermediate” Claude login tokens. modelpick reads the token in memory and
stores nothing, but whether that is acceptable under your terms with each
vendor is your decision. You decide whether to use it.

## Development

Run `git config core.hooksPath .githooks` once after cloning; the pre-commit
hook then runs `bash scripts/check.sh` (gofmt, vet, including the `live`-tagged
smoke test, tests, the browser tests, four-target cross-build) and refuses a
commit that fails. When every staged path is outside `internal/`, `cmd/`,
`e2e/`, `scripts/`, `.githooks/`, `go.mod` and `go.sum` (a docs-only commit),
the hook skips the browser tests and prints a line saying so; everything else
still runs, and CI always runs the full script.

`git config modelpick.autodeploy true` turns on an auto-deploy in that clone:
after a merge or pull (including `git pull --rebase`) on `main`, the
post-merge and post-rewrite hooks run `bash scripts/deploy-local.sh`, which
builds the three commands to `cmd/<name>/<name>` and installs `modelpick` to
`~/.local/bin/modelpick` (or `$MODELPICK_INSTALL_DIR`). It only installs: it
never restarts a running `modelpick ui`. Run `bash scripts/deploy-local.sh` to
do it by hand.

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

The page says when it is done: `<body>` carries `data-settled` ("true" once
no op, Refresh or Diagnose and no reload is in flight), `data-gen` (how many
times it became settled) and `data-writes` (how many of those it sent); see
`internal/web/static/js/settle.js`. A test step that sends an op and is
followed by another action goes through `act(page, () => …)` in
`e2e/tests/helpers.js`, which waits for that signal. Every other action
first checks the page's count of ops against what the test waited for and
fails with "acted on an unsettled page" when it is behind, whatever the
timing; `e2e/tests/guard.spec.js` shows that check failing.

An opt-in smoke test against your real accounts, which makes real usage calls
(never forced past the floor):
`go test -tags live ./internal/live/ -v`.

## License

MIT, copyright Meatlover LLC. See [LICENSE](LICENSE).
