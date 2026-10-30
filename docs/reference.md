# Reference

Every command, flag, output field, status, exit code and environment variable,
for people who script modelpick or write a launcher around it. The ideas are
in [how modelpick decides](concepts.md); the config file is in the
[configuration guide](configuration.md).

## Commands

`modelpick help` prints the list below; `modelpick help <command>` and
`modelpick <command> --help` (or `-h`) print one command's usage. Both go to
stdout and exit 0. An unknown command, a missing one, or a bad argument prints
the reason and the usage on stderr and exits 1.

### `modelpick pick`

```
modelpick pick [<profile>] [--refresh]
```

With a profile: the [pick JSON](#pick-json) on stdout, exit code from its
`status`. A bad argument (an unknown flag, a second profile, an empty
profile `""`) still prints a `failed` result on stdout, so a launcher that
reads stdout sees it, and the usage on stderr.

With no profile: a table of every profile's pick, then each distinct error
once (`<account>: <message>`), then `For JSON, name a profile: modelpick pick
<profile>`. Each account is read at most once for the whole table. Exit 1 if
any profile failed, else 2 if any error was reported, else 0.

```
PROFILE          VENDOR       ACCOUNT     MODEL                      THINKING
coder-difficult  codex        codex-main  gpt-6.1-sol                high
reviewer         opencode-go  go-2        opencode-go/deepseek-v4.1  -
tester           opencode-go  go-2        opencode-go/deepseek-v4.1  -
```

A failed profile's row holds its name and the reason only.

`--refresh` makes a usage call for each account a capped choice reaches, even
when the last reading is younger than the floor. It never overrides an
unexpired server `Retry-After`, and it does not renew an expired login for a
reading younger than the floor.

### `modelpick env`

```
modelpick env <profile> [--refresh]
```

The same pick as `sh` lines for `eval`; see [`env` output](#env-output).
Exit code as for `pick <profile>`. Without a profile it fails (exit 1).

### `modelpick ls`

```
modelpick ls [<account>…] [--refresh] [--json]
modelpick ls models [--json]
```

`ls` reads the named accounts, in the order given (default: every account,
sorted), through the floor, and prints one line per window:

```
ACCOUNT      STATE  WINDOW    USED  RESETS                     AGE
codex-main   ok     5h         22%  in 3h 29m      Sat 00:59Z  4m
                    7d         47%  in 3d 18h      Tue 15:29Z
go-1         ok     5h         97%  in 1h 11m      Fri 22:41Z  4m
                    7d         51%  in 4d 4h       Wed 01:29Z
                    30d        33%  in 16d 16h  Nov 16 13:29Z
```

`WINDOW` is the length, plus the model for a window scoped to one model
(`7d Fable`), or the vendor's label when the length is unknown. `AGE` is the
age of the reading. An account whose `STATE` is `error` has its message on a
line after the table, `<account>: <message>`. `--json` prints the
[`ls --json` shape](#ls---json). An unknown account name is an error before
any account is read. `ls` exits 0 whatever state the accounts are in, 1 for a
config error or a bad argument.

`ls models` lists the models each account's subscription offers, with their
thinking values: Claude from a list built into modelpick, Codex from
`<codex_home>/models_cache.json`, OpenCode Go by running
`opencode models opencode-go`. It makes no usage call. A listing that fails is
a row `error: <message>`; the exit code is still 0.

### `modelpick config`

```
modelpick config
```

Alias `conf`. The effective configuration as JSON: defaults applied, paths
absolute, caps as written. It makes no network call and does not read the
cache. See [`config` output](#config-output). Exit 0, or 1 when the file is
missing or invalid (the reason on stderr).

### `modelpick doctor`

```
modelpick doctor [all]
```

Validates the config and everything it depends on: the vendor tools on
`PATH`, each login, login expiry, thinking values against each account's
model list, cap arrays against the last reading, and choices that can never
be picked. It makes no usage call. Each problem is one line,
`ERROR  <subject>: <message>` or `WARN   <subject>: <message>`, followed by a
count, `N error(s), M warning(s)`, or `no problems found`. The subject is an
account, several accounts sharing one cause (`go-1, go-2: …`),
`profile <p>, choice <n>`, `schedule <name>` or `config`. Exit 1 if there is
any error, else 0; warnings do not change it.

`doctor all` also checks that each account's usage can be read: an account
whose saved reading is younger than the floor is used as is; any other is
called once, skipping the floor but not a `Retry-After`, after renewing an
expired login. The calls run at the same time. An account whose tool or login
`doctor` already reports as an error is not called.

The [operations guide](operations.md#troubleshooting) explains every message.

### `modelpick ui`

```
modelpick ui [--port N] [--no-open]
```

Serves a page on `127.0.0.1` for reading usage and editing the config, prints
`modelpick ui: http://127.0.0.1:<port>/?t=<token>`, and opens it in the
browser unless `--no-open`. It runs until Ctrl-C or SIGTERM and then exits 0.
`--port N` listens on port N; without it, the previous run's port when free,
else any. When another `modelpick ui` of the same user is running and
answers, it prints and opens that one's URL and exits 0. See
[the UI](operations.md#the-ui-modelpick-ui).

### `modelpick version`

```
modelpick version
```

Alias `--version`. Prints `modelpick <build>`: the release tag the binary was
built at (`v0.2.0`), else the commit's UTC date and first 6 hex digits
(`20261024-54fbf8`), with `-dirty` for uncommitted changes, or `dev` for a
build with no version control information. Exit 0.

### `modelpick help`

```
modelpick help [<command>]
```

Aliases `--help`, `-h`. With no command, the list of commands and a link to
these docs; with one, that command's usage. Exit 0, or 1 for an unknown
command.

## Exit codes

| Code | When |
|---|---|
| 0 | Success. `pick`/`env`: status `ok`. `doctor`: no error. |
| 1 | Failure. `pick`/`env`: status `failed`. `doctor`: at least one error. Any command: a bad argument, a missing or invalid config. |
| 2 | `pick`/`env`: status `ok_with_errors`. `pick` with no profile: some profile reported an error. |
| 130 | `pick`, `env`, `ls` or `doctor` stopped by Ctrl-C or SIGTERM. A renewal or usage call under way is cancelled. A second signal kills the process. |

## `pick` JSON

`pick <profile>` always prints one JSON object on stdout with these twelve
fields, in this order, one per line:

| Field | Type | Meaning |
|---|---|---|
| `status` | string | `ok`, `ok_with_errors` or `failed`; see [status values](#status-values). |
| `profile` | string | The profile asked for, `""` when none could be read from the arguments. |
| `vendor` | string or null | `claude`, `codex` or `opencode-go`; null when `failed`. |
| `account` | string or null | The account's name in the config. |
| `model` | string or null | The model, exactly as written in the config. |
| `thinking` | string or null | The thinking value as written; null for none, and when `failed`. |
| `account_ref` | object or null | The account's entry; see [`account_ref`](#account_ref). |
| `load_balance` | object or null | Set only when the pick came from a group; see [`load_balance`](#load_balance). |
| `reason` | string or null | Why nothing was picked; null unless `failed`. |
| `skipped` | array | Each choice passed over, in the order the walk met it; see [`skipped`](#skipped). |
| `errors` | array | What failed during this pick; see [`errors`](#errors). |
| `warnings` | array of strings | What did not change the decision; see [`warnings`](#warnings). |

`skipped`, `errors` and `warnings` are always arrays, never null. Objects and
array elements are written on one line each, and `>=` is not escaped.

### Status values

| `status` | Exit | Meaning | What a launcher does |
|---|---|---|---|
| `ok` | 0 | A choice was picked and nothing failed along the way. | Use `vendor`, `model`, `thinking`, `account_ref`. |
| `ok_with_errors` | 2 | A choice was picked, but something failed during this pick (a usage call or a credential read), so at least one choice was skipped for a reason other than its own usage or schedule. | Use the pick, and show `errors`: an account needs attention. |
| `failed` | 1 | Nothing was picked: every choice was skipped, the profile does not exist, the config is missing or invalid, or the arguments were wrong. | Do not launch. Show `reason`. |

`reason` for `failed` is one of: `no choice available`;
`unknown profile "<p>"; configured: <names>`; the config error, such as
`no config file at <path>. Start from the example: …`; or the argument error,
such as `pick: unknown flag "--x"`.

### `account_ref`

The picked account's config entry, with `~` expanded to an absolute path.
Accounts hold no secret, so it is safe to print:

| `vendor` | Fields |
|---|---|
| `claude` | `{"vendor": "claude", "config_dir": "/home/you/.claude"}` |
| `codex` | `{"vendor": "codex", "codex_home": "/home/you/.codex"}` |
| `opencode-go` | `{"vendor": "opencode-go", "key_file": "/home/you/.config/opencode/keys", "key_line": 2}` |

### `load_balance`

`null` unless the picked choice is a member of a load balancing group, then
`{"algorithm": "random", "choice": 1, "member": "1b"}`: the algorithm, the
group's position in the profile (a number), and the member's label.

### `skipped`

One `{"account": "<a>", "model": "<m>", "why": "<reason>"}` per choice passed
over. A group's members are listed in file order, whatever order the shuffle
tried them in. The reasons:

| `why` | Cause |
|---|---|
| `in schedule <name>` | A `skip_during` schedule matches now. |
| `outside schedule <name>` | The choice has `allow_during` and its schedule does not match now; `outside schedules <a>, <b>` when it lists several. |
| `credential missing (see errors)` | The account's login is missing or unreadable; `errors` says which. |
| `usage unknown (see errors)` | The choice is capped and the account has no usable reading; `errors` says why. |
| `usage unknown (no windows reported)` | The reading has no window that applies to this model. |
| `<window> window <used>% >= cap <cap>` | A window is at or over its fixed cap, for example `5h window 93% >= cap 90`. |
| `<window> window <used>% >= pace <allowed>% (<where>, +<N>)` | A window is at or over its pace cap, for example `7d window 58% >= pace 52.9% (day 3 of 7, +10)`. `<where>` is `day D of L` for a whole-day window, else `hour H of L`. |

`<window>` is the window's length (`5h`, `7d`, `30d`), or `?` when the vendor
did not say.

### `errors`

One `{"account": "<a>", "error": "<message>"}` per account and message, no
repeats: a credential that is missing or unreadable, a usage call that
failed, a login that could not be renewed, a cache that cannot be used. The
[troubleshooting table](operations.md#troubleshooting) lists the messages.
Any entry makes a pick that still found a choice `ok_with_errors`.

### `warnings`

Plain strings, each starting with the account name. They never change the
status:

- `<a>: using reading from <time> (<n> min old)`, with `; last call at <t>
  failed: <error>` or `; this call failed: <error>` when a call failed: the
  reading is older than the floor.
- `<a>: token expired; using reading from <time> (<n> min old)`
- `<a>: renewal failed: <reason>; using reading from <time> (<n> min old)`
- `<a>: rate-limited until <time>`
- `<a>: cache unavailable: <error>; using stored reading from <time> (<n> min old)`
- `<a>: account reports <n> windows (<lengths>), cap has <m> values; ignored the last <k>`
- `<a>: unrecognised window "<label>" capped at default_cap <cap>`
- `<a>: <window> window has no reset time; pace cap used default_cap <cap>`

A cap array shorter than the windows gives no warning: the missing positions
use `default_cap`.

## `env` output

`modelpick env <profile>` prints the pick as POSIX `sh` lines. Values are
single-quoted. On `ok` or `ok_with_errors`:

| Line | Value |
|---|---|
| `export MODELPICK_STATUS=` | `status` |
| `export MODELPICK_PROFILE=` | `profile` |
| `export MODELPICK_VENDOR=` | `vendor` |
| `export MODELPICK_ACCOUNT=` | `account` |
| `export MODELPICK_MODEL=` | `model` |
| `export MODELPICK_THINKING=` | `thinking`; `unset MODELPICK_THINKING` when null |
| `export CLAUDE_CONFIG_DIR=` | Claude: `account_ref.config_dir`; `unset CLAUDE_CONFIG_DIR` for the default `~/.claude` |
| `export CODEX_HOME=` | Codex: `account_ref.codex_home` |
| `export MODELPICK_KEY_FILE=`, `export MODELPICK_KEY_LINE=` | OpenCode Go: which key; for the other vendors both are unset |

For example:

```
export MODELPICK_STATUS='ok'
export MODELPICK_PROFILE='coder-difficult'
export MODELPICK_VENDOR='codex'
export MODELPICK_ACCOUNT='codex-main'
export MODELPICK_MODEL='gpt-6.1-sol'
export MODELPICK_THINKING='high'
export CODEX_HOME='/home/you/.codex'
unset MODELPICK_KEY_FILE
unset MODELPICK_KEY_LINE
```

Each entry of `errors` comes first as a line that prints
`modelpick: <account>: <error>` on stderr. On `failed` the output unsets
`MODELPICK_STATUS`, `MODELPICK_PROFILE`, `MODELPICK_VENDOR`,
`MODELPICK_ACCOUNT`, `MODELPICK_MODEL`, `MODELPICK_THINKING`,
`MODELPICK_KEY_FILE` and `MODELPICK_KEY_LINE`, prints
`modelpick: <profile>: <reason>` on stderr and ends with `false`, so
`eval "$(modelpick env p)" || exit 1` stops. `CLAUDE_CONFIG_DIR` and
`CODEX_HOME` are left alone then. The output never holds a credential.

## `ls --json`

An array, one object per account, in table order:

```json
[
  {
    "account": "go-1",
    "vendor": "opencode-go",
    "state": "ok",
    "reading_age_seconds": 248,
    "windows": [
      {
        "label": "rolling",
        "length": "5h",
        "used_pct": 97,
        "resets_at": "2026-10-30T22:41:14Z",
        "scope": null
      }
    ]
  }
]
```

| Field | Type | Meaning |
|---|---|---|
| `account`, `vendor` | string | As in the config. |
| `state` | string | `ok`; `expired token`; `rate-limited until <RFC 3339 time>`; `rate-limited (no retry time given)`; or `error: <message>`. |
| `reading_age_seconds` | number or null | Age of the reading shown; null with no reading. |
| `windows` | array | Empty with no reading. |
| `windows[].label` | string | The vendor's own name for the window. |
| `windows[].length` | string | `5h`, `7d`, `30d`, or `?` when unknown. |
| `windows[].used_pct` | number | Percent used; 0 once the reset time has passed. |
| `windows[].resets_at` | string or null | RFC 3339, UTC; null when unknown. |
| `windows[].scope` | string or null | The model a scoped window counts; null for a general window. |

`ls models --json` is an array of
`{"account": "<a>", "models": [{"id": "<model>", "thinking": ["low", …]}]}`,
with `"error": "<message>"` added when the listing failed. `thinking` is `[]`
for a model that takes none.

## `config` output

```json
{
  "settings": {"min_call_interval_minutes": 15, "default_cap": 95, "login_expiry_warning_days": 7},
  "accounts": {
    "claude-main": {"vendor": "claude", "config_dir": "/home/you/.claude"},
    "codex-main": {"vendor": "codex", "codex_home": "/home/you/.codex"}
  },
  "schedules": {"deepseek-peak": {"days": ["mon", "tue", "wed", "thu", "fri"], "utc": ["01:00-04:00"]}},
  "profiles": {
    "coder": [
      {"account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [98]},
      {"account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium", "uncapped": true}
    ]
  }
}
```

The real output is indented over many lines; the shape is:

- `settings`: all three keys, with defaults filled in.
- `accounts`: each account's fields with absolute paths.
- `schedules`: exactly as written in the file.
- `profiles`: each item is a choice or a group
  (`{"load_balance": "random", "choices": [...]}`). A choice has `account`,
  `model`, `thinking`, then `cap` as written, or `"uncapped": true` when it
  has none, and `skip_during` or `allow_during` when set.

There is no `version` key.

## Environment variables

modelpick reads:

| Variable | Use |
|---|---|
| `MODELPICK_CONFIG` | The config file. Else `$XDG_CONFIG_HOME/modelpick/config.json`, else `~/.config/modelpick/config.json` (also on macOS). |
| `XDG_CONFIG_HOME` | See above. |
| `XDG_CACHE_HOME` | The cache directory is `$XDG_CACHE_HOME/modelpick/`, else `~/.cache/modelpick/`. |
| `HOME` | Expands `~`. With no home directory, every command that reads the config refuses to run (`cannot determine the home directory: $HOME is not set`). |
| `HTTPS_PROXY`, `NO_PROXY` | Usage calls go through the proxy they name, as for any Go program. |
| `PATH` | Finds `claude`, `codex` and `opencode`. |

A renewal runs the vendor's tool with your environment minus the variables
that would make it log in some other way: `CLAUDE_CONFIG_DIR` (set to the
account's directory instead, unless it is the default), `ANTHROPIC_API_KEY`,
`ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_BASE_URL`,
`CLAUDE_CODE_USE_BEDROCK` and `CLAUDE_CODE_USE_VERTEX` for Claude;
`CODEX_HOME` (set to the account's), `OPENAI_API_KEY`, `CODEX_API_KEY` and
`OPENAI_BASE_URL` for Codex.

## Building a launcher

A launcher asks modelpick which account and model to use, points the vendor's
tool at that account, and starts it. modelpick never starts a tool itself.

1. Run `modelpick pick <profile>` and read stdout as JSON, or
   `eval "$(modelpick env <profile>)"`.
2. Exit code 1 (`failed`): do not launch; show `reason`.
3. Exit code 2 (`ok_with_errors`): launch, and show `errors`.
4. Point the tool at the account:
   - `claude`: set `CLAUDE_CONFIG_DIR` to `account_ref.config_dir` (or leave it
     unset for `~/.claude`);
   - `codex`: set `CODEX_HOME` to `account_ref.codex_home`;
   - `opencode-go`: the key is line `account_ref.key_line` of
     `account_ref.key_file`, counting only non-blank lines that do not start
     with `#`. How the key reaches OpenCode is up to the launcher;
     modelpick does not install it.
5. Pass `model` and `thinking` with the tool's own flags, and `exec` it.

With `jq`:

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

With `env`:

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

The empty check matters: a modelpick that is missing from `PATH` or crashed
prints nothing, `eval ""` succeeds, and the launcher would start with whatever
`MODELPICK_*` values an earlier `eval` left behind.

The flags each tool takes for model and thinking level are the tool's own;
check them against your installed versions.
