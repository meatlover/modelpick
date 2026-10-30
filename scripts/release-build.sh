#!/usr/bin/env bash
# Builds a release: everything the release job does except the upload, so it
# can be run and tested locally.
#
#   bash scripts/release-build.sh <tag> <outdir>
#
# Steps, in order; one that fails prints `release-build: <step>: <reason>` to
# stderr and exits non-zero, and nothing is written to <outdir>:
#   tag          <tag> is vX.Y.Z or vX.Y.Z-prerelease
#   clean tree   no uncommitted or untracked files, and HEAD is the tag's commit
#   version gate internal/cli/version.go holds the tag without its `v`
#   notes        the CHANGELOG.md section `## <tag> …` is not blank
#   build        one modelpick_<X.Y.Z>_<os>_<arch>.tar.gz per line of
#                scripts/targets (CGO_ENABLED=0, -trimpath, -s -w)
#   checksums    SHA256SUMS in sha256sum format
#   self-check   the host's archive, extracted, prints `modelpick <tag>` for
#                `modelpick version`; Go stamps that from the clean tagged
#                checkout, so a mismatch is never patched over with ldflags
# Then <outdir> gets the archives, SHA256SUMS and notes.md. Everything is built
# in a scratch directory first: an <outdir> inside the repo (dist/) would
# otherwise make the tree dirty for the go build that stamps the version.
#
#   RELEASE_TARGETS  space-separated os/arch list replacing scripts/targets;
#                    for tests only (the release job never sets it)
set -euo pipefail

fail() {
  echo "release-build: $1: $2" >&2
  exit 1
}

[ $# -eq 2 ] || { echo "usage: release-build.sh <tag> <outdir>" >&2; exit 2; }
tag=$1
out=$2
case $out in /*) ;; *) out=$PWD/$out ;; esac
cd "$(dirname "$0")/.."

if ! [[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  fail tag "$tag is not vX.Y.Z or vX.Y.Z-prerelease"
fi
version=${tag#v}

if [ -n "$(git status --porcelain)" ]; then
  fail "clean tree" "uncommitted or untracked files"
fi
tagged=$(git rev-parse -q --verify "refs/tags/$tag^{commit}" 2>/dev/null) || fail "clean tree" "no tag $tag in this checkout"
if [ "$(git rev-parse HEAD)" != "$tagged" ]; then
  fail "clean tree" "HEAD is not the commit $tag points at"
fi

have=$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/cli/version.go)
if [ "$have" != "$version" ]; then
  fail "version gate" "internal/cli/version.go holds \"$have\", the tag needs \"$version\""
fi

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

awk -v h="## $tag " 'index($0, h) == 1 { on = 1; next } /^## / { on = 0 } on' CHANGELOG.md >"$stage/notes.md"
if ! grep -q '[^[:space:]]' "$stage/notes.md"; then
  fail notes "CHANGELOG.md has no non-blank section starting \"## $tag \""
fi

if [ -n "${RELEASE_TARGETS:-}" ]; then
  targets=$RELEASE_TARGETS
else
  targets=$(cat scripts/targets)
fi

pkg=$stage/pkg
archives=""
for target in $targets; do
  os=${target%/*}
  arch=${target#*/}
  rm -rf "$pkg"
  mkdir "$pkg"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$pkg/modelpick" ./cmd/modelpick \
    || fail build "go build failed for $target"
  cp README.md CHANGELOG.md LICENSE "$pkg/"
  cp examples/config.json "$pkg/config.example.json"
  name=modelpick_${version}_${os}_${arch}.tar.gz
  COPYFILE_DISABLE=1 tar -czf "$stage/$name" -C "$pkg" modelpick README.md CHANGELOG.md LICENSE config.example.json \
    || fail build "tar failed for $target"
  archives="$archives $name"
done
rm -rf "$pkg"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}
(cd "$stage" && sha256 $archives >SHA256SUMS) || fail checksums "could not write SHA256SUMS"

host=$(go env GOOS)_$(go env GOARCH)
hostarchive=modelpick_${version}_${host}.tar.gz
case " $archives " in
  *" $hostarchive "*) ;;
  *) fail self-check "the host target ${host/_//} is not in the target list" ;;
esac
mkdir "$stage/check"
tar -xzf "$stage/$hostarchive" -C "$stage/check"
got=$("$stage/check/modelpick" version) || fail self-check "$hostarchive: modelpick version failed"
if [ "$got" != "modelpick $tag" ]; then
  fail self-check "$hostarchive prints \"$got\", want \"modelpick $tag\""
fi

mkdir -p "$out"
for f in $archives SHA256SUMS notes.md; do
  cp "$stage/$f" "$out/$f"
done
echo "release-build: $tag: $(echo $archives | wc -w | tr -d ' ') archive(s) in $out"
