# Releasing

A release is a `vX.Y.Z` tag. Pushing the tag runs
`.github/workflows/release.yml`, which checks, builds and publishes a GitHub
Release with four archives, `SHA256SUMS` and that version's changelog section
as notes. Nothing is uploaded by hand.

## Steps

Start from a clean, up-to-date `main` on which `bash scripts/check.sh` passes.

1. Set `Version` in `internal/cli/version.go` to `X.Y.Z` (drop the `-dev`).
2. In `CHANGELOG.md`, rename `## Unreleased` to `## vX.Y.Z — YYYY-MM-DD`.
3. Commit both as `release: vX.Y.Z`.
4. Tag that commit, annotated: `git tag -a vX.Y.Z -m vX.Y.Z`.
5. Open the next cycle in a second commit: `Version` becomes `X.Y+1.0-dev`
   and `CHANGELOG.md` gets a fresh `## Unreleased` above the release section.
6. Push `main` and the tag: `git push origin main vX.Y.Z`.

A tag with a hyphen (`v0.5.0-rc.1`) publishes a prerelease. Its `Version`
must read `0.5.0-rc.1` and its changelog section is `## v0.5.0-rc.1 — date`.

## What the job checks

The `check` job runs `bash scripts/check.sh` on the tagged commit. Only if it
passes does the `release` job run `bash scripts/release-build.sh <tag> dist`,
whose steps each fail naming themselves (`release-build: version gate: …`):

- **tag**: the tag is `vX.Y.Z`, optionally with a `-prerelease` suffix.
- **clean tree**: no uncommitted files, and `HEAD` is the commit the tag
  points at.
- **version gate**: `internal/cli/version.go` holds the tag without its `v`.
- **notes**: `CHANGELOG.md` has a non-blank section whose heading starts
  `## <tag> `.
- **build**: one `modelpick_X.Y.Z_<os>_<arch>.tar.gz` per line of
  `scripts/targets` (also the list `check.sh` cross-builds), each holding
  `modelpick`, `README.md`, `CHANGELOG.md`, `LICENSE` and `config.example.json`
  (a copy of `examples/config.json`). `CGO_ENABLED=0`,
  `-trimpath`, `-s -w`.
- **checksums**: `SHA256SUMS`.
- **self-check**: the runner's own archive is extracted and its
  `modelpick version` must print exactly `modelpick <tag>`. Go stamps that
  from the clean tagged checkout; the script never forces it with ldflags.

A failure at any step publishes nothing. You can run the script yourself on a
tagged, clean checkout: `bash scripts/release-build.sh vX.Y.Z /tmp/dist`.

## Downloading

[Getting started](getting-started.md#a-prebuilt-release) shows how to download
an archive, check it against `SHA256SUMS` and install it.

## Making the repository public

The repository is private until the owner makes it public. At the flip, in
one commit:

1. Delete the "Until the repository is public" note in
   [getting started](getting-started.md#install). It is the only place the
   docs mention `GOPRIVATE` or the private repository; a docs test fails if
   that text appears anywhere else.
2. Check that the README's install line, `go install
   github.com/meatlover/modelpick/cmd/modelpick@latest`, works from a machine
   with no GitHub login and no `GOPRIVATE`, and that `curl` can download a
   release asset.
3. Turn on private vulnerability reporting in the repository's security
   settings, which [SECURITY.md](../SECURITY.md) points to.
4. Settle the open items of the pre-public audit:
   - `docs/superpowers/` (design specs, plans and mockups) holds the
     author's home path, profile directory names and an internal tool's name.
     Keep it, scrub those strings, or remove it; removing it means moving the
     test in `internal/core/pick_test.go` that reads a spec, and the two CSS
     comments that cite the mockups.
   - The account name `claude-rec` in `examples/config.json` (and so in the
     configuration guide), the core tests and the UI demo, and the Keychain
     item name derived from the author's path in `internal/adapter/adapter.go`
     and `internal/web/demo/demo.go`: rename to neutral values or accept.
   - The key file name `~/.ssh/opencode-go-api` in examples and fixtures:
     rename or accept.
5. Add a macOS CI job, which costs nothing for a public repository, and
   remove the matching item from the
   [architecture guide's known limitations](architecture.md#known-limitations).

Later: a Homebrew tap, once there is a release to point it at.
