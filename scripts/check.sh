#!/usr/bin/env bash
# The single gate: CI runs this script, and it must exit 0 before every commit.
set -euo pipefail
cd "$(dirname "$0")/.."

unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt: these files need formatting:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go vet ./...
go test ./...

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS="${target%/*}" GOARCH="${target#*/}" go build ./...
  echo "build ok: $target"
done
