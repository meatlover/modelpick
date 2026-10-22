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

Prebuilt binaries will come later. Linux and macOS, amd64 and arm64.

## Commands

```
modelpick pick <profile> [--refresh]   JSON: the chosen vendor/account/model/thinking, or why none
modelpick ls [--refresh] [--json]      usage of every configured account: windows, % used, resets, reading age
modelpick ls models [--json]           models each configured account's subscription offers
modelpick config                       the effective configuration. No network.
modelpick doctor                       validates the config and everything it depends on. No usage calls.
```

`--refresh` forces a usage call even when the last reading is younger than the
floor. It still honours a server `Retry-After` that has not elapsed.

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
Concurrent picks on a cold cache make one call between them.

## `pick` status and exit code

| `status` | Exit | Meaning |
|---|---|---|
| `ok` | 0 | A model was picked and nothing failed along the way. |
| `ok_with_errors` | 2 | A model was picked, but something failed during this pick (a usage call or credential read failed), so at least one choice was skipped for a reason other than its own usage or schedule. |
| `failed` | 1 | No model: nothing available, unknown profile, config error, or any other failure. |

The JSON always has the fields `status, profile, vendor, account, model,
thinking, account_ref, reason, skipped, errors, warnings`; the last three are
lists, never `null`.

## Terms and credentials

Read this before you use modelpick.

- modelpick reads each tool's stored login (Claude Code's Keychain item or
  credentials file, Codex's `auth.json`, your OpenCode Go key file) **in
  memory**, only to call that vendor's usage endpoint.
- It **stores no credentials**: not in its cache, its output or its logs.
- It **never refreshes tokens** and never writes any vendor file. An expired
  Claude token is reported, not renewed; open Claude Code to refresh it.

All three usage endpoints are undocumented and may change without notice; a
changed response is reported as “usage unknown”, never as 0%.

Anthropic's terms say third-party tools “may not collect, store, or
intermediate” Claude login tokens. modelpick reads the token in memory and
stores nothing, but whether that is acceptable under your terms with each
vendor is your decision. You decide whether to use it.

## Development

Run `git config core.hooksPath .githooks` once after cloning; the pre-commit
hook then runs `bash scripts/check.sh` (gofmt, vet, tests, four-target
cross-build) and refuses a commit that fails.

An opt-in smoke test against your real accounts, which makes real usage calls
(never forced past the floor):
`go test -tags live ./internal/live/ -v`.
