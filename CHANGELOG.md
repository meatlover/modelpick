# Changelog

## Unreleased

### New

- An expired Claude or Codex login is renewed. When a usage call is due,
  modelpick asks Claude Code (`claude -p`, a few tokens of that subscription)
  or Codex (`codex app-server`, no quota) to renew it, then makes the call. It
  still never writes a vendor file or handles a refresh token. A failed
  renewal counts as an attempt, retried at most once per
  `min_call_interval_minutes`, and shows the old reading with `renewal failed:
  <reason>`. If the refresh token itself has expired, log in again.
- The page's Refresh button works for an expired account that can be renewed.

### Changed

- `ls`, `pick`, `env` and `doctor` exit 130 on Ctrl-C, which cancels a renewal.
- The live smoke test renews only with `MODELPICK_LIVE_RENEW=1`.

## v0.2.0 — 2026-10-24

### New

- `modelpick ui`: a local web page for usage, profiles, schedules and accounts.
  Every edit is checked like `doctor` and applied at once. The first write
  reformats the config file and saves the old one to
  `~/.cache/modelpick/config.before-ui.json`.
- Pace caps: a cap position may be `"pace+N"` (N 5–50), which allows the share
  of the window already elapsed plus N, or `"default"`.
- Schedules: `allow_during` (usable only while a schedule matches), alongside
  `skip_during`, and a list form with one entry per set of days.
- `modelpick env <profile> [--refresh]`: the pick as `sh` export lines for
  `eval`. A failed pick unsets every `MODELPICK_*` variable and ends in `false`.
- `modelpick pick` with no profile: a table of every profile's pick.
- `modelpick ls [<account>…]`: list or refresh only the named accounts.
- `modelpick doctor all`: also makes at most one usage call per account that
  has no fresh reading. Plain `doctor` still makes none.

### Changed

- The `ls` table's columns are now `ACCOUNT STATE WINDOW USED RESETS AGE`.
  RESETS shows the time left and the reset time, error messages print below
  the table, and the VENDOR column is gone. `ls --json` is unchanged.
- `pick` with no profile prints the table instead of a failed result. Exit
  codes: 1 if any profile failed, else 2 if any had errors, else 0.
  `pick <profile>` output is unchanged.
- `doctor` prints one line per problem, naming every account it affects, and
  reports a stored failed usage reading.
- `models` is a reserved account name, because `ls models` lists models.
- A build at a release tag, including `go install …@v0.2.0`, now reports its
  real version in the User-Agent of usage calls.

## v0.1.0 — 2026-10-23

First release: Claude Code, Codex and OpenCode Go accounts; `pick`, `ls`,
`ls models`, `config` and `doctor`.
