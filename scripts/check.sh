#!/usr/bin/env sh
#
# The repository quality gate. Reports, never rewrites (except --format).
#
#   sh scripts/check.sh           gofmt + vet + test + lint
#   sh scripts/check.sh --no-lint skip the lint pass
#   sh scripts/check.sh --format  rewrite Go sources in place
#
# There is no client half to build, so this is the whole gate. Everything but
# the lint pass depends on nothing more than a `go`, and the lint pass is
# skipped rather than fatal when golangci-lint is missing, because CI runs it
# either way and a contributor without the tool should still be able to check
# their work. It is NOT invoked through mise: `mise run` resolves every tool in
# the merged config before running any task body, so one broken tool in a
# global config would take the gate down with it.

set -eu

# Nested modules, each with its own go.mod. Every `./...` below stops at their
# directory boundary, so they need naming explicitly or they get no gate at all
# and the run still ends in "ok". `gofmt -l .` is the exception: it is a plain
# file walk that knows nothing of modules, so it already covers them. Keep this
# in step with the go.mod files.
NESTED="tui"

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

# Each nested module is also consumed on its own — `go install
# github.com/FacileStudio/nacelle/tui@latest` resolves tui/go.mod's require of
# the core, not the sibling directory next to it. GOWORK=off is the entire
# point of this step: with the workspace on, every import is satisfied by the
# local source and a stale require is invisible, which is how tui/go.mod sat
# four commits behind a core it could not compile against while this script
# printed "ok". Turning the workspace off builds what a stranger would get.
#
# The root is in this loop for the same reason and it was missing from it,
# which cost a red CI run. Every other step here runs with the workspace on,
# so the root module was only ever built against go.work.sum — and a `go mod
# tidy` run inside the workspace splits the sums between the two files, so
# go.sum can be missing an entry that nothing local ever asks for. `go get
# github.com/FacileStudio/nacelle` reads go.mod and go.sum and has never heard
# of the workspace file sitting next to them, which is exactly what CI's own
# GOWORK=off job proved and this script did not.
echo "==> go build (no workspace)"
(GOWORK=off "$GO" build ./...) || status=1
for module in $NESTED; do
  (GOWORK=off "$GO" -C "$module" build ./...) || status=1
done

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

# The linter is guarded on the binary running rather than on it being on PATH.
# A mise shim for a version that was never installed resolves under
# `command -v` and then fails on every invocation, which reads as a repository
# that does not lint rather than as a machine that cannot.
if [ "$mode" != "nolint" ]; then
  echo "==> golangci-lint"
  if golangci-lint version >/dev/null 2>&1; then
    golangci-lint run ./... || status=1
    # Naming a nested module as a second pattern instead — `golangci-lint run
    # ./... ./tui/...` — prints "directory prefix tui does not contain main
    # module" to stderr and still exits 0, so a broken nested module would pass
    # silently. cd, and pass the config explicitly rather than trusting the
    # upward search to find it from in there.
    for module in $NESTED; do
      (cd "$module" && golangci-lint run --config "$root/.golangci.yml" ./...) || status=1
    done
  else
    echo "check: no usable 'golangci-lint', skipping the lint pass (CI still runs it)" >&2
  fi
fi

if [ "$status" -ne 0 ]; then
  echo "check failed"
  exit "$status"
fi

echo "==> ok"
