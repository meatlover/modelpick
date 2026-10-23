# Admin guide

How to run modelpick day to day: what it reads and writes, when it reaches the
network, how to call it from a launcher, and what to do when `doctor` or `pick`
reports a problem. Install it first ([installation guide](install.md)); the
config format is in the [configuration guide](configuration.md).

## What modelpick reads and writes

| Path | modelpick | Holds |
|---|---|---|
| The config file (see [location](configuration.md)) | reads | Accounts, schedules, profiles. Never a secret. |
| `~/.cache/modelpick/` (or `$XDG_CACHE_HOME/modelpick/`) | reads and writes; created `0700` | One `<account>.json` state file (`0600`) and one `<account>.lock` per account. |
| Claude: Keychain item `Claude Code-credentials[-<8 hex>]`, else `<config_dir>/.credentials.json` | reads only | The Claude Code login. |
| Codex: `<codex_home>/auth.json`, `<codex_home>/models_cache.json` | reads only | The Codex login; the model list. |
| OpenCode Go: your `key_file` | reads only | One API key per line. |

Credentials are held in memory for the one request that needs them. They are
never written to the cache, printed, or logged. modelpick never refreshes a
token and never writes a vendor file.

### State files

`<account>.json` records the last good reading, when the last call was
attempted, the last error and a server `Retry-After` deadline:

```json
{"last_good": {"taken_at": "...", "windows": [...]}, "last_attempt": "...",
 "last_error": "", "last_error_at": "...", "retry_until": "..."}
```

Files are replaced atomically (temp file, then rename). A missing or corrupt
file counts as empty. An account whose token was expired from the start has a
lock file but no state file, because no call was ever made for it.

The state file is keyed by **account name**. Renaming an account in the config
starts it with an empty state; the old file stays until you delete it.

## When modelpick reaches the network

| Command | Usage calls | Other outside contact |
|---|---|---|
| `pick <profile>` | Only for accounts whose capped choice is reached, at most one per account per floor. Uncapped choices and skipped choices never trigger one. | `claude --version` once, for the User-Agent, when a Claude call is made. |
| `ls` | Every configured account, subject to the floor. | Same. |
| `ls models` | None. | Runs `opencode models opencode-go`, which may contact OpenCode. |
| `config` | None. | None. |
| `doctor` | None. | Runs `opencode models opencode-go`; reads the Keychain. |

The endpoints are undocumented and may change:

| Vendor | Endpoint |
|---|---|
| claude | `GET https://api.anthropic.com/api/oauth/usage` |
| codex | `GET https://chatgpt.com/backend-api/wham/usage` |
| opencode-go | `GET https://opencode.ai/zen/go/v1/usage` |

Each call has a 10-second timeout. Redirects are never followed, so a login
header cannot be sent to another host. A response modelpick does not
understand makes that account's usage **unknown**; it is never read as 0%.

## The call floor

At most one usage call per account per `min_call_interval_minutes` (default
15), across all modelpick processes on the machine:

- A reading younger than the floor is used as is.
- The attempt is recorded **before** the call, so a failed call also uses up
  the floor. Until the floor passes, the last good reading is used with a
  warning, or, with none, usage is unknown and the error says when the next
  call is allowed.
- Concurrent processes take the account's lock (`flock`) in turn; the second
  one finds the first one's attempt and makes no call. A lock held by a
  process that died is released by the kernel.
- A server `Retry-After` (capped at 24 hours) blocks calls until it passes,
  even with `--refresh`.
- If the lock cannot be taken or the attempt cannot be written, no call is made
  (the floor fails closed). modelpick uses whatever stored reading it can read
  and warns `cache unavailable`.

`--refresh` on `pick` or `ls` skips the floor, not `Retry-After`. Use it after
you know usage changed, for example after a reset. Do not put it in a loop.

To force the next call without `--refresh`, delete that account's state file:
`rm ~/.cache/modelpick/<account>.json`. Deleting it also forgets the
`Retry-After` deadline, so do not do this to get around a rate limit.

## Calling modelpick from a launcher

`pick` always prints one JSON object on stdout, and its exit code agrees with
`status`:

| Exit | `status` | What the caller should do |
|---|---|---|
| 0 | `ok` | Use `vendor`, `model`, `thinking`, `account_ref`. |
| 2 | `ok_with_errors` | Use the pick, and surface `errors`: an account is broken and was skipped. |
| 1 | `failed` | Do not launch. Show `reason`. |

`account_ref` is the chosen account's config object with absolute paths:
`config_dir` for Claude (set `CLAUDE_CONFIG_DIR`), `codex_home` for Codex (set
`CODEX_HOME`), `key_file` and `key_line` for OpenCode Go (which key to use).
A minimal shell launcher:

