#!/usr/bin/env bash
# One-shot smoke driver for tau: builds a scratch binary, sets up an isolated
# config (never touches ~/.config/tau), launches tui2 in a detached tmux
# session, sends a couple of harmless local-only commands, captures the
# screen, and cleans up. Exits non-zero if the binary fails to build or the
# tmux session never renders a prompt.
#
# This is the "does the whole thing still work" check - for actually
# exploring/reproducing a specific TUI bug, drive the same tmux session
# interactively instead (see SKILL.md step 5); this script is not meant to
# be extended into a full test harness.
#
# Usage: ./smoke.sh [scratch-dir]
#   scratch-dir defaults to $SCRATCH if set, else a mktemp dir (cleaned up
#   on exit unless $SCRATCH was pre-set, in which case it's left in place).
set -euo pipefail
cd "$(dirname "$0")/../../.."   # repo root (this file is .claude/skills/tmux-tui-testing/smoke.sh)

SESSION="tau-smoke-$$"
OWN_SCRATCH=0
SCRATCH_DIR="${1:-${SCRATCH:-}}"
if [ -z "$SCRATCH_DIR" ]; then
  SCRATCH_DIR="$(mktemp -d)"
  OWN_SCRATCH=1
fi

cleanup() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  if [ "$OWN_SCRATCH" = 1 ]; then
    rm -rf "$SCRATCH_DIR"
  fi
}
trap cleanup EXIT

echo "== building =="
go build -o "$SCRATCH_DIR/tau-test" ./cmd/tau

echo "== isolated config =="
SRC="${XDG_CONFIG_HOME:-$HOME/.config}/tau"
mkdir -p "$SCRATCH_DIR/tau-config"
for f in config.yaml auth.yaml models.json; do
  [ -f "$SRC/$f" ] && cp "$SRC/$f" "$SCRATCH_DIR/tau-config/$f"
done
chmod 600 "$SCRATCH_DIR/tau-config/auth.yaml" 2>/dev/null || true

echo "== non-interactive subcommands (no TTY, no LLM call) =="
TAU_CONFIG_DIR="$SCRATCH_DIR/tau-config" "$SCRATCH_DIR/tau-test" provider list >/dev/null
TAU_CONFIG_DIR="$SCRATCH_DIR/tau-config" "$SCRATCH_DIR/tau-test" plugin list >/dev/null
TAU_CONFIG_DIR="$SCRATCH_DIR/tau-config" "$SCRATCH_DIR/tau-test" skills list >/dev/null
echo "  ok"

echo "== launching tui2 in tmux =="
tmux new-session -d -s "$SESSION" -x 100 -y 30 \
  "TAU_CONFIG_DIR=$SCRATCH_DIR/tau-config $SCRATCH_DIR/tau-test chat --no-web"
sleep 1.5

echo "== initial render =="
out="$(tmux capture-pane -t "$SESSION" -p)"
if ! echo "$out" | grep -q '│>'; then
  echo "FAIL: chat prompt box not found in initial render" >&2
  echo "$out" >&2
  exit 1
fi
if ! echo "$out" | grep -qE 'τ.*Ready'; then
  echo "FAIL: status bar 'τ ... Ready' not found in initial render" >&2
  echo "$out" >&2
  exit 1
fi
echo "  ok (prompt box + status bar present)"

echo "== driving: /help overlay =="
tmux send-keys -t "$SESSION" '/help'
tmux send-keys -t "$SESSION" Enter
sleep 0.5
out="$(tmux capture-pane -t "$SESSION" -p)"
if ! echo "$out" | grep -q 'REASONING'; then
  echo "FAIL: /help overlay did not render" >&2
  echo "$out" >&2
  exit 1
fi
echo "  ok"
tmux send-keys -t "$SESSION" Escape
sleep 0.3

echo "== quitting (double Ctrl+C guard) =="
tmux send-keys -t "$SESSION" C-c
sleep 0.2
tmux send-keys -t "$SESSION" C-c
sleep 0.3

echo "PASS"
