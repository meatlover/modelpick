# Getting started

From nothing to a first pick: install modelpick, write a config, check it,
ask for a model, and open the UI.

## Requirements

- **Linux or macOS**, on amd64 or arm64. Windows is not supported.
- **Go 1.24 or newer**, only to build from source or with `go install`.
- **The vendor tools you use, logged in.** modelpick reads the login those
  tools already store; it never logs in for you.
  - Claude: `claude` (Claude Code), logged in once per config directory you
    configure.
  - Codex: `codex`, logged in with a ChatGPT account (`<codex_home>/auth.json`
    must exist).
  - OpenCode Go: `opencode`, plus a file holding your Go API keys, one per
    line.

  `doctor` reports a missing tool as an error. `pick` works without them
  until a Claude or Codex login expires: renewing it runs the tool. `ls
  models` needs `opencode` to list OpenCode Go models.

## Install

modelpick is one static binary with no third-party dependencies. Install it
one of four ways. Homebrew puts it on your `PATH`; for the others, put it there
yourself (`~/.local/bin` below is only an example).

### With Homebrew

```
brew install meatlover/tap/modelpick
```

This installs the latest release's archive for your platform, checked against
its `SHA256SUMS`, on macOS or Linux. The example config is then at
`$(brew --prefix)/share/modelpick/config.example.json`. The tap,
[meatlover/homebrew-tap](https://github.com/meatlover/homebrew-tap), picks up a
new release within about an hour; prereleases are not published there.

### A prebuilt release

Each release has an archive per platform, `modelpick_X.Y.Z_<os>_<arch>.tar.gz`
for `linux` and `darwin`, `amd64` and `arm64`, and a `SHA256SUMS` file. Pick
the latest release on the repository's Releases page; `vX.Y.Z` below stands for
its tag and `X.Y.Z` for the number. For a Mac with Apple silicon:

```
base=https://github.com/meatlover/modelpick/releases/download/vX.Y.Z
curl -fLO "$base/modelpick_X.Y.Z_darwin_arm64.tar.gz" -fLO "$base/SHA256SUMS"
shasum -a 256 -c SHA256SUMS --ignore-missing
tar -xzf modelpick_X.Y.Z_darwin_arm64.tar.gz
install -m 755 modelpick ~/.local/bin/modelpick
```

`shasum -a 256 -c` prints `OK` for the archive; on Linux, `sha256sum -c
SHA256SUMS --ignore-missing` does the same. The archive holds `modelpick`,
`config.example.json` (the example config), `README.md`, `CHANGELOG.md` and
`LICENSE`.

The binary is not signed or notarized. One fetched with `gh` or `curl` is not
quarantined by macOS; one downloaded in a browser is, and
`xattr -d com.apple.quarantine modelpick` clears it.

### With `go install`

```
go install github.com/meatlover/modelpick/cmd/modelpick@latest
```

This builds the latest release tag and installs it into
`$(go env GOPATH)/bin`. Name a tag (`@vX.Y.Z`) for a particular release.

### From a checkout

```
git clone https://github.com/meatlover/modelpick.git
cd modelpick
git checkout vX.Y.Z
go build -o ~/.local/bin/modelpick ./cmd/modelpick
```

Leave out the `git checkout` to build the latest `main`. To keep a checkout's
install current automatically, see [CONTRIBUTING.md](../CONTRIBUTING.md).

### Check the build

```
modelpick version
```

It prints `modelpick` and the build: the release tag (`v0.2.0`), or for a
build between releases the commit's UTC date and first 6 hex digits
(`20261024-54fbf8`), with `-dirty` for uncommitted changes. A build with no
git information prints `dev`.

## First config

modelpick reads one JSON file: `$MODELPICK_CONFIG`, else
`$XDG_CONFIG_HOME/modelpick/config.json`, else
`~/.config/modelpick/config.json` (also on macOS). Start from the example:

```
mkdir -p ~/.config/modelpick
cp config.example.json ~/.config/modelpick/config.json
```

`config.example.json` is in the release archive. In a checkout it is
`examples/config.json`; with `go install`, download it from
[`examples/config.json`](../examples/config.json) in the repository.

Then edit it:

1. Under `accounts`, keep one entry per login you have and rename it to your
   liking. Point each at where its login lives: `config_dir` for Claude (the
   `CLAUDE_CONFIG_DIR` you log in with, `~/.claude` by default), `codex_home`
   for Codex (`~/.codex` by default), `key_file` and `key_line` for an
   OpenCode Go key.
2. Under `profiles`, write one profile per kind of work, each a list of
   choices in order of preference. End each profile with a choice that has no
   `cap`, so it always has an answer.
3. Delete what you do not use, including the example `schedules`.

The [configuration guide](configuration.md) explains every field, and
[how modelpick decides](concepts.md) explains caps, schedules and groups.

## Check it

```
modelpick doctor
```

This makes no usage calls. It checks the file, that each tool is on `PATH`,
that each login can be read, and that each model and thinking value is one the
account offers. It prints `no problems found`, or one line per problem and a
count; it exits 0 when there are no errors (warnings are allowed) and 1
otherwise. Fix every `ERROR` line before going on; the
[troubleshooting table](operations.md#troubleshooting) explains each message.

With a missing config, it says where it looked:

```
ERROR  config: no config file at /home/you/.config/modelpick/config.json. Start from the example: https://github.com/meatlover/modelpick/blob/main/examples/config.json (also in each release archive as config.example.json), then run `modelpick doctor`.
1 error(s), 0 warning(s)
```

On macOS, the first read of a Claude login may ask whether `security` may use
Claude Code's Keychain item. Allow it; modelpick reads it into memory only. On
Linux, and on macOS when there is no Keychain item, the login is read from
`<config_dir>/.credentials.json`.

## First pick

```
modelpick ls
```

shows each account's windows and how much of each is used. It makes one usage
call per account (then at most one per account per 15 minutes) and creates
the cache under `~/.cache/modelpick/` (or `$XDG_CACHE_HOME/modelpick/`).

```
modelpick pick <profile>
```

prints which account, model and thinking level to use, as JSON; `modelpick
pick` with no profile prints a table of every profile. The
[reference](reference.md) describes the output and how to call modelpick from
a launcher script.

## Open the UI

```
modelpick ui
```

opens a local page that shows every account's usage and edits the config.
Every edit is written to the file at once, and only if it adds no `doctor`
error. The first edit rewrites the file in a canonical layout and keeps a copy
of the original in the cache directory. Stop it with Ctrl-C. See
[the UI](operations.md#the-ui-modelpick-ui).

## Upgrade

Install the new version the same way (`brew upgrade modelpick` with Homebrew). The config format has `"version": 1`;
a release that changes it will say so in the [changelog](../CHANGELOG.md).
The cache needs no migration: an unreadable state file is treated as empty and
rewritten on the next call. A running `modelpick ui` keeps the old binary
until you restart it.

## Uninstall

modelpick owns only three things. Remove them and nothing is left:

```
rm ~/.local/bin/modelpick            # or wherever you put the binary,
                                     # or: brew uninstall modelpick
rm -r ~/.config/modelpick            # the config
rm -r ~/.cache/modelpick             # cached usage readings and lock files
```

Use your `$MODELPICK_CONFIG`, `$XDG_CONFIG_HOME` and `$XDG_CACHE_HOME` paths
if you set them. modelpick never writes to `~/.claude*`, `~/.codex`,
OpenCode's data or your key file, so there is nothing to undo there.
