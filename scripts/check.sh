#!/usr/bin/env sh
#
# The repository quality gate. Reports, never rewrites (except --format).
#
#   sh scripts/check.sh          gofmt + vet + test
#   sh scripts/check.sh --format rewrite Go sources in place
#
# There is no client half to build, so this is the whole gate. It deliberately
# depends on nothing but a `go`, and is NOT invoked through
# mise: `mise run` resolves every tool in the merged config before running any
# task body, so one broken tool in a global config would take the gate down
# with it.

set -eu

# Nested modules, each with its own go.mod. Every `./...` below stops at their
# directory boundary, so they need naming explicitly or they get no gate at all
# and the run still ends in "ok". `gofmt -l .` is the exception: it is a plain
# file walk that knows nothing of modules, so it already covers them. Keep this
# in step with the go.mod files.
NESTED="tui"

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

# Failures accumulate rather than aborting. With more than one module, stopping
# at the first one hides half the report, and the second half is the half
# nobody remembers to re-run.
status=0

echo "==> gofmt"
unformatted="$("$GOFMT" -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt: the following files are not formatted (run 'sh scripts/check.sh --format'):"
  echo "$unformatted"
  status=1
fi

echo "==> go vet"
"$GO" vet ./... || status=1
for module in $NESTED; do
  ("$GO" -C "$module" vet ./...) || status=1
done

# -race, not as a nicety. Tool handlers run concurrently on the SDK runner's
# goroutines and park their results in a mutex-guarded sink, which is the exact
# shape the detector exists for. It also earns its runtime: a poll of
# cmd.ProcessState raced with cmd.Wait in tools/ for as long as nothing looked.
echo "==> go test"
"$GO" test -race ./... || status=1
for module in $NESTED; do
  ("$GO" -C "$module" test -race ./...) || status=1
done

if [ "$status" -ne 0 ]; then
  echo "check failed"
  exit "$status"
fi

echo "==> ok"
