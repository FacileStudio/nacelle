#!/usr/bin/env sh
#
# The repository quality gate. Reports, never rewrites (except --format).
#
#   sh scripts/check.sh           gofmt + vet + test + lint
#   sh scripts/check.sh --no-lint skip the lint pass
#   sh scripts/check.sh --format  rewrite Go sources in place
#
# One module since the terminal client moved to its own repository
# (FacileStudio/nacelle-tui), so there is nothing to walk and no workspace to
# reconcile. The lint pass is skipped rather than fatal when golangci-lint is
# missing, because CI runs it either way and a contributor without the tool
# should still be able to check their work.

set -eu

mode="all"
case "${1:-}" in
--no-lint) mode="nolint" ;;
--format) mode="format" ;;
"") ;;
*)
  echo "usage: $0 [--no-lint|--format]" >&2
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

status=0

echo "==> gofmt"
unformatted="$("$GOFMT" -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt: the following files are not formatted (run 'sh scripts/check.sh --format'):"
  echo "$unformatted"
  status=1
fi

echo "==> go build"
"$GO" build ./... || status=1

echo "==> go vet"
"$GO" vet ./... || status=1

# -race, not as a nicety. Tool handlers run concurrently on the SDK runner's
# goroutines and park their results in a mutex-guarded sink, which is the exact
# shape the detector exists for. It also earns its runtime: a poll of
# cmd.ProcessState raced with cmd.Wait in tools/ for as long as nothing looked.
echo "==> go test"
"$GO" test -race ./... || status=1

# The linter is guarded on the binary running rather than on it being on PATH.
# A mise shim for a version that was never installed resolves under
# `command -v` and then fails on every invocation, which reads as a repository
# that does not lint rather than as a machine that cannot.
if [ "$mode" != "nolint" ]; then
  echo "==> golangci-lint"
  if golangci-lint version >/dev/null 2>&1; then
    golangci-lint run ./... || status=1
  else
    echo "check: no usable 'golangci-lint', skipping the lint pass (CI still runs it)" >&2
  fi
fi

if [ "$status" -ne 0 ]; then
  echo "check failed"
  exit "$status"
fi

echo "==> ok"
