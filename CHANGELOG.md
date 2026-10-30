# Changelog

## Unreleased

### New

- `modelpick help`, `--help` and `-h` print the usage, with a line on each
  command, to stdout and exit 0; `modelpick <command> --help` and
  `modelpick help <command>` print that command's usage. Before, they exited 1
  with "unknown command" (`pick --help` printed a failed pick, `ui --help` a
  flag-package message).
- `modelpick ui` tells you when a newer `modelpick` is installed under it
  (for example by autodeploy): the page shows a grey note, "A newer modelpick
  is installed. Stop this one (Ctrl-C where you ran `modelpick ui`) and run
  `modelpick ui` again.", and `/api/state` carries `binary_replaced`.
- Pushing a `vX.Y.Z` tag publishes a GitHub Release: archives for linux and
  darwin on amd64 and arm64, `SHA256SUMS`, and that version's changelog
  section as the notes. The job runs `scripts/check.sh` first, refuses a tag
  whose `internal/cli/version.go` or changelog section disagrees, and runs the
  built binary to confirm `modelpick version` prints the tag. See
  `docs/release.md`.
- The project is MIT licensed (Meatlover LLC).
- `doctor` (and `doctor all` and the UI's doctor status) warns when a Claude
  login's refresh token expires within `login_expiry_warning_days` (default 7)
  and reports an error once it has expired, naming the UTC time. Renewal never
  extends that date, so only logging in again helps. `0` turns the check off;
  Codex and OpenCode Go logins have no known expiry and are never reported.
  These findings do not block config edits.

### Changed

- A config file that does not exist is reported with the path looked for and
  where to start: "no config file at <path>. Start from the example: … (also
  in each release archive as config.example.json), then run `modelpick
  doctor`." Other read errors keep their text.
- Release archives hold `config.example.json` (a copy of
  `examples/config.json`) next to `README.md`.
- The documentation is reorganised: `docs/install.md` is now
  `docs/getting-started.md` and `docs/admin.md` is `docs/operations.md`; new
  are `docs/concepts.md` (how a pick is decided, with a worked example),
  `docs/reference.md` (every command, output field, exit code and environment
  variable), `CONTRIBUTING.md` and `SECURITY.md`. Tests keep the docs in step
  with the code.

## v0.3.0 — 2026-10-30

### New

- `modelpick version` (alias `--version`) names the build: the release tag it
  was built at (`v0.2.0`), or else the commit's UTC date and first 6 hex
  digits (`20261024-54fbf8`), with `-dirty` for uncommitted changes.
- An expired Claude or Codex login is renewed. When a usage call is due,
  modelpick asks Claude Code (`claude -p`, a few tokens of that subscription)
  or Codex (`codex app-server`, no quota) to renew it, then makes the call. It
  still never writes a vendor file or handles a refresh token. A failed
  renewal counts as an attempt, retried at most once per
  `min_call_interval_minutes`, and shows the old reading with `renewal failed:
  <reason>`; anything token-like in the reason reads `[redacted]`. If the
  refresh token itself has expired, log in again.
- The page's Refresh button works for an expired account that can be renewed.
- Load balancing groups: a profile position can hold two or more choices
  (`"load_balance": "random"`), and `pick` takes a random usable member, moving
  on only when none is usable. `pick` JSON gains `load_balance`; `doctor`
  extends never-picked to groups and warns on identical members. The UI
  creates, edits and dissolves groups (⇉ Load balance…, shown while a
  profile has two or more choices outside groups).

### Changed

- Removing an account in the UI now also deletes its cached state
  (`<account>.json` and `<account>.lock` in the cache dir), so adding an
  account of the same name later starts with no inherited reading or floor.
  A hand-edited removal or rename leaves the files; they are safe to delete.
- The UI's doctor status lists each problem with a Go to that opens the tab
  holding its item, scrolls to it and lights it up for a moment. Check all,
  which read as "check every problem", is now **Diagnose** (`doctor all`),
  in the header left of the status instead of inside its panel.
- A choice card's schedules end under Remove's right edge.
- An account whose general (not model-scoped) window is at 100% reads
  **Limit reached** instead of ok; that window's bar and the nearest reset
  time are orange.
- "No caps" is now **Remove all caps**, a text link. Every Remove is red.
  A card's actions (Remove, Remove all caps, + schedule, + caps), an
  account's Remove and a Usage row's Refresh rest half transparent and grey,
  and show in full under the pointer or with focus inside the card or row.
- A card's blue frame shows on hover and keyboard focus, not after a click.
  A load balancing group's frame is grey.
- `ls`, `pick`, `env` and `doctor` exit 130 on Ctrl-C, which cancels a renewal.
- The UI no longer refuses an edit as "changed outside the UI" when it was
  made on a card before the page had redrawn after the card's previous edit
  (a field, cap or schedule edit made while another was in flight).
- A card redrawn while one of its edits is in flight shows that edit, instead
  of the value before it until the edit is answered.
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
