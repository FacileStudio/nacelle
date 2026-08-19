#!/usr/bin/env bash
# Drives the nacelle TUI inside a detached tmux session.
#
# Usage:
#   driver.sh launch [dir]           start the app (default dir: cwd)
#   driver.sh ask "<unique text>"    type text, press enter, wait for the
#                                    round to settle (status back to "ready")
#   driver.sh capture                print the current pane
#   driver.sh scroll-up|scroll-down  send PageUp/PageDown, print the pane
#   driver.sh quit                   ctrl+c then kill the tmux session
#
# Every subcommand prints the pane after acting, so the caller doesn't need
# a separate capture call in the common case.
set -euo pipefail

SESSION=nacelle-driver
BIN="${NACELLE_BIN:-$HOME/.local/bin/nacelle}"

wait_for() {
  # $1: grep pattern, $2: timeout seconds (default 10)
  timeout "${2:-10}" bash -c "until tmux capture-pane -t '$SESSION' -p | grep -q '$1'; do sleep 0.2; done"
}

case "${1:-}" in
launch)
  dir="${2:-.}"
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  # -backend anthropic is not a preference, it is what makes this driver work
  # on any machine: OpenRouter's backend validates its API key at
  # construction and the process exits before the TUI ever appears if the
  # key is missing, while Anthropic's backend defers that to the first real
  # request — the only way to reach the interactive screen without a key.
  tmux new-session -d -s "$SESSION" -x 100 -y 30 \
    "cd '$dir' && '$BIN' -backend anthropic; sleep 30"
  wait_for '·' 10
  tmux capture-pane -t "$SESSION" -p
  ;;
ask)
  [ $# -ge 2 ] || {
    echo "usage: driver.sh ask \"<unique text>\"" >&2
    exit 2
  }
  tmux send-keys -t "$SESSION" "$2" Enter
  sleep 0.1 # closes a real, if narrow, race: send-keys can return before
            # bubbletea has processed Enter, and the very next check below
            # can catch the text still sitting in the unsubmitted prompt.
  wait_for "$2" 10
  wait_for 'ready · ' 15
  tmux capture-pane -t "$SESSION" -p
  ;;
capture)
  tmux capture-pane -t "$SESSION" -p
  ;;
scroll-up)
  tmux send-keys -t "$SESSION" PageUp
  sleep 0.2
  tmux capture-pane -t "$SESSION" -p
  ;;
scroll-down)
  tmux send-keys -t "$SESSION" PageDown
  sleep 0.2
  tmux capture-pane -t "$SESSION" -p
  ;;
quit)
  tmux send-keys -t "$SESSION" C-c 2>/dev/null || true
  sleep 0.2
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  ;;
*)
  echo "usage: driver.sh {launch [dir]|ask \"<text>\"|capture|scroll-up|scroll-down|quit}" >&2
  exit 2
  ;;
esac
