# nacelle — Development

Local setup, the quality gate, CI, and how versions are cut.

## Prerequisites

- **Go 1.25.0** (`go.mod`), which is the documented floor for a backend importing the core
  loop. `mise.toml` pins exactly that.
- **mise**, for the task runner. Optional — every task is a one-line shell command runnable
  directly.
- **golangci-lint v2**, optional locally. CI pins `v2.12.2`. The local gate skips the lint
  pass and says so when the binary is missing or fails to run.

There is no database, no Docker, and no client half — the terminal client lives in its own
repository, [FacileStudio/nacelle-tui](https://github.com/FacileStudio/nacelle-tui).

## Tasks

```sh
mise run check      # gofmt + vet + test + golangci-lint
mise run test       # go test ./...
mise run format     # rewrite Go sources in place
```

## Git hooks

Run `mise install` once per clone. It installs the pinned toolchain and then runs
`lefthook install` through its `postinstall` hook, which is what writes the git hooks.

`lefthook.yml` wires two of them. `commit-msg` comes from the shared config in
[FacileStudio/hooks](https://github.com/FacileStudio/hooks), pinned by tag: it requires a
Conventional Commits subject, `type(scope): summary`, and rewrites the subject with the
gitmoji for that type, so do not type the emoji yourself. `pre-push` calls
`scripts/check.sh` directly, so a bad push is caught before it leaves your machine (though
not, on its own, before it merges, see [CI](#ci)).

The script itself is unchanged by the move to lefthook and is still the gate; only its
caller moved. lefthook caches the shared config at the pinned `ref`, so there is no network
call per commit. Bump that `ref` in `lefthook.yml` to pick up a new policy.

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
backends must never import each other, and `tools/` and `mcp/` must never import a provider
SDK directly — a tool has to be callable by every backend, which is why `nacelle.Tool`
is this package's own interface. `failOn: info`, deliberately strict — every style rule filet
has is `info` severity, so a looser threshold could only ever fire on a Go file that does not
parse or a forbidden import.

## CI

Two workflows under `.github/workflows/`, both on push to `main` and on every pull request:

- **`ci.yml`** — one job on Go 1.25 with `GOTOOLCHAIN=local`: gofmt, build, vet, `test -race`,
  `golangci-lint-action@v7` at `v2.12.2`.
- **`filet.yml`** — runs `filet check`, `fail-on: info`.

CI is what makes the gate unbypassable: the pre-push hook stops a local `git push --no-verify`
skip from reaching `main` unnoticed, but only CI actually blocks a PR.

## Tests

Every package carries its own `_test.go` files. Converter tests assert on the marshalled wire
bytes, not on internal Go structs — `anthropic/conversation_test.go` and
`openrouter/conversation_test.go` both check the literal JSON a request would carry, because
that is the only level at which "the model sees it" means anything.

## Versioning

Semver tags, never branch tracking. While on `v0`, a breaking change bumps the **minor**.
Every change is recorded in [CHANGELOG.md](../CHANGELOG.md) in Keep a Changelog format, with
the reason it exists. Add an `Unreleased` entry as part of the change, not after.

Tagged since 2026-08-22: `v0.1.0`, then `v0.2.0` onward for the reasoning and hooks work.
One module, one tag per release. The terminal client pins whichever tag it has tested against;
its require bump is a commit in its own repository, not a second tag here.

### Cutting a release

Check first with the suite-wide flow:

```sh
mycelium flow run release-preflight
```

Then it is one commit, one tag:

```sh
sh scripts/check.sh
git commit && git push && git tag vX.Y.Z && git push origin vX.Y.Z
```

A **breaking** change bumps the minor while on v0 (see above) and deserves an entry in
[CHANGELOG.md](../CHANGELOG.md) saying why the break was worth it. Consumers — nacelle-tui
included — pick the new version up by bumping their own `require`, on their own schedule.
