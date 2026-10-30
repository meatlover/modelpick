# How modelpick decides

This guide explains the ideas behind a pick: what an account, a window and a
reading are, how a profile is walked, and what the answer tells you. The
[configuration guide](configuration.md) has the exact syntax of every field;
the [reference](reference.md) has every output field.

## Accounts

An **account** is one login to one vendor: a Claude Code config directory, a
Codex home, or one line of an OpenCode Go key file. You name your accounts
(`claude-main`, `go-1`) and can have any number per vendor. The config file
only says where each login lives; modelpick reads the login itself from there,
in memory, when it needs to ask the vendor for usage.

## Windows and readings

Each subscription limits use over one or more **windows**: a 5-hour session
window, a 7-day weekly window, sometimes a 30-day one or a window that counts
only one model. A window has a length, a percent used, and a time it resets.

A **reading** is one answer from the vendor's usage endpoint: every window of
one account at one moment. modelpick stores the last good reading of each
account in its cache. A window whose reset time has passed counts as 0% until
the next reading.

## The call floor

modelpick asks a vendor for a reading at most once per account per
`min_call_interval_minutes` (15 by default), across every modelpick process on
the machine. A younger reading is used as is. When a call fails, the last good
reading is used and `warnings` says how old it is. `--refresh` skips the floor
for one command, but never a server's `Retry-After`. The
[operations guide](operations.md#the-call-floor) has the details.

## Profiles and positions

A **profile** names a kind of work (`coder-difficult`, `tester`) and lists
**choices** in order of preference. A choice is an account, a model and a
thinking level, with optional caps and schedules. `pick <profile>` returns the
first choice that is usable right now.

Each list item is a **position**, numbered from 1. A position is either one
choice or a load balancing group (below), whose members are `1a`, `1b`, and so
on. `doctor`, the UI and the `load_balance` field of a pick name choices this
way.

## Caps

A **cap** says how much of a window a choice may use before it is skipped.
`cap` is an array with one element per window position:

- a number, a **fixed cap**: the choice is blocked when that window's used
  percent is at or over it;
- `"pace+N"`, a **pace cap**: the cap follows how far the window is through
  its length, plus N points. Halfway through a 7-day window, `"pace+10"`
  allows 60%. It spreads a weekly allowance over the week instead of spending
  it on the first day;
- `"default"`: `default_cap` (95 unless you change it).

Window positions are counted in a fixed order: general windows shortest
first, then windows scoped to the choice's model. Missing positions use
`default_cap`. A choice with no `cap` key has no limit: its usage is never
read, so it is usable even when usage is unknown, which makes it a good last
choice. A capped choice whose usage is unknown is skipped; unknown is never
read as 0%.

## Schedules

A **schedule** is a named set of days and UTC hours, for example a provider's
peak-price hours. A choice can refer to schedules in one of two ways:

- `skip_during`: the choice is skipped while any of them matches;
- `allow_during`: the choice is usable only while one of them matches.

A choice has one or the other, never both. Schedules are checked before
anything else, so a choice skipped by a schedule never causes a usage call.

## Load balancing groups

A **group** is several choices that share one position. When `pick` reaches a
group, it checks the schedules and credentials of every member, shuffles the
members that pass, and tries them in that order with their caps. The first
usable member is picked. Consecutive picks therefore spread over, say, three
OpenCode Go keys instead of draining the first one. If no member is usable,
the walk goes on to the next position.

Only the members the shuffle reaches have their usage read. `skipped` lists a
group's members in file order whatever the shuffle did, and because the
credential check runs on every member first, a broken login gives
`ok_with_errors` on every run, not only when the shuffle happens to try it.

## How `pick` walks a profile

For each position in order, `pick` judges a choice in these steps and stops
at the first that fails:

1. A `skip_during` schedule matches now: skipped, `in schedule <name>`.
2. It has `allow_during` and none of those schedules matches: skipped,
   `outside schedule <name>` (`outside schedules <a>, <b>` for several).
3. The account's login is missing or unreadable (a local check, no network):
   skipped, `credential missing (see errors)`. An *expired* login passes this
   step; the usage call renews it.
4. No `cap`: picked.
5. The account's reading is unknown: skipped, `usage unknown (see errors)`.
   A reading with no window that applies to this model is skipped as
   `usage unknown (no windows reported)`.
6. Any window is at or over its cap: skipped, with the window and the cap, for
   example `5h window 93% >= cap 90` or
   `7d window 58% >= pace 52.9% (day 3 of 7, +10)`.
7. Otherwise: picked.

An account's usage is read only when a capped choice reaches step 5, and at
most once per pick, so a profile that is answered by its first choice reads
at most one account.

The answer is one JSON object:

- `status` is `ok` when a choice was picked and nothing failed on the way,
  `ok_with_errors` when a choice was picked but something failed (a usage call
  or a credential read, so a choice was skipped for a reason other than its
  own usage or schedule), and `failed` when nothing could be picked. The exit
  code follows: 0, 2, 1.
- `skipped` lists every choice passed over before the pick, with `why`.
- `errors` lists what failed in this run, one entry per account and message.
- `warnings` lists what did not change the decision but is worth knowing:
  a reading older than the floor, a cap array longer than the windows, a pace
  cap that fell back to `default_cap`.

The [reference](reference.md#pick-json) lists every field.

## Expired logins

Claude and Codex logins hold an access token that expires after a while.
When a usage call is due and the token has expired, modelpick asks the
vendor's own tool to renew it (`claude -p` with a tiny prompt, or
`codex app-server`), then makes the call. modelpick never writes a vendor
file or touches the refresh token; the tool renews its own login. A renewal
happens only when the last good reading is older than the floor, and a failed
one is tried again at most once per floor.

The refresh token behind a Claude login also expires, at a date fixed when you
logged in, and renewal never extends it. `doctor` warns
`login_expiry_warning_days` (7 by default) before that date and reports an
error once it has passed; from then on only logging in again helps.

## A worked example

This config has one profile, `coder`, with four choices:

```json
{
  "version": 1,
  "accounts": {
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" },
    "codex-main":  { "vendor": "codex", "codex_home": "~/.codex" },
    "go-1": { "vendor": "opencode-go", "key_file": "~/.config/opencode/keys", "key_line": 1 }
  },
  "schedules": {
    "deepseek-peak": { "days": ["mon", "tue", "wed", "thu", "fri"], "utc": ["01:00-04:00", "06:00-10:00"] }
  },
  "profiles": {
    "coder": [
      { "account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, 95, 98], "skip_during": ["deepseek-peak"] },
      { "account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, "pace+10"] },
      { "account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [98] },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ]
  }
}
```

It is Thursday 2026-10-22, 07:30 UTC (`2026-10-22T07:30:00Z`). Every login is
present, and the cache holds these readings, each younger than the floor:

| Account | Window | Used | Resets (UTC) |
|---|---|---|---|
| go-1 | 5h | 12% | 2026-10-22T10:00:00Z |
| claude-main | 5h | 41% | 2026-10-22T09:00:00Z |
| claude-main | 7d | 58% | 2026-10-26T07:30:00Z |
| codex-main | 5h | 22% | 2026-10-22T11:00:00Z |
| codex-main | 7d | 64% | 2026-10-27T00:00:00Z |

`modelpick pick coder` walks the profile:

1. **go-1, deepseek.** Thursday 07:30 is inside `deepseek-peak`
   (06:00-10:00 on weekdays), so step 1 skips it: `in schedule
   deepseek-peak`. go-1's reading is never looked at, though its 12% would
   have passed.
2. **claude-main, opus.** The login is present and the choice is capped, so
   modelpick reads claude-main. Its windows in position order are 5h, then
   7d. The 5h window is at 41%, under its cap of 90. The 7d window has a pace
   cap: it started 2026-10-19 07:30, three days ago, so it is 3/7 = 42.9%
   through and `"pace+10"` allows 52.9%. It is at 58%: skipped,
   `7d window 58% >= pace 52.9% (day 3 of 7, +10)`.
3. **codex-main, gpt-6.1-sol.** Capped, so modelpick reads codex-main. The 5h
   window is at 22% against the cap of 98. The cap array has no second value,
   so the 7d window uses `default_cap`, 95; it is at 64%. Picked.
4. **claude-main, sonnet** is never reached. It has no cap, so it would have
   been picked whatever the readings said.

Nothing failed, so the status is `ok` and the exit code 0:

```json
{
  "status": "ok",
  "profile": "coder",
  "vendor": "codex",
  "account": "codex-main",
  "model": "gpt-6.1-sol",
  "thinking": "high",
  "account_ref": {"vendor": "codex", "codex_home": "/home/you/.codex"},
  "load_balance": null,
  "reason": null,
  "skipped": [
    {"account": "go-1", "model": "opencode-go/deepseek-v4.1", "why": "in schedule deepseek-peak"},
    {"account": "claude-main", "model": "claude-opus-5-5", "why": "7d window 58% >= pace 52.9% (day 3 of 7, +10)"}
  ],
  "errors": [],
  "warnings": []
}
```

`account_ref` is the account's entry with `~` expanded (here for a home
directory of `/home/you`), so a launcher can set `CODEX_HOME` from it.
A test runs this config through the real pick at that time with those
readings and compares the result with the block above.
