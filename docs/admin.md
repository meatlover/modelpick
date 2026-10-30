# Admin guide

How to run modelpick day to day: what it reads and writes, when it reaches the
network, how to call it from a launcher, and what to do when `doctor` or `pick`
reports a problem. Install it first ([installation guide](install.md)); the
config format is in the [configuration guide](configuration.md).

## What modelpick reads and writes

| Path | modelpick | Holds |
|---|---|---|
| The config file (see [location](configuration.md)) | reads; `ui` writes it | Accounts, schedules, profiles. Never a secret. |
| `~/.cache/modelpick/` (or `$XDG_CACHE_HOME/modelpick/`) | reads and writes; created `0700` | One `<account>.json` state file (`0600`) and one `<account>.lock` per account. While `modelpick ui` runs, `ui.json` (`0600`); after its first edit, `config.before-ui.json`. |
| Claude: Keychain item `Claude Code-credentials[-<8 hex>]`, else `<config_dir>/.credentials.json` | reads only | The Claude Code login. |
| Codex: `<codex_home>/auth.json`, `<codex_home>/models_cache.json` | reads only | The Codex login; the model list. |
| OpenCode Go: your `key_file` | reads only | One API key per line. |

Credentials are held in memory for the one request that needs them. They are
never written to the cache, printed, or logged. modelpick never writes a
vendor file and never handles a refresh token. When a call is due and a Claude
or Codex login has expired, it asks the vendor's tool to renew it (see
"An expired token" under Routine tasks).

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
| `pick <profile>` | Only for accounts whose capped choice is reached, at most one per account per floor. Uncapped choices and skipped choices never trigger one. | `claude --version` once, for the User-Agent, when a Claude call is made. An expired login is first renewed: `claude -p` (a few tokens) for Claude, `codex app-server` (no quota) for Codex. |
| `env <profile>` | Same as `pick <profile>`. | Same. |
| `pick` (no profile) | Each account any profile reaches, read at most once for the whole table, subject to the floor. | Same. |
| `ls [<account>…]` | The named accounts, or every configured account when none are named, subject to the floor. | Same. |
| `ls models` | None. | Runs `opencode models opencode-go`, which may contact OpenCode. |
| `config` | None. | None. |
| `doctor` | None; `doctor all` makes one usage call per account without a fresh reading, except an account with an unexpired `Retry-After` or a tool or credential error. An expired login is renewed first. | Runs `opencode models opencode-go`; reads the Keychain; may run `claude -p` or `codex app-server` to renew a login. |
| `ui` | Only the page's Refresh buttons and Diagnose (`doctor all`), which skip the floor, and one background first reading per run for an account with no saved reading, which goes through it. None passes an unexpired `Retry-After`. An expired login is renewed first. | Runs `opencode models opencode-go` once per run per account; reads the Keychain; may run `claude -p` (a few tokens) or `codex app-server` (no quota) to renew an expired login. |

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

## The UI: `modelpick ui`

```
modelpick ui [--port N] [--no-open]
```

`modelpick ui` serves a page for reading usage and editing the config file. It
runs in the foreground on `127.0.0.1` only, prints one line,
`modelpick ui: http://127.0.0.1:<port>/?t=<token>`, and opens that URL in your
browser unless you pass `--no-open` (a browser that fails to open is a note,
not an error). Ctrl-C or SIGTERM stops it. The token is new for each run; the
page works only in a browser that opened that URL.

- `--port N` listens on port N. Without it, modelpick takes the previous run's
  port when it is free, else any free port.
- **One instance per user.** `<cache>/ui.json` holds the running instance's
  pid, port and token. A second `modelpick ui` that finds that instance alive
  and answering prints and opens its URL and exits 0, whatever `--port` says.
  A clean exit removes the file. Two `modelpick ui` started at the same moment
  can both serve; the one that wrote `ui.json` last is the one later starts
  reuse.

**Usage.** Opening or reloading the page shows the saved readings only, as the
last `pick`, `ls` or Refresh stored them; it calls no vendor. Three things
call out. The Refresh buttons, like `ls --refresh`, skip the floor; a
`Retry-After` still holds. Diagnose runs `doctor all` (below). And an
account with no saved reading gets one first reading in the background per
run, when the server starts or the account is added; it is an ordinary
read, so the floor and `Retry-After` apply.

