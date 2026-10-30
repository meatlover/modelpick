#!/usr/bin/env bash
# Reads NUL-separated paths (git diff --cached --name-only -z) on stdin.
# Exit 0: at least one path can change the browser tests' outcome, so they
# must run. Exit 1: none can (docs, *.md, workflows). The browser tests drive
# the real back end (internal/, cmd/) and the static UI, so those, the tests
# (e2e/), the scripts and hooks that run them, and the module files all count.
# Used by .githooks/pre-commit; tested by internal/archtest/hook_test.go.
set -euo pipefail
while IFS= read -r -d '' path; do
  case "$path" in
    internal/* | cmd/* | e2e/* | scripts/* | .githooks/* | go.mod | go.sum) exit 0 ;;
  esac
done
exit 1
