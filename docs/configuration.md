# Configuration guide

modelpick reads one JSON file. Its location is, in order:

1. `$MODELPICK_CONFIG`
2. `$XDG_CONFIG_HOME/modelpick/config.json`
3. `~/.config/modelpick/config.json` (also on macOS)

The file never holds a secret; it only says where each tool already keeps its
login. `modelpick doctor` validates the file and everything it depends on, and
`modelpick config` prints the effective configuration (defaults applied, caps
shown per window position).

The running example below is `examples/config.json`; a test keeps the two
identical. Model names and numbers are illustrative.

```json
{
  "version": 1,
  "settings": {
    "min_call_interval_minutes": 15,
    "default_cap": 95
  },
  "accounts": {
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" },
    "claude-rec":  { "vendor": "claude", "config_dir": "~/.claude-rec" },
    "codex-main":  { "vendor": "codex", "codex_home": "~/.codex" },
    "go-1": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 1 },
    "go-2": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 2 }
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
      { "account": "claude-rec",  "model": "claude-sonnet-5-5", "thinking": "medium" }
    ],
    "tester": [
      { "account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, 95, 98], "skip_during": ["deepseek-peak"] },
      { "account": "go-2", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, 95, 98], "skip_during": ["deepseek-peak"] },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ]
  }
}
```

Unknown keys anywhere are errors, as are unknown account or schedule
references, malformed times, caps outside 0-100, an unknown `vendor`, and a
`version` other than `1`. Errors name the failing field, for example
`profiles.tester[1].skip_during`.

## `version`

Must be `1`.

## `settings`

Both keys are optional; the defaults are built into the binary.

| Key | Default | Meaning |
|---|---|---|
| `min_call_interval_minutes` | `15` | The floor: at most one usage call per account in this many minutes. A younger reading is used as is. `pick --refresh` and `ls --refresh` skip the floor, but never an unexpired server `Retry-After`. Must not be negative. |
| `default_cap` | `95` | Cap (percent) used for window positions a choice's `cap` array does not cover. 0-100. |

## `accounts`

A map of your own account names to the place the login lives. Any number of
accounts per vendor. Each vendor takes exactly the fields below; a missing or
foreign field is an error. Paths may start with `~`.

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

A named set of times during which a choice must not be used, for example a
provider's peak-price hours.

- `days`: lowercase `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun`.
- `utc`: ranges `"HH:MM-HH:MM"`, all in **UTC**. The start is inclusive and
  the end exclusive: `01:00-04:00` covers 01:00:00 to 03:59:59.
- A range whose end is earlier than its start crosses midnight and belongs to
  the day it starts on: `22:00-02:00` on `fri` covers Friday 22:00 to
  Saturday 02:00.
- Both lists must be non-empty; an empty list is an error.

A choice lists schedules in `skip_during`. If **any** listed schedule matches
now, the choice is skipped, with no exception even when it is the last choice.

## `profiles`

A map of profile name (the kind of work: `coder-difficult`, `tester`, ...) to
a non-empty, ordered list of choices. `pick` returns the first usable one.

| Key | Required | Meaning |
|---|---|---|
| `account` | yes | A key of `accounts`. |
| `model` | yes | The vendor's model name, passed through unchanged. |
| `thinking` | yes | The vendor's own string, or `null` for none. Omitting the key is an error. |
| `cap` | no | See below. |
| `skip_during` | no | List of schedule names. |

### Thinking

modelpick passes the string through. `pick` does not check it (it is a static
check of the file only); `doctor` does, and reports an invalid value as an
error:

- Claude: `low`, `medium`, `high`, `xhigh` or `max`.
- Codex: the model's `supported_reasoning_levels` in
  `<codex_home>/models_cache.json`.
- OpenCode Go: `null`.

### Caps

`cap` is an array of percentages, one per usage window. A window **blocks** the
choice when `used% >= cap`.

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
  fallback choice. `doctor` warns when a profile has no uncapped choice.
- **`"cap": null`** is an error ("omit cap for no limit").
- **`"cap": []`** is capped, with every window at `default_cap`.

Every trim or fill `pick` applies is reported in its `warnings`, because caps
shift by position if a plan starts reporting a different set of windows. A
capped choice whose usage is unknown is skipped; unknown usage is never
treated as 0%. A window whose reset time has passed counts as 0% until the
next reading.

## What `pick` does with a choice

For each choice in order: skip if a `skip_during` schedule matches; skip if the
account's credential is missing (a local check; an *expired* Claude token does
not skip here); pick it if it has no `cap`; otherwise read the account's
usage, skip if unknown, skip if any window is at or over its cap, else pick it.
Accounts are only read when a choice reaches that step.

## `pick` status and exit code

The output is always JSON on stdout, with the same fields in the same order:
`status, profile, vendor, account, model, thinking, account_ref, reason,
skipped, errors, warnings`.

| `status` | Exit | Meaning |
|---|---|---|
| `ok` | 0 | A model was picked and nothing failed along the way. |
| `ok_with_errors` | 2 | A model was picked, but something failed during this pick (a usage call returned 401/5xx/timeout, a credential could not be read, a response could not be parsed), so a choice was skipped for a reason other than its own usage or schedule. See `errors`. |
| `failed` | 1 | No model: everything blocked with no usable uncapped choice, an unknown profile, a config error, or any other failure. See `reason`. |

`warnings` never change the status: trimmed or filled caps, unrecognised
windows, or a reading used past the floor.