**Doctor status.** The top-right status lists every `doctor` problem. Each
one has **Go to**, which opens the tab that holds its item, scrolls to it,
moves the focus there and lights it up for a moment: an account's row on
Accounts (each account's, for a problem shared by several), a choice's card
or a group's frame on its profile, a schedule in the Schedules list. A
problem with the whole file has no Go to.

**Diagnose**, left of the status, runs `doctor all`: it calls the vendor once
for each account with no fresh reading, then checks again, and the status
shows the result. A Diagnose sent while one runs (from another tab) makes no
calls of its own: it waits for the running one and shows its result.

**Editing.** Every change applies to the file at once; there is no Save
button and no undo. A change is refused, and the file left as it was, when it
would add a `doctor` error the file did not already have. If the file changes
on disk while the page is open, the page asks whether to load the file's
version or apply your change on top of it. While the file does not parse
(a hand edit gone wrong), every tab is read-only and the page shows
`Cannot edit — <parse error>`; it reloads once the file parses again.

Each account's model list is read once per run, so an edit does not run
`opencode models` every time. A listing that failed (a timeout, `opencode`
not installed yet) is tried again when you reload the page or come back to
it.

**Load balancing.** Where several choices should share one position, the
Profiles tab can group them (the file form is in the
[configuration guide](configuration.md#load-balancing-groups)).

- **⇉ Load balance…** above the choice list puts a checkbox on every choice
  not already in a group. Tick two or more and press **Load balance N
  models** (Cancel or Esc backs out). The group appears at the first ticked
  choice's place and the others join it in their order. The button is there
  only while two or more choices are outside groups.
- A group is a frame around its members, numbered `2a`, `2b`, with the header
  `algorithm: random` and **Stop load balancing**, which turns the members
  back into plain choices in place. **+ Add Model** at the bottom of the frame
  adds a member, preferring an account the group does not use yet.
- Drag a choice into or out of the frame, or a whole group among the
  positions; a group cannot go into another group. With the keyboard,
  Cmd/Ctrl+Arrow moves a member out at the frame's edge, and a plain choice
  moves past a group, not into it. To join by keyboard use **Load balance…**.
- A group left with one member (a removal, a drag out, a removed account) is
  dissolved and the member stays as a plain choice; with none it is gone. The
  file never holds a one-member group.

**The first write reformats the file.** The UI writes canonical JSON: 2-space
indent, top-level keys in the order `version, settings, accounts, schedules,
profiles`, map keys sorted, trailing `"default"` cap elements dropped.
A schedule you paint is rewritten from the grid: days with the same hours
grouped into one object, the list form when days differ. A hand-laid-out file therefore changes layout on the UI's first edit, though
not meaning.

**The safety copy.** Before its first write in each run, the UI copies the
config file as it was to `<cache>/config.before-ui.json` (`0600`). It is a
safety copy, not an undo: the next run's first write replaces it. If the copy
cannot be saved, the edit is refused (`Could not save the safety copy`). To go
back to the file as it was before this run (default paths):
`cp ~/.cache/modelpick/config.before-ui.json ~/.config/modelpick/config.json`.

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

Or let the shell do the parsing: `modelpick env` prints the same decision as
`sh` export lines (and `false` when there is no choice), so a launcher can be

```bash
out=$(modelpick env coder-difficult)
[ -n "$out" ] || exit 1
eval "$out" || exit 1
case "$MODELPICK_VENDOR" in
  claude) exec claude --model "$MODELPICK_MODEL" ;;   # CLAUDE_CONFIG_DIR is set or unset for you
  codex)  exec codex -m "$MODELPICK_MODEL" ;;         # CODEX_HOME is set
  opencode-go) exec opencode -m "$MODELPICK_MODEL" ;; # MODELPICK_KEY_FILE / _LINE name the key
  *) echo "modelpick: no vendor" >&2; exit 1 ;;
esac
```

The empty check is there because a modelpick that is missing from `PATH` or
crashed prints nothing, and `eval ""` succeeds, which would launch with
whatever `MODELPICK_*` values an earlier eval left in the environment.

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

**An expired token.** When a usage call is due and the login has expired,
modelpick asks the vendor's tool to renew it, then makes the call. For Claude
it runs `claude -p` with a tiny prompt on haiku: a few tokens of that
subscription, about 10 seconds. For Codex it runs `codex app-server` and asks
for the account with a token refresh: no quota, about 3 seconds. modelpick
never writes a vendor file or handles a refresh token; the tool renews its own
login.

Renewal happens only when the last good reading is older than the floor. Even
`--refresh` and the page's Refresh do not renew for a fresher reading. A failed
renewal counts as an attempt: it is retried at most once per
`min_call_interval_minutes`, and the old reading shows with `renewal failed:
<reason>`. Once the refresh token itself has expired, log in again: `/login`
in Claude Code (`CLAUDE_CONFIG_DIR=<config_dir> claude`), or `codex login`
(`CODEX_HOME=<codex_home>`). Ctrl-C ends `ls`, `pick`, `env` and `doctor` with
exit 130 and cancels a renewal in progress.

## Troubleshooting

Start with `modelpick doctor`, then `modelpick ls`, whose `STATE` column (the
second) shows each account's current state. An account whose usage call failed
shows `error` there, and its message is on a line below the table
(`<account>: <message>`); `ls --json` carries the full text in `state`.

| Message | Meaning | Fix |
|---|---|---|
| `claude not found on PATH` (or `codex`, `opencode`) | `doctor` cannot run the tool. | Install it or fix `PATH`. `pick` still works. |
| `no credential: not found in keychain: ... or file: ...` | No Claude login for that `config_dir`. | Log in with `CLAUDE_CONFIG_DIR=<config_dir> claude`. Check the path is the exact one you logged in with; the Keychain name is derived from it. |
| `no credential: not found: <codex_home>/auth.json` | No Codex login there. | `CODEX_HOME=<codex_home> codex login`. |
| `credential unreadable (...)` | The login exists but is not in the expected shape, or the Keychain lookup failed or timed out. | Log in again with the tool. On macOS, check for a Keychain permission prompt. |
| `token expired — the next usage call asks Claude Code to renew it; if that fails, log in again with /login` (or Codex) | The stored token is past its expiry. | Nothing yet: the next due call renews it. If `renewal failed: <reason>` shows, log in again (`/login` in Claude Code, `codex login`). |
| `login expires <time>; log in again in Claude Code with this account before then` (warning) | The refresh token, which renewal never extends, expires within `login_expiry_warning_days` (default 7). `<time>` is UTC. Only Claude logins report an expiry. | Log in again in Claude Code (`/login`) with this account before then. |
| `login expired <time>; renewal fails until you log in again in Claude Code with this account` (error) | The refresh token has expired, so renewing the access token fails. It does not block config edits. | Log in again in Claude Code (`/login`) with this account. |
| `usage endpoint returned HTTP 401; log in to <tool> with this account` | The vendor rejected the login. | Log in again. |
| `usage endpoint returned HTTP <n>` | Any other HTTP failure, including a refused redirect (3xx). | Usually transient; the next call after the floor retries. |
| `rate-limited until <time>` | The server sent `Retry-After`. | Wait; `--refresh` does not override it. |
| `unrecognised response shape` | The vendor changed its usage response. | Usage for that account is unknown until modelpick is updated. File an issue with the redacted response. |
| `cache unavailable: ...` | The cache directory or lock file cannot be used. | Check permissions on `~/.cache/modelpick`. No usage calls are made until this is fixed. |
| `no usage reading; last call at ... failed: ...; next call allowed at ...` | The last call failed and the floor has not passed. | Wait, or use `--refresh` once. |
| `profile <p>, choice <i>: never picked, choice <j> always wins first` | Choice j comes earlier and is usable whenever choice i is (uncapped, or the same account and model with caps at least as high), so i can never be picked. | Reorder or remove a choice, or narrow choice j's schedule. |
| `profile <p>, choice <i>: never picked, choice <j> always wins first` (group) | The same rule for groups: a later position is covered when, at every minute it can run, some member of an earlier group outranks it (a group member that is uncapped, say). A later group is covered when each of its members is. | As above; for an uncapped member, give it a cap or a schedule. |
| `profile <p>, choice 2b: same as choice 2a; doubles its chance in the load balancing group` | Two members of a group have the same account, model and thinking. | Remove one, or change its model. |
| `schedule <name>: empty` | The schedule has no days and no hours (as the UI creates it), so it never matches. | Paint its hours in the UI, or fill in `days` and `utc`. |
| `account reports N windows (...), cap has M values; ignored the last K` | Your `cap` array has more values than the plan reports windows. A shorter array is fine: the missing positions use `default_cap`. | Drop the extra values; positions are in [this order](configuration.md#caps). |
| `thinking "x" not supported by <model> (supported: ...)` | The thinking value is not one the model takes. | Use one of the listed values, or `null`. |
| `model <m> not offered by <account>` | The model is not in the account's model list. | Check `modelpick ls models`. A warning only: the list may be out of date. |

`modelpick doctor all` does what `doctor` does and also checks that each
account's usage can be read. It uses a saved reading younger than
`min_call_interval_minutes` as is. Otherwise it calls the vendor once for that
account, like Refresh: the call ignores the floor but not a server
`Retry-After`, an expired login is renewed first, and an account whose tool or credential
`doctor` already reports as an error is not called. The accounts are called at
the same time. A failed call shows as the warning `last usage reading failed: <error>`,
except when the vendor answered with a rate limit and `Retry-After`, which is
not reported. A state file that cannot be read shows as `cannot read stored
usage: <error>` (the call still goes ahead and rewrites it), and a cache that
cannot be written as `cannot update stored usage: <error>`.

`doctor` exits 1 if it found any error, else 0; so does `doctor all`.

## Live smoke test

From a checkout, against your real accounts:

```
go test -tags live ./internal/live/ -v
```

It makes real usage calls, through the same floor as the CLI (never forced
past it). It does not renew an expired login unless `MODELPICK_LIVE_RENEW=1`
is set, because a Claude renewal spends a few tokens. It fails if a vendor's response is not in a shape modelpick
recognises; other outcomes (no reading, expired token, an older reading) are
logged, with window lengths and percents only. It is never run by
`scripts/check.sh` or CI; `check.sh` only vets it (`go vet -tags live ./...`)
so it keeps compiling.
