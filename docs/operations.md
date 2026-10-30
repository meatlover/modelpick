# Operations guide

How modelpick behaves on a machine: what it reads and writes, when it reaches
the network and which processes it starts, how the UI is protected, how
logins are renewed, and what each `doctor` message means. Install it first
([getting started](getting-started.md)); the config format is in the
[configuration guide](configuration.md).

## Files and permissions

| Path | modelpick | Holds |
|---|---|---|
| The config file (see [location](configuration.md)) | reads; `ui` writes it | Accounts, schedules, profiles. Never a secret. |
| `~/.cache/modelpick/` (or `$XDG_CACHE_HOME/modelpick/`) | reads and writes; created `0700` | One `<account>.json` state file (`0600`) and one `<account>.lock` per account; removing an account in the UI deletes both, a hand edit leaves them (safe to delete). While `modelpick ui` runs, `ui.json` (`0600`); after its first edit, `config.before-ui.json` (`0600`). |
| A temporary directory `modelpick-renew-*` | creates and removes | Empty. A Claude renewal runs `claude` in it, so it finds no project files. Removed when the renewal ends. |
| Claude: Keychain item `Claude Code-credentials[-<8 hex>]`, else `<config_dir>/.credentials.json` | reads only | The Claude Code login. |
| Codex: `<codex_home>/auth.json`, `<codex_home>/models_cache.json` | reads only | The Codex login; the model list. |
| OpenCode Go: your `key_file` | reads only | One API key per line. |

