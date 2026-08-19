---
name: run-nacelle-tui
description: Build, run, and drive nacelle's TUI (the terminal coding-assistant client in tui/). Use when asked to start nacelle, run the nacelle TUI, build it, screenshot/capture its screen, or interact with the running app — including without an API key, to check the client itself rather than a model's answers.
---

`nacelle` is a Go agent SDK; `tui/` is its one runnable app, a Bubble Tea
terminal client. It is alt-screen and raw-mode, so it has to be driven
through tmux — drive it via
`.claude/skills/run-nacelle-tui/driver.sh` (relative to `tui/`).

All paths below are relative to `tui/`.

## Prerequisites

- `tmux` — already present on this machine (`tmux -V` → 3.5a). If missing:
  `sudo apt-get install -y tmux`.
- The Go toolchain nacelle pins (`tui/mise.toml`'s floor is 1.26.4, root is
  1.25). This machine has it userspace at `~/.local/go/bin`, not on `PATH`
  by default:

  ```bash
  export PATH="$HOME/.local/go/bin:$PATH"
  ```

## Build

From `tui/`:

```bash
go build -o ~/.local/bin/nacelle .
```

`go build -o`, not `go install` — `go install .../tui@latest` would name the
binary `tui`, not `nacelle` (the module's last path element). See
`ROADMAP.md`/the wiki if this ever needs re-deriving.

## Run (agent path)

Use the driver — every subcommand prints the resulting pane, so a separate
capture call usually isn't needed:

```bash
D=.claude/skills/run-nacelle-tui/driver.sh
"$D" launch [dir]              # start the app (default dir: cwd)
"$D" ask "<unique text>"       # type it, press enter, wait for it to settle
"$D" capture                   # print the current pane
"$D" scroll-up                 # PageUp, print the pane
"$D" scroll-down                # PageDown, print the pane
"$D" quit                      # ctrl+c, then kill the tmux session
```

`ask`'s text has to be unique per call — the driver polls for that exact
string to confirm the round has actually started before it starts polling
for `ready · ` again, and a repeated string would match the *previous*
round's echo instead of waiting for a new one.

Session name is `nacelle-driver` (fixed); `NACELLE_BIN` overrides the binary
path if you're testing one that isn't installed at `~/.local/bin/nacelle`.

Nothing here needs an API key. `launch` always passes `-backend anthropic`
regardless of `~/.nacelle.yml`, specifically so the app reaches its
interactive screen with no credentials at all — see Gotchas. Without a key,
every `ask` will settle on a real (visible) auth error rather than a real
answer; that's expected, and it's still enough to verify the client itself:
the prompt, the spinner, scrolling, styling, all of it.

## Run (human path)

```bash
~/.local/bin/nacelle
```

Type, Enter to ask, ctrl+c to stop a run or quit, ctrl+\ to force-quit.
Useless headless — needs a real terminal.

## Test

From `tui/`: `go test ./...` (add `-race` to match the repo gate). All
green as of this writing.

## Gotchas

- **The default backend can prevent the TUI from ever appearing.**
  `~/.nacelle.yml` on this machine sets `backend: openrouter`, and
  OpenRouter's backend validates its API key at *construction* — with no
  key, the process prints one line to stderr and exits before
  `tea.NewProgram(...).Run()` is ever called, so tmux's pane closes before
  a `capture-pane` can catch anything. Anthropic's backend defers key
  validation to the first real HTTP request, so `-backend anthropic`
  (which the driver always passes) is what actually gets you into the
  interactive screen on a machine with no key configured. This is not a
  preference — plain `~/.local/bin/nacelle` with no override may not even
  launch, depending on what's in `~/.nacelle.yml`.
- **`ask` needs unique text or its own polling races itself.** The first
  version of this driver polled for `ready · ` after every `send-keys`
  with no other check; sending two questions back-to-back matched an
  earlier round's stale "ready" the instant the second `send-keys` landed,
  and the two questions ended up concatenated in the prompt instead of
  each getting its own turn. Fixed by polling for the question's own text
  first, *then* for the ready marker.
- **A second, narrower race in that same fix**: `send-keys "$text" Enter`
  can return before bubbletea has processed the Enter, so the very next
  poll can catch the text still sitting unsubmitted in the prompt line
  rather than echoed into the transcript — seen once in six real runs
  while writing this driver. `ask` sleeps 0.1s after `send-keys` before
  polling, which cleared it across five repeat runs; if it ever
  resurfaces, the pane output makes it obvious (the prompt line still
  holds the text instead of the placeholder) — retry the `ask`.
- **This machine's default `GOMODCACHE` has root-owned entries** (e.g.
  under `golang.org/x`), so a `go get`/`go build` that needs to download a
  new dependency can fail with `permission denied` even though the build
  itself is fine. Not hit by the plain `go build` above (everything's
  already vendored in the module cache), but if it comes up:
  `export GOMODCACHE=<a scratch dir you own>` for that one command.

## Troubleshooting

- **`nacelle: nacelle/openrouter: no API key; set OPENROUTER_API_KEY or Config.APIKey`
  printed once, no TUI appears**: this is the construction-time-validation
  gotcha above. Use `driver.sh launch`, which always passes
  `-backend anthropic`, or pass it yourself if invoking the binary directly.
- **`tmux: can't find pane: nacelle-driver` right after `launch`**: the
  app exited before the driver's `wait_for '·'` could see the banner —
  almost always the OpenRouter case above. Run `"$BIN" -backend anthropic`
  directly (no tmux) to see the real stderr.
