# Configuration guide

modelpick reads one JSON file. Its location is, in order:

1. `$MODELPICK_CONFIG`
2. `$XDG_CONFIG_HOME/modelpick/config.json`
3. `~/.config/modelpick/config.json` (also on macOS)

The file never holds a secret; it only says where each tool already keeps its
login. `modelpick doctor` validates the file and everything it depends on, and
`modelpick config` prints the effective configuration (defaults applied, paths
absolute, caps as written). You can edit the file by hand or with
`modelpick ui`, which writes it in a canonical layout (see
[the UI](operations.md#the-ui-modelpick-ui)). This guide is a field reference;
[how modelpick decides](concepts.md) explains what the fields do together.

The running example below is `examples/config.json`; a test keeps the two
identical. Model names and numbers are illustrative.

```json
{
  "version": 1,
  "settings": {
    "min_call_interval_minutes": 15,
    "default_cap": 95,
    "login_expiry_warning_days": 7
  },
  "accounts": {
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" },
    "claude-secondary":  { "vendor": "claude", "config_dir": "~/.claude-secondary" },
    "codex-main":  { "vendor": "codex", "codex_home": "~/.codex" },
    "go-1": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 1 },
    "go-2": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 2 },
    "go-3": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 3 }
  },
  "schedules": {
    "deepseek-peak": {
      "days": ["mon", "tue", "wed", "thu", "fri"],
      "utc": ["01:00-04:00", "06:00-10:00"]
    }
  },
  "profiles": {
    "coder-difficult": [
      { "account": "claude-main", "model": "claude-opus-5-5",  "thinking": "high", "cap": [90, 98] },
      { "account": "claude-main", "model": "claude-fable-5-1", "thinking": "high", "cap": [90, 98, 95] },
      { "account": "codex-main",  "model": "gpt-6.1-sol",      "thinking": "high", "cap": [98] },
      { "account": "claude-secondary",  "model": "claude-sonnet-5-5", "thinking": "medium" }
    ],
    "tester": [
      { "account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, 95, 98], "skip_during": ["deepseek-peak"] },
      { "account": "go-2", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, 95, 98], "skip_during": ["deepseek-peak"] },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ],
    "reviewer": [
      {
        "load_balance": "random",
        "choices": [
          { "account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null, "cap": [95, 95, 98] },
          { "account": "go-2", "model": "opencode-go/deepseek-v4.1", "thinking": null, "cap": [95, 95, 98] },
          { "account": "go-3", "model": "opencode-go/deepseek-v4.1", "thinking": null, "cap": [95, 95, 98] }
        ]
      },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ]
  }
}
```

Unknown keys anywhere are errors, as are unknown account or schedule
references, malformed times, a cap element that is not one of the
[three forms](#caps), an unknown `vendor`, an account named `models`, and a
`version` other than `1`. Errors name the failing field, for example
`profiles.tester[1].skip_during` or
`profiles.tester[0].cap[0]: "pace+4": N must be 5–50`.

## `version`

Must be `1`.

## `settings`

Every key is optional; the defaults are built into the binary.

| Key | Default | Meaning |
|---|---|---|
| `min_call_interval_minutes` | `15` | The floor: at most one usage call per account in this many minutes. A younger reading is used as is. `pick --refresh` and `ls --refresh` skip the floor, but never an unexpired server `Retry-After`. Must not be negative. |
| `default_cap` | `95` | Cap (percent) used for window positions a choice's `cap` array does not cover. 0-100. |
| `login_expiry_warning_days` | `7` | `doctor` warns when a login's refresh token expires within this many days, and reports an error once it has expired (renewal then fails until you log in again). 0 turns the check off; Codex and OpenCode Go logins have no known expiry and are never reported. Must not be negative. |

## `accounts`

A map of your own account names to the place the login lives. Any number of
accounts per vendor. Each vendor takes exactly the fields below; a missing or
foreign field is an error. `config_dir`, `codex_home` and `key_file` must be
absolute or start with `~` (`~` or `~/…`, your home directory); a relative
path is an error (`must be an absolute path or start with ~`), because it
would depend on the directory modelpick happens to run in.

`models` is a reserved account name (`accounts.models: models is a reserved account name`):
`modelpick ls models` already means "list every account's models".

| `vendor` | Fields | Credential read |
|---|---|---|
| `claude` | `config_dir` | macOS: the Keychain item Claude Code made for that directory (service `Claude Code-credentials` for `~/.claude`, `Claude Code-credentials-<8 hex>` otherwise); Linux, or no Keychain item: `<config_dir>/.credentials.json`. |
| `codex` | `codex_home` | `<codex_home>/auth.json`. |
| `opencode-go` | `key_file`, `key_line` | `key_line` is the 1-based index among the key lines of `key_file`, ignoring blank lines and `#` comments. |

`pick` copies the chosen account's object into `account_ref` with paths made
absolute, so a launcher can use it (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, which
key) without parsing this file. That is safe because accounts cannot hold a
secret.

## `schedules`

A named set of times, for example a provider's peak-price hours. A choice
refers to a schedule to be skipped during it (`skip_during`) or to be used
only during it (`allow_during`); the schedule itself does not say which, so
one schedule can serve both.

A schedule is either one object `{ "days": [...], "utc": [...] }` or a list
of such objects. A list matches when any of its objects matches, which lets
each day have its own hours (the UI writes this form when the days differ).

- `days`: lowercase `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun`.
- `utc`: ranges `"HH:MM-HH:MM"`, all in **UTC**. The start is inclusive and
  the end exclusive: `01:00-04:00` covers 01:00:00 to 03:59:59.
- A range is longer than 0 minutes and shorter than 24 hours. `24:00` is
  allowed only as a range end (`22:00-24:00`), and `"00:00-24:00"` is the
  one whole-day range.
- A range whose end is earlier than its start crosses midnight and belongs to
  the day it starts on: `22:00-02:00` on `fri` covers Friday 22:00 to
  Saturday 02:00.
- One empty list is an error. Both empty, `{"days": [], "utc": []}`, is the
  empty schedule: it matches nothing, and `doctor` warns `schedule <name>:
  empty`. The UI creates a new schedule this way.

```json
{
  "version": 1,
  "accounts": {
    "go-1": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 1 },
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" }
  },
  "schedules": {
    "deepseek-peak": { "days": ["mon", "tue", "wed", "thu", "fri"],
                       "utc": ["01:00-04:00", "06:00-10:00"] },
    "weekend-mornings": [ { "days": ["sat"], "utc": ["08:00-12:00"] },
                          { "days": ["sun"], "utc": ["00:00-24:00"] } ],
    "later": { "days": [], "utc": [] }
  },
  "profiles": {
    "tester": [
      { "account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, "default", "pace+10"], "allow_during": ["weekend-mornings"] },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium",
        "skip_during": ["deepseek-peak"] }
    ]
  }
}
```

## `profiles`

A map of profile name (the kind of work: `coder-difficult`, `tester`, ...) to
a non-empty, ordered list of choices and [load balancing groups](#load-balancing-groups).
`pick` returns the first usable one.

| Key | Required | Meaning |
|---|---|---|
| `account` | yes | A key of `accounts`. |
| `model` | yes | The vendor's model name, passed through unchanged. |
| `thinking` | yes | The vendor's own string, or `null` for none. Omitting the key is an error. |
| `cap` | no | See below. |
| `skip_during` | no | List of schedule names. Skipped while **any** of them matches, with no exception even when it is the last choice. |
| `allow_during` | no | List of schedule names. Usable only while **any** of them matches. Not empty. |

A choice has `skip_during` or `allow_during`, never both
(`use skip_during or allow_during, not both`).

### Load balancing groups

A list item with `load_balance` instead of `account` is a group: several
choices that share one position, so consecutive picks spread across them (say
three OpenCode Go keys) instead of draining the first. The `reviewer` profile
in the example is one.

| Key | Required | Meaning |
|---|---|---|
| `load_balance` | yes | The algorithm. Only `"random"` exists. It is required, not defaulted, so a later algorithm cannot change what an existing file means. |
| `choices` | yes | At least 2 members, each a choice as in the table above, with its own `cap` and schedules. A member cannot be a group. |

A group has exactly these two keys; caps and schedules belong to the members.
Both `account` and `load_balance` on one item, or neither, is an error, as is
an unknown algorithm (the message lists the known ones). Errors locate the
member, for example `profiles.reviewer[0].choices[1].cap`.

Numbers count positions: in `reviewer` the group is 1 and the Claude choice
is 2. Members are `1a`, `1b`, `1c`; that is how `doctor` and the UI name them.
A pick of a member says so in its output:
`"load_balance": {"algorithm": "random", "choice": 1, "member": "1b"}`.

### Thinking

modelpick passes the string through. `pick` does not check it (it is a static
check of the file only); `doctor` does, and reports an invalid value as an
error:

- Claude: `low`, `medium`, `high`, `xhigh` or `max`.
- Codex: the model's `supported_reasoning_levels` in
  `<codex_home>/models_cache.json`.
- OpenCode Go: `null`.

### Caps

`cap` is an array with one element per usage window. Each element is one of:

- a number 0–100, a fixed cap in percent;
- `"pace+N"`, N a whole number from 5 to 50: a cap that follows the window's
  elapsed time (below);
- `"default"`: this position uses `default_cap`. It is how a position before
  a configured one stays at the default, as in `[95, "default", "pace+10"]`.

A window **blocks** the choice when `used% >= cap`. Anything else is a config
error naming the element, such as `"pace+2": N must be 5–50` or
`must be a number, "pace+N" or "default"`. Trailing `"default"` elements mean
what a missing position means, and the UI does not write them.

**Pace caps.** For a window with a known length and reset time, elapsed% is
how far the window is through its length, and the window may use
`min(100, elapsed% + N)`. `"pace+10"` on a 7-day window two days in allows
about 39%. The skip reason says so: `7d window 58% >= pace 49% (day 2.7 of
7, +10)`. A window with no known length or reset time uses `default_cap`
instead, with a warning. N starts at 5 because at the instant a window
resets `pace+0` would allow 0% and block an unused window.

Order of the windows, which fixes what each position means:

1. General (unscoped) windows, shortest to longest.
2. Then windows scoped to this choice's model, shortest to longest.
3. Windows of unknown length come last, and always use `default_cap`
   whatever the array says, so they never shift a configured position.

Windows scoped to a *different* model are ignored for the choice.

Examples: Claude `[5h, 7d]`, and for a Fable choice `[5h, 7d, Fable-weekly]`;
Codex whatever the plan reports (often just `[7d]`); OpenCode Go
`[5h, 7d, 30d]`.

Array length against the number of windows:

- **More values than windows:** the extra values are ignored.
- **Fewer values than windows:** the missing positions take `default_cap`.
- **Omitted `cap`:** the choice has no limit. Usage is not consulted for it,
  so it is usable even when usage is unknown. This is how a profile gets a
  fallback choice. A profile without one is allowed; `doctor` instead warns
  when an earlier choice makes a later one impossible to pick
  (`never picked, choice 1 always wins first`).
- **`"cap": null`** is an error ("omit cap for no limit").
- **`"cap": []`** is capped, with every window at `default_cap`.

Extra values that `pick` ignores, and windows of unknown length, are reported
in its `warnings` (and by `doctor`), because caps shift by position if a plan
starts reporting a different set of windows. Missing positions filled with
`default_cap` are not reported. A capped choice whose usage is unknown is
skipped; unknown usage is never treated as 0%. A window whose reset time has
passed counts as 0% until the next reading.

## What `pick` does with them

[How modelpick decides](concepts.md#how-pick-walks-a-profile) walks through a
pick step by step, with a worked example. The output, its `status` values and
exit codes are in the [reference](reference.md#pick-json).
