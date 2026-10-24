# Installation guide

modelpick is one static Go binary with no third-party dependencies. It runs on
Linux and macOS, on amd64 and arm64. Windows is not supported.

## Requirements

- **Go 1.24 or newer**, to build it. Prebuilt binaries are not published yet.
- **The vendor tools you configure, logged in and on `PATH`.** modelpick reads
  the login those tools already store; it never logs in for you.
  - Claude: `claude` (Claude Code), logged in once per config directory you
    configure.
  - Codex: `codex`, logged in with a ChatGPT account (`<codex_home>/auth.json`
    must exist).
  - OpenCode Go: `opencode`, plus a file holding your Go API keys, one per
    line.

  `modelpick doctor` reports a missing tool as an error. `pick` itself works
  without them; only `doctor` and `ls models` run them.

## Install

### From a checkout (works while the repository is private)

```
git clone git@github.com:meatlover/modelpick.git
cd modelpick
git checkout v0.2.0
go build -o ~/.local/bin/modelpick ./cmd/modelpick
```

Put the binary anywhere on your `PATH`; `~/.local/bin` is only an example.
A build at a release tag reports that release's version; a build of `main`
between releases reports the next version with `-dev`. The version appears only
in the `User-Agent` of usage calls.

To keep a checkout's install current, set `git config modelpick.autodeploy true`; see the README's Development section.

### With `go install`

```
go install github.com/meatlover/modelpick/cmd/modelpick@v0.2.0
```

This needs the `v0.2.0` tag on GitHub. It installs into
`$(go env GOPATH)/bin`. While the repository is private, Go
must be told not to use the public module proxy for it, and git must be able
to authenticate to GitHub:

```
export GOPRIVATE=github.com/meatlover/modelpick
```

## First setup

1. Create the config from the example and edit it:

   ```
   mkdir -p ~/.config/modelpick
   cp examples/config.json ~/.config/modelpick/config.json
   ```

   The file location is `$MODELPICK_CONFIG`, else
   `$XDG_CONFIG_HOME/modelpick/config.json`, else
   `~/.config/modelpick/config.json` (also on macOS). The
   [configuration guide](configuration.md) explains every field. Rename the
   accounts to your own, point each at the place its login lives, and write
   the profiles you need.

2. Check it:

   ```
   modelpick doctor
   ```

   This makes no usage calls. It exits 0 when there are no errors (warnings
   are allowed) and 1 otherwise. Fix every `ERROR` line before going on; the
   [admin guide](admin.md#troubleshooting) lists what each one means.

3. Look at live usage once:

   ```
   modelpick ls
   ```

   This makes one usage call per account (then at most one per 15 minutes) and
   creates the cache under `~/.cache/modelpick/` (or
   `$XDG_CACHE_HOME/modelpick/`).

4. Ask for a pick:

   ```
   modelpick pick <profile>
   ```

### macOS Keychain

For Claude accounts on macOS, modelpick reads Claude Code's Keychain item with
`/usr/bin/security find-generic-password`. If macOS asks whether `security` may
access the item, allow it; modelpick reads the password into memory only. A
lookup that waits longer than 20 seconds fails, and that account is reported
as having an unreadable credential.

On Linux, and on macOS when no Keychain item exists, the credential is read
from `<config_dir>/.credentials.json`.

## Upgrade

Rebuild or `go install` the new version. The config format has `"version": 1`;
a release that changes it will say so. The cache needs no migration: an
unreadable state file is treated as empty and rewritten on the next call.

## Uninstall

modelpick owns only three things. Remove them and nothing is left:

```
rm ~/.local/bin/modelpick            # or wherever you put the binary
rm -r ~/.config/modelpick            # the config
rm -r ~/.cache/modelpick             # cached usage readings and lock files
```

Use your `$MODELPICK_CONFIG` / `$XDG_CONFIG_HOME` / `$XDG_CACHE_HOME` paths
if you set them. modelpick never writes to `~/.claude*`, `~/.codex`, OpenCode's
data or your key file, so there is nothing to undo there.
