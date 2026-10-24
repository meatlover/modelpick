#!/usr/bin/env bash
# Builds the three commands into cmd/<name>/<name> and installs modelpick.
# Install only: it never runs a built binary and never starts, stops or
# signals a process (a running `modelpick ui` keeps its old binary).
#
#   MODELPICK_INSTALL_DIR  install directory (default: $HOME/.local/bin)
#
# The git post-merge and post-rewrite hooks call this on main when
# `git config modelpick.autodeploy true` is set; run it by hand with
#   bash scripts/deploy-local.sh
set -euo pipefail

if ! command -v go >/dev/null 2>&1; then
  echo "modelpick: deploy-local: go is not on PATH" >&2
  exit 1
fi
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# build_to <package dir name>: builds to a temp file next to the target, then
# renames it into place, so a failed build never leaves a half-written binary.
build_to() {
  local name=$1 tmp
  tmp=$(mktemp "cmd/$name/.$name.tmp.XXXXXX")
  if ! go build -o "$tmp" "./cmd/$name"; then
    rm -f "$tmp"
    return 1
  fi
  mv -f "$tmp" "cmd/$name/$name"
}

for name in modelpick modelpick-ui-demo modelpick-e2e-server; do
  build_to "$name"
done

dir=${MODELPICK_INSTALL_DIR:-$HOME/.local/bin}
mkdir -p "$dir"
tmp=$(mktemp "$dir/.modelpick.tmp.XXXXXX")
cp cmd/modelpick/modelpick "$tmp"
chmod 755 "$tmp"
mv -f "$tmp" "$dir/modelpick"

version=$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/cli/version.go)
echo "modelpick: deployed $version ($(git rev-parse --short HEAD)) to $dir/modelpick"
