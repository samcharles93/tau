#!/usr/bin/env bash
# Visual parity sign-off between tau's legacy inline TUI (--legacy-tui) and
# the default Bubbletea-based tui2, per reference/tui-migration/plan.md's
# Phase 3/5 verification gate.
#
# Drives an identical scripted interaction against both renderers via termvis
# (https://github.com/samcharles93/termvis) and saves before/after screenshots
# for manual comparison. Requires `termvis`, `ttyd`, and a Chrome/Chromium
# browser on PATH — see `termvis skill show` or the termvis skill's
# check-deps.sh for a preflight check.
#
# Runs against a REAL provider (whatever your ~/.config/tau is configured
# with) — this makes real, billed API calls. Config/auth/model cache are
# copied into an isolated scratch dir first, so your real session history is
# never touched.
#
# Known flakiness: tau negotiates advanced terminal features on startup
# (Kitty keyboard protocol, synchronized-output mode, focus reporting) that
# ttyd's canvas-mode xterm.js bridge doesn't always handle cleanly — this can
# surface as a screenshot showing ttyd's own "Press <Enter> to Reconnect"
# placeholder instead of tau's UI, even though tau itself is running fine
# (verified separately by running it in a real pty with no ttyd in between).
# This looks like a termvis/ttyd/xterm.js compatibility gap, not a tau bug.
# If you hit it, just rerun — if it's still visible in a screenshot, delete
# that run's --out dir and try again before concluding something regressed.
#
# Usage:
#   ./scripts/tui-parity.sh [--config-dir DIR] [--out DIR] [--width N] [--height N]
#
#   --config-dir DIR   Source tau config to copy auth/models from (default: ~/.config/tau)
#   --out DIR          Where to write screenshots (default: .local/tui-parity/<timestamp>)
#   --width/--height   termvis viewport size in PIXELS, not rows/cols (default: 1200x700)

set -euo pipefail

cd "$(dirname "$0")/.."

SRC_CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/tau"
OUT_DIR=""
WIDTH=1200
HEIGHT=700

while [ $# -gt 0 ]; do
  case "$1" in
    --config-dir) SRC_CONFIG_DIR="$2"; shift 2 ;;
    --out) OUT_DIR="$2"; shift 2 ;;
    --width) WIDTH="$2"; shift 2 ;;
    --height) HEIGHT="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

if ! command -v termvis >/dev/null 2>&1; then
  echo "termvis not found on PATH. Install: go install github.com/samcharles93/termvis@latest" >&2
  exit 1
fi

if [ -z "$OUT_DIR" ]; then
  OUT_DIR=".local/tui-parity/$(date +%Y%m%d-%H%M%S 2>/dev/null || echo run)"
fi
mkdir -p "$OUT_DIR/legacy" "$OUT_DIR/tui2"

WORKDIR="$(mktemp -d)"
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT

echo "Building tau..."
go build -o "$WORKDIR/tau-test" ./cmd/tau

# One isolated config copy per variant so neither run's session history
# (or the real ~/.config/tau) leaks into the other.
for variant in legacy tui2; do
  mkdir -p "$WORKDIR/config-$variant"
  for f in config.yaml auth.yaml models.json; do
    if [ -f "$SRC_CONFIG_DIR/$f" ]; then
      cp "$SRC_CONFIG_DIR/$f" "$WORKDIR/config-$variant/$f"
    fi
  done
  chmod 600 "$WORKDIR/config-$variant/auth.yaml" 2>/dev/null || true
done

# Scripted interaction, shared between both variants: initial render, a plain
# chat turn, a tool-triggering turn, and /help — matches the plan's
# verification checklist (send a message, trigger a tool call, resize).
# termvis's wait_for/stable polls the actual redraw instead of guessing a
# fixed sleep, and self-corrects (returns timed_out instead of erroring) if a
# turn runs long.
run_variant() {
  local name="$1" cmd="$2" outdir="$3"
  local log="$outdir/termvis.log"
  echo "--- $name --- (termvis protocol log: $log)"
  (
    echo '{"action": "key", "value": "escape", "wait_for": {"stable": true, "timeout": "8s"}, "save": "'"$outdir"'/01-start.png"}'
    echo '{"action": "type", "value": "In one short sentence, what is your name and what tools do you have?", "typing_delay": "5ms"}'
    echo '{"action": "enter", "wait_for": {"stable": true, "timeout": "45s"}, "save": "'"$outdir"'/02-response.png"}'
    echo '{"action": "type", "value": "Use your shell tool to run: echo tui-parity-check", "typing_delay": "5ms"}'
    echo '{"action": "enter", "wait_for": {"stable": true, "timeout": "45s"}, "save": "'"$outdir"'/03-tool-call.png"}'
    echo '{"action": "type", "value": "/help", "typing_delay": "5ms"}'
    echo '{"action": "enter", "wait": "500ms"}'
    echo '{"action": "enter", "wait": "500ms", "save": "'"$outdir"'/04-help.png"}'
    echo '{"action": "ctrl", "value": "c", "wait": "300ms"}'
    echo '{"action": "ctrl", "value": "c", "wait": "300ms"}'
  ) | termvis -width "$WIDTH" -height "$HEIGHT" -- bash -c "$cmd" > "$log" 2>&1
}

run_variant "legacy (--legacy-tui)" \
  "TAU_CONFIG_DIR=$WORKDIR/config-legacy $WORKDIR/tau-test chat --no-web --legacy-tui" \
  "$OUT_DIR/legacy"

run_variant "tui2 (default)" \
  "TAU_CONFIG_DIR=$WORKDIR/config-tui2 $WORKDIR/tau-test chat --no-web" \
  "$OUT_DIR/tui2"

echo
echo "Screenshots written to $OUT_DIR/{legacy,tui2}/. Compare 01-start, 02-response, 03-tool-call, 04-help side by side."
