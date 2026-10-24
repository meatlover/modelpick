# Changelog

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
