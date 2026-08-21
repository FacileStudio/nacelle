# nacelle — Development

Local setup, the quality gate, CI, and how versions are cut.

## Prerequisites

- **Go 1.25.0** for the root module (`go.mod`). `tui/` is a separate module on
  **Go 1.26.4** — Bubble Tea v2 declares that floor, and the root stays on 1.25 so the
  documented floor for a backend importing the core loop stays honest. `mise.toml` pins
  `1.26.4`, the higher of the two, so one toolchain covers both.
- **mise**, for the task runner. Optional — every task is a one-line shell command runnable
  directly.
- **golangci-lint v2**, optional locally. CI pins `v2.12.2`. The local gate skips the lint
  pass and says so when the binary is missing or fails to run.

There is no database, no Docker, and no client half beyond `tui/` itself.

## Tasks

```sh
mise run check      # gofmt + vet + test + golangci-lint, every module
mise run test       # go test ./... && go -C tui test ./...
mise run format     # rewrite Go sources in place
mise run hooks      # enable the tracked git hooks in this clone
```

`mise run hooks` is `git config core.hooksPath .githooks`. Run it once per clone — it wires
`.githooks/pre-push`, which calls `scripts/check.sh` directly, so a bad push is caught before
it leaves your machine (though not, on its own, before it merges — see [CI](#ci)).

## The quality gate

`scripts/check.sh` reports and never rewrites, except under `--format`.

```sh
sh scripts/check.sh           # gofmt + vet + test -race + golangci-lint
sh scripts/check.sh --no-lint # skip the lint pass
sh scripts/check.sh --format  # gofmt -w . and exit
```

Every stage runs even after an earlier one fails, so one invocation reports everything rather
than one thing at a time. Worth knowing before changing it:

- **Not invoked through `mise run`.** `mise run` resolves every tool in the *merged* config
  before running any task body, so one broken tool anywhere in a global mise config would take
  the gate down with it. The hook and CI both call the script directly.
- **Resolves the toolchain from `GOROOT` when set.** mise exports `GOROOT` for the pinned
  version while leaving an unrelated `go` earlier on `PATH`, and a `go` binary driving a
  different `GOROOT` fails with `compile: version "X" does not match go tool version "Y"`.
- **`golangci-lint` is probed by running it**, not by `command -v` — a mise shim for a version
  never installed resolves on `PATH` and then fails on every invocation, which would read as a
  repository that does not lint rather than a machine that cannot.
- **`tui/` is built with `GOWORK=off`.** With the workspace on, every import is satisfied from
  the local source tree and a stale `require` in `tui/go.mod` is invisible — which is how it
  once sat several commits behind a core it could not actually compile against, while the gate
  printed `ok`. `GOWORK=off` builds what `go install github.com/FacileStudio/nacelle/tui@latest`
  would actually get.
- **`-race`, not a nicety.** Tool handlers run concurrently on the SDK's own goroutines and
  park their results in a mutex-guarded sink — exactly the shape the detector exists for.

## Linters

`.golangci.yml` runs with `default: none` and an explicit enable list: `errcheck` (including
type assertions), `govet`, `ineffassign`, `staticcheck` (all checks), `unused`, `bodyclose`,
`errorlint`, `misspell`, `nilerr`, `unconvert`, plus the `gofmt` formatter. Test files are
excluded from `errcheck` and `bodyclose`. Issue caps are lifted
(`max-issues-per-linter: 0`, `max-same-issues: 0`), so a run shows everything it finds.

## `filet`

`filet.yml`'s `architecture` block is the layering enforced, not just documented: the two
backends must never import each other, and `tools/`, `mcp/` and `tui/` must never import a
provider SDK directly — a tool has to be callable by every backend, which is why `nacelle.Tool`
is this package's own interface. `failOn: info`, deliberately strict — every style rule filet
has is `info` severity, so a looser threshold could only ever fire on a Go file that does not
parse or a forbidden import.

## CI

Two workflows under `.github/workflows/`, both on push to `main` and on every pull request:

- **`ci.yml`** — `check` (root module, Go 1.25, `GOWORK=off`/`GOTOOLCHAIN=local`): gofmt, vet,
  `test -race`, `golangci-lint-action@v7` at `v2.12.2`. `check-modules` (matrix, currently just
  `tui` on Go 1.26.4): build, vet, `test -race`, lint with `--config ../.golangci.yml`.
- **`filet.yml`** — runs `filet check`, `fail-on: info`.

CI is what makes the gate unbypassable: the pre-push hook stops a local `git push --no-verify`
skip from reaching `main` unnoticed, but only CI actually blocks a PR.

## Tests

Every package carries its own `_test.go` files. Converter tests assert on the marshalled wire
bytes, not on internal Go structs — `anthropic/conversation_test.go` and
`openrouter/conversation_test.go` both check the literal JSON a request would carry, because
that is the only level at which "the model sees it" means anything. `tui/`'s tests drive the
Bubble Tea model directly (`m.consume(...)`, `m.settle()`, `m.absorb(...)`) rather than through
a rendered terminal, and check the resulting viewport text with ANSI stripped.

## Versioning

Semver tags, never branch tracking. While on `v0`, a breaking change bumps the **minor**.
Every change is recorded in [CHANGELOG.md](../CHANGELOG.md) in Keep a Changelog format, with
the reason it exists. Add an `Unreleased` entry as part of the change, not after.

No tag exists yet, and nothing outside this repo decides when one does. Staying untagged has
a running cost: every consumer pins a pseudo-version, and `tui/go.mod`'s has to be bumped by
hand each time the core moves. See [`ROADMAP.md`](../ROADMAP.md) at the repo root.