```bash
out=$(modelpick pick coder-difficult); rc=$?
[ "$rc" -eq 1 ] && { echo "$out" | jq -r .reason >&2; exit 1; }
[ "$rc" -eq 2 ] && echo "$out" | jq -c .errors >&2
case "$(echo "$out" | jq -r .vendor)" in
  claude) CLAUDE_CONFIG_DIR=$(echo "$out" | jq -r .account_ref.config_dir) \
            exec claude --model "$(echo "$out" | jq -r .model)" ;;
  codex)  CODEX_HOME=$(echo "$out" | jq -r .account_ref.codex_home) \
            exec codex -m "$(echo "$out" | jq -r .model)" ;;
  opencode-go) exec opencode -m "$(echo "$out" | jq -r .model)" ;;
esac
```

The flags each tool takes for model and thinking level are the tool's own;
check them against your installed versions. modelpick does not switch the
OpenCode key for you: `account_ref.key_line` says which key was picked, and
installing that key is the launcher's job.

## Routine tasks

**Add an account.** Log in with the vendor tool first (for a second Claude
account: `CLAUDE_CONFIG_DIR=~/.claude-2 claude`, then log in). Add it under
`accounts`, reference it from a profile, run `modelpick doctor`.

**Add an OpenCode Go key.** Append it to the key file and add an account with
the next `key_line`. `key_line` counts key lines only (blank lines and `#`
comments are skipped), so inserting a key in the middle renumbers the ones
after it; append instead.

**Remove an account.** Remove it from `accounts` and from every profile
(`pick` rejects a reference to a missing account), then delete its
`<account>.json` and `<account>.lock`.

**Change caps or order.** Edit the profile. `modelpick config` shows the
effective result, and `doctor` warns when a cap array no longer matches the
number of windows the account last reported.

**An expired token.** modelpick never refreshes tokens. Open the vendor tool
with that account (`CLAUDE_CONFIG_DIR=<config_dir> claude`) and it refreshes
its own login. Until then the account uses its last good reading, if any.

## Troubleshooting

Start with `modelpick doctor`, then `modelpick ls`, whose `STATE` column shows
each account's current state.

| Message | Meaning | Fix |
|---|---|---|
| `claude not found on PATH` (or `codex`, `opencode`) | `doctor` cannot run the tool. | Install it or fix `PATH`. `pick` still works. |
| `no credential: not found in keychain: ... or file: ...` | No Claude login for that `config_dir`. | Log in with `CLAUDE_CONFIG_DIR=<config_dir> claude`. Check the path is the exact one you logged in with; the Keychain name is derived from it. |
| `no credential: not found: <codex_home>/auth.json` | No Codex login there. | `CODEX_HOME=<codex_home> codex login`. |
| `credential unreadable (...)` | The login exists but is not in the expected shape, or the Keychain lookup failed or timed out. | Log in again with the tool. On macOS, check for a Keychain permission prompt. |
| `token expired — open Claude Code with this account to refresh it` (or Codex) | The stored token is past its expiry. | Open the tool with that account. |
| `usage endpoint returned HTTP 401; log in to <tool> with this account` | The vendor rejected the login. | Log in again. |
| `usage endpoint returned HTTP <n>` | Any other HTTP failure, including a refused redirect (3xx). | Usually transient; the next call after the floor retries. |
| `rate-limited until <time>` | The server sent `Retry-After`. | Wait; `--refresh` does not override it. |
| `unrecognised response shape` | The vendor changed its usage response. | Usage for that account is unknown until modelpick is updated. File an issue with the redacted response. |
| `cache unavailable: ...` | The cache directory or lock file cannot be used. | Check permissions on `~/.cache/modelpick`. No usage calls are made until this is fixed. |
| `no usage reading; last call at ... failed: ...; next call allowed at ...` | The last call failed and the floor has not passed. | Wait, or use `--refresh` once. |
| `profile <p> has no uncapped choice; pick can return failed` | Every choice in the profile is capped. | Add a last choice without `cap` if you always want an answer. |
| `account reports N windows (...), cap has M values; filled/ignored ...` | The plan reports a different number of windows than your `cap` array has. | Make the array match the windows listed; positions are in [this order](configuration.md#caps). |
| `thinking "x" not supported by <model> (supported: ...)` | The thinking value is not one the model takes. | Use one of the listed values, or `null`. |
| `model <m> not offered by <account>` | The model is not in the account's model list. | Check `modelpick ls models`. A warning only: the list may be out of date. |

`doctor` exits 1 if it found any error, else 0.

## Live smoke test

From a checkout, against your real accounts:

```
go test -tags live ./internal/live/ -v
```

It makes real usage calls, through the same floor as the CLI (never forced
past it). It fails if a vendor's response is not in a shape modelpick
recognises; other outcomes (no reading, expired token, an older reading) are
logged, with window lengths and percents only. It is never run by
`scripts/check.sh` or CI.