Credentials are held in memory for the one request that needs them. They are
never written to the cache, printed, or logged. modelpick never writes a
vendor file and never handles a refresh token; see [renewal](#renewal).

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

## Network calls

modelpick reaches the network only for usage calls, one HTTPS `GET` per call:

| Vendor | Endpoint | Headers besides `Accept: application/json` |
|---|---|---|
| claude | `https://api.anthropic.com/api/oauth/usage` | `Authorization: Bearer <access token>`, `anthropic-beta: oauth-2025-04-20`, `User-Agent: claude-cli/<version> (external, cli)` |
| codex | `https://chatgpt.com/backend-api/wham/usage` | `Authorization: Bearer <access token>`, `ChatGPT-Account-Id: <account id from auth.json>`, `User-Agent: modelpick/<version>` |
| opencode-go | `https://opencode.ai/zen/go/v1/usage` | `Authorization: Bearer <key>`, `User-Agent: modelpick/<version>` |

The Claude `User-Agent` is the one Claude Code itself sends, with the version
that `claude --version` prints (`2.1.0` if that fails). `<version>` for the
other two is modelpick's release number: that release's at a release tag, the
next one with `-dev` between releases.

The endpoints are undocumented and may change. Each call has a 10-second
timeout. Redirects are never followed, so a login header cannot be sent to
another host; a 3xx is reported as `usage endpoint returned HTTP <n>`. A
response modelpick does not understand makes that account's usage
**unknown**; it is never read as 0%. Calls go through `HTTPS_PROXY` when it
is set.

When each command calls:

| Command | Usage calls |
|---|---|
| `pick <profile>`, `env <profile>` | Only for accounts whose capped choice is reached, at most one per account per floor. Uncapped and skipped choices never trigger one. |
| `pick` (no profile) | Each account any profile reaches, read at most once for the whole table, subject to the floor. |
| `ls [<account>…]` | The named accounts, or every account, subject to the floor. |
| `ls models`, `config`, `doctor` | None. |
| `doctor all` | One per account without a reading younger than the floor, skipping the floor; none for an account with an unexpired `Retry-After` or whose tool or login `doctor` reports as an error. |
| `ui` | Only the page's Refresh buttons and Diagnose (`doctor all`), which skip the floor, and one background first reading per run for an account with no saved reading, which goes through it. None passes an unexpired `Retry-After`. |

Before any of these usage calls, an expired Claude or Codex login is first
[renewed](#renewal).

## Child processes

| Process | When | Effect |
|---|---|---|
| `claude --version` | Before the first Claude usage call of a process, again only if it failed (5-second timeout). | Reads the version for the `User-Agent`. |
| `claude -p "Reply with exactly one word: ok" --model haiku …` | A Claude usage call is due and the access token has expired. | Claude Code renews its own login; costs a few tokens of that subscription, about 10 seconds. |
| `codex app-server` | A Codex usage call is due and the access token has expired. | Codex renews its own login and rewrites `auth.json`; no quota, about 3 seconds. |
| `opencode models opencode-go` | `doctor`, `ls models`, and the UI once per run per account (10-second timeout). | Lists OpenCode Go models; it may contact OpenCode. |
| `/usr/bin/security find-generic-password -s <service> -w` | macOS, each time a Claude login is read (20-second timeout). | Reads Claude Code's Keychain item. macOS may ask whether `security` may access it. |
| `open <url>` (macOS) or `xdg-open <url>` | `modelpick ui` without `--no-open`. | Opens the page in your browser. A failure is a note, not an error. |

A renewal has a 60-second limit. The renewal processes get your environment
without the variables that would make them log in some other way; the
[reference](reference.md#environment-variables) lists them.

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

`--refresh` on `pick`, `env` or `ls` skips the floor, not `Retry-After`. Use it
after you know usage changed, for example after a reset. Do not put it in a
loop.

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
browser unless you pass `--no-open`. Ctrl-C or SIGTERM stops it.

- `--port N` listens on port N. Without it, modelpick takes the previous run's
  port when it is free, else any free port.
- **One instance per user.** `<cache>/ui.json` holds the running instance's
  pid, port and token. A second `modelpick ui` that finds that instance alive
  and answering prints and opens its URL and exits 0, whatever `--port` says.
  A clean exit removes the file. Two `modelpick ui` started at the same moment
  can both serve; the one that wrote `ui.json` last is the one later starts
  reuse.
- **A newer install.** If `modelpick` is replaced on disk while the UI runs
  (for example by the autodeploy hook after a merge), the running UI keeps the
  old code. It re-checks its own executable file each time the page reads its
  state; once the file is gone, or is a different file, size or modification
  time than at start, the page shows a grey note,
  `A newer modelpick is installed`, for the rest of that run. Stop it with
  Ctrl-C where you ran `modelpick ui` and run `modelpick ui` again. Nothing is
  locked: edits keep working while the note shows. If the executable cannot be
  checked, no note appears.

### What protects the page

The page can change your config, so only your browser, through the URL
printed, may use it:

- It listens on `127.0.0.1` only.
- **Host**: a request whose `Host` is not `127.0.0.1:<port>` or
  `localhost:<port>` is refused (`403`), which stops DNS rebinding.
- **Origin**: a request that carries an `Origin` must come from that same
  address; a request other than `GET` or `HEAD` must carry one (`403`).
- **Token**: each run makes a new random 32-byte token. `GET /?t=<token>`
  sets it as an `HttpOnly`, `SameSite=Strict` cookie named for the port and
  redirects to `/`; every other request needs that cookie (`401`). The token
  in the URL is accepted only there and by the single-instance check.
- **Headers**: every response carries
  `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'`,
  `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`. An edit's
  request body is limited to 1 MiB.

The page shows readings and model names, never a credential.

### Using the page

**Usage.** Opening or reloading the page shows the saved readings only, as the
last `pick`, `ls` or Refresh stored them; it calls no vendor. Three things
call out. The Refresh buttons, like `ls --refresh`, skip the floor; a
`Retry-After` still holds. Diagnose runs `doctor all` (below). And an
account with no saved reading gets one first reading in the background per
run, when the server starts or the account is added; it is an ordinary
read, so the floor and `Retry-After` apply. Refresh is enabled for an expired
account that can be renewed.

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
grouped into one object, the list form when days differ. A hand-laid-out file
therefore changes layout on the UI's first edit, though not meaning.

**The safety copy.** Before its first write in each run, the UI copies the
config file as it was to `<cache>/config.before-ui.json` (`0600`). It is a
safety copy, not an undo: the next run's first write replaces it. If the copy
cannot be saved, the edit is refused (`Could not save the safety copy`). To go
back to the file as it was before this run (default paths):
`cp ~/.cache/modelpick/config.before-ui.json ~/.config/modelpick/config.json`.

## Renewal

When a usage call is due and a Claude or Codex access token has expired,
modelpick asks the vendor's tool to renew it, then makes the call. For Claude
it runs `claude -p` with a tiny prompt on haiku: a few tokens of that
subscription, about 10 seconds. For Codex it runs `codex app-server` and asks
for the account with a token refresh: no quota, about 3 seconds. modelpick
never writes a vendor file or handles a refresh token; the tool renews its own
login. OpenCode Go keys do not expire.

Renewal happens only when the last good reading is older than the floor. Even
`--refresh` and the page's Refresh do not renew for a fresher reading. A failed
renewal counts as an attempt: it is retried at most once per
`min_call_interval_minutes`, and the old reading shows with
`renewal failed: <reason>`. Ctrl-C ends `ls`, `pick`, `env` and `doctor` with
exit 130 and cancels a renewal in progress.

Once the refresh token itself has expired, log in again: `/login` in Claude
Code (`CLAUDE_CONFIG_DIR=<config_dir> claude`), or `codex login`
(`CODEX_HOME=<codex_home>`). For Claude, `doctor` warns
`login_expiry_warning_days` before that happens.

## Routine tasks

**Add an account.** Log in with the vendor tool first (for a second Claude
account: `CLAUDE_CONFIG_DIR=~/.claude-2 claude`, then log in). Add it under
`accounts`, reference it from a profile, run `modelpick doctor`.

**Add an OpenCode Go key.** Append it to the key file and add an account with
the next `key_line`. `key_line` counts key lines only (blank lines and `#`
comments are skipped), so inserting a key in the middle renumbers the ones
after it; append instead.

**Remove an account.** Remove it from `accounts` and from every profile
(the config is rejected while a profile refers to a missing account), then
delete its `<account>.json` and `<account>.lock`. Removing an account in the
UI deletes both for you; a hand-edited removal or rename leaves them, and
they are safe to delete.

**Change caps or order.** Edit the profile. `modelpick config` shows the
effective result, and `doctor` warns when a cap array has more values than the
windows the account last reported.

## Troubleshooting

Start with `modelpick doctor`, then `modelpick ls`, whose `STATE` column (the
second) shows each account's current state. An account whose usage call failed
shows `error` there, and its message is on a line below the table
(`<account>: <message>`); `ls --json` carries the full text in `state`.

`doctor` prints each finding as `<subject>: <message>`; `pick` puts the same
kinds of messages in `errors` and `warnings`. In the table, `<…>` stands for
a value. A test checks that every message `doctor` can print has a row.

| Message | Meaning | Fix |
|---|---|---|
| `config <path>: <field>: <problem>` | The config file is invalid; the field is named, for example `profiles.tester[1].skip_during`. | Fix that field; the [configuration guide](configuration.md) has the rules. |
| `no config file at <path>. Start from the example: …` | There is no config file where modelpick looks. | Create it; see [getting started](getting-started.md#first-config). |
| `claude not found on PATH`, `codex not found on PATH`, `opencode not found on PATH` | `doctor` cannot find the tool. | Install it or fix `PATH`. `pick` still works without it. |
| `no credential: not found in keychain: <service> or file: <config_dir>/.credentials.json` | No Claude login for that `config_dir`. | Log in with `CLAUDE_CONFIG_DIR=<config_dir> claude`. Check that the path is the one you logged in with; the Keychain name is derived from it. |
| `no credential: not found: <codex_home>/auth.json` | No Codex login there. | `CODEX_HOME=<codex_home> codex login`. |
| `no credential: key file <key_file> line <n>` | The OpenCode Go key file has fewer than `key_line` keys. | Add the key, or fix `key_line`. Blank lines and `#` lines do not count. |
| `credential unreadable (<source>): <error>` | The login exists but cannot be read or is not in the expected shape, or the Keychain lookup failed or timed out. | Log in again with the tool. On macOS, look for a Keychain permission prompt. |
| `token expired — the next usage call asks Claude Code to renew it; if that fails, log in again with /login` | The Claude access token is past its expiry. A warning. | Nothing yet: the next due call renews it. If `renewal failed: <reason>` shows, use `/login` in Claude Code. |
| `token expired — the next usage call asks Codex to renew it; if that fails, run codex login` | The same for Codex. | Nothing yet. If renewal fails, run `codex login`. |
| `login expires <time>; log in again in Claude Code with this account before then` | The Claude refresh token, which renewal never extends, expires within `login_expiry_warning_days`. `<time>` is UTC. A warning. | Log in again (`/login`) with this account before then. |
| `login expired <time>; renewal fails until you log in again in Claude Code with this account` | The refresh token has expired. An error, but it does not block config edits in the UI. | Log in again (`/login`) with this account. |
| `cannot list models, thinking values not checked: <reason>` | The account's model list could not be read, so `doctor` cannot check its thinking values. For a Codex account never used, the reason is `codex: <codex_home>/models_cache.json not found; run codex once to create it`. | Run `codex` once with that `CODEX_HOME`, or fix what the reason names (`opencode models` failed, for example). |
| `model <m> not offered by <account>` | The model is not in the account's model list. A warning: the list may be out of date. | Check `modelpick ls models`. |
| `thinking "<x>" not supported by <model> (supported: <values>)` | The thinking value is not one the model takes. | Use one of the listed values, or `null`. |
| `<account>: account reports <n> windows (<lengths>), cap has <m> values; ignored the last <k>` | The cap array has more values than the account's last reading has windows. A shorter array is fine. | Drop the extra values; positions are in [this order](configuration.md#caps). |
| `<account>: unrecognised window "<label>" capped at default_cap <cap>` | A window of unknown length; it always uses `default_cap`. | Nothing; report it if the vendor added a window. |
| `profile <p>, choice <i>: never picked, choice <j> always wins first` | Choice j comes earlier and is usable whenever choice i is (uncapped, or the same account and model with caps at least as high), so i can never be picked. For a group, a later position is covered when, at every minute it can run, some member of an earlier group outranks it; a group is covered when each of its members is. | Reorder or remove a choice, narrow choice j's schedule, or give an uncapped member a cap. |
| `profile <p>, choice 2b: same as choice 2a; doubles its chance in the load balancing group` | Two members of a group have the same account, model and thinking. | Remove one, or change its model. |
| `schedule <name>: empty` | The schedule has no days and no hours (as the UI creates it), so it never matches. | Paint its hours in the UI, or fill in `days` and `utc`. |
| `last usage reading failed: <error>` | The last usage call for this account failed and no reading has replaced it. Not shown while a `Retry-After` holds or when another finding names the cause. | Read the error; it is one of the call errors below. |
| `cannot read stored usage: <error>` | The account's state file cannot be read. `doctor all` still calls, which rewrites it. | Check permissions on the cache directory, or delete the file. |
| `cannot update stored usage: <error>` | `doctor all` could not write the reading to the cache. | Check permissions on the cache directory. |
| `unrecognised window group "<group>"` | Claude reported a window kind modelpick does not know. It is ignored. | Nothing; report it. |
| `unparseable resets_at; reset time unknown` | Claude sent a reset time modelpick cannot read; that window's reset is unknown. | Nothing; report it if it persists. |
| `additional_rate_limits present; not parsed in v1` | Codex reported extra limits modelpick does not read yet. | Nothing. |
| `unrecognised status "<status>" on <window>` | OpenCode Go reported a window status modelpick does not know. | Nothing; report it. |
| `usage endpoint returned HTTP 401; log in to <tool> with this account` | The vendor rejected the login. | Log in again with the tool. |
| `usage endpoint returned HTTP <n>` | Any other HTTP failure, including a refused redirect (3xx). | Usually transient; the next call after the floor retries. |
| `token expired; open <tool> with this account to refresh it` | The token has expired and no renewal was made for it. | Open the tool with this account. |
| `token expired and <tool> could not renew it (<reason>); log in again in <tool> with this account` | Renewal failed and there is no older reading to fall back on. | Log in again. |
| `renewal failed: <reason>` | Renewal failed; an older reading is used. | Log in again if it keeps failing. |
| `rate-limited until <time>`, `rate-limited (no retry time given)` | The vendor answered 429. With a `Retry-After`, calls wait until `<time>`. | Wait; `--refresh` does not override it. |
| `unrecognised response shape` | The vendor changed its usage response. | Usage for that account is unknown until modelpick is updated. File an issue with the response, credentials removed. |
| `cache unavailable: <error>` | The cache directory or lock file cannot be used, so no usage call is made. | Check permissions on `~/.cache/modelpick`. |
| `no usage reading; last call at <t> failed: <error>; next call allowed at <t>` | The last call failed and the floor has not passed. | Wait, or use `--refresh` once. |
| `no usage reading yet; next call allowed at <t>` | A call was attempted within the floor and left neither a reading nor an error (the process was killed, for example). | Wait, or use `--refresh` once. |
| `usage response had no windows`, `usage unavailable` | The reading has no window for this model, or there is none. | See the account's `ls` line. |

`doctor` and `doctor all` exit 1 if they found any error, else 0.

## FAQ

**Why was my first choice not picked?** `pick <profile>` lists every choice it
passed over in `skipped`, with the reason; see
[how modelpick decides](concepts.md#how-pick-walks-a-profile).

**Why is usage "unknown" instead of 0%?** A reading that failed or that
modelpick could not understand says nothing about how much is left. Treating
it as 0% would pick an exhausted account. Give the profile an uncapped last
choice so it always has an answer.

**Does modelpick start Claude Code, Codex or OpenCode?** Only to list
models, read the Claude version, or renew an expired login, as listed under
[child processes](#child-processes). It never starts a coding session; a
launcher does that ([reference](reference.md#building-a-launcher)).

**Can I run it from cron or many terminals at once?** Yes. The floor holds
across processes, so frequent runs read the cache instead of the vendor.
