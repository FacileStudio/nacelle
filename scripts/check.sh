#!/usr/bin/env sh
#
# The repository quality gate. Reports, never rewrites (except --format).
#
#   sh scripts/check.sh          gofmt + vet + test
#   sh scripts/check.sh --format rewrite Go sources in place
#
# nacelle is a library: there is no client half, so this is the whole gate.
# It deliberately depends on nothing but a `go`, and is NOT invoked through
# mise: `mise run` resolves every tool in the merged config before running any
# task body, so one broken tool in a global config would take the gate down
# with it.

set -eu

mode="all"
case "${1:-}" in
--format) mode="format" ;;
"") ;;
*)
  echo "usage: $0 [--format]" >&2
  exit 2
  ;;
esac

root="$(git rev-parse --show-toplevel)"
cd "$root"

# Resolve the toolchain from GOROOT when it is set. mise exports GOROOT for the
# version this repo pins while leaving an unrelated `go` earlier on PATH, and a
# go binary driving a different GOROOT fails with
# `compile: version "X" does not match go tool version "Y"`.
if [ -n "${GOROOT:-}" ] && [ -x "$GOROOT/bin/go" ]; then GO="$GOROOT/bin/go"; else GO=go; fi
if [ -n "${GOROOT:-}" ] && [ -x "$GOROOT/bin/gofmt" ]; then GOFMT="$GOROOT/bin/gofmt"; else GOFMT=gofmt; fi

if ! command -v "$GO" >/dev/null 2>&1; then
  echo "check: no usable go ('$GO')" >&2
  exit 1
fi

if [ "$mode" = "format" ]; then
  "$GOFMT" -w .
  echo "==> formatted"
  exit 0
fi

echo "==> gofmt"
unformatted="$("$GOFMT" -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt: the following files are not formatted (run 'sh scripts/check.sh --format'):"
  echo "$unformatted"
  exit 1
fi

echo "==> go vet"
"$GO" vet ./...

echo "==> go test"
"$GO" test ./...

echo "==> ok"
