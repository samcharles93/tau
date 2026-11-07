---
name: tmux-tui-testing
description: Build, run, and drive tau end-to-end - the Bubbletea v2 TUI (internal/tui2) via a detached tmux session with send-keys/capture-pane, plus non-interactive CLI subcommands (provider/plugin/skills list) for changes that don't touch the UI. Use whenever a tau change needs to be verified actually running, not just passing go test - e.g. after editing internal/tui2/*.go, internal/cli/*.go, or any Go package, or when asked to manually test/verify/reproduce a bug, build tau, or run tau.
---

# Running and testing tau

tau is a Go CLI with two surfaces: non-interactive subcommands (`provider`,
`plugin`, `skills`, ...) and an interactive Bubbletea v2 TUI (`internal/tui2`,
the default). The TUI is an alt-screen app, so
it can't be driven with plain Bash piping. This skill covers building it,
launching it safely (without touching the user's real session history or
making unintended API calls), and driving it with tmux send-keys/capture-pane.

**Fastest path:** run `.Codex/skills/tmux-tui-testing/smoke.sh` from the repo
root. It builds a scratch binary, sets up isolated config, exercises the
non-interactive subcommands, launches the TUI in tmux, drives one overlay
(`/help`), and cleans up - prints `PASS`/`FAIL`. Use it as a first check
after any change; use the manual steps below when you need to reproduce or
poke at something specific.

## 0. Prerequisites

`tmux` and a Go toolchain on `PATH` - nothing else. Verified with `tmux 3.7b`
/ `go1.26.5`; no OS packages, no Xvfb, no GPU (it's a terminal app, not a
GUI).

## 1. Build to scratch

```bash
go build -o "$SCRATCH/tau-test" ./cmd/tau
```

`$SCRATCH` is this session's scratchpad dir; use any writable dir if driving
this manually outside a Codex session. Rebuild after every code change
before re-testing — a stale binary is the most common cause of "my fix isn't
showing up."

## 2. Isolated config (never use ~/.config/tau directly)

Copy just the read-only config pieces into a scratch dir, mirroring
`scripts/tui-parity.sh`:

```bash
SRC="${XDG_CONFIG_HOME:-$HOME/.config}/tau"
mkdir -p "$SCRATCH/tau-config"
for f in config.yaml auth.yaml models.json; do
  [ -f "$SRC/$f" ] && cp "$SRC/$f" "$SCRATCH/tau-config/$f"
done
chmod 600 "$SCRATCH/tau-config/auth.yaml" 2>/dev/null || true
```

This gives the test instance the user's real provider/model config (so
completions, `/model`, etc. have real data) while a fresh `sessions.db` means
no real session history leaks in and nothing you do here touches it.

## 2a. Direct invocation (no TUI, no tmux) - covers most PRs

Most changes touch internal Go packages, not `internal/tui2` itself - for
those, `go test ./...` (or `task check`) is the real verification and this
whole tmux dance is unnecessary. When you do want to exercise the built
binary without a TUI, tau's non-interactive subcommands work fine headless
against the isolated config from step 2, no TTY required:

```bash
TAU_CONFIG_DIR="$SCRATCH/tau-config" "$SCRATCH/tau-test" provider list
TAU_CONFIG_DIR="$SCRATCH/tau-config" "$SCRATCH/tau-test" plugin list
TAU_CONFIG_DIR="$SCRATCH/tau-config" "$SCRATCH/tau-test" skills list
```

**`-p`/`--prompt` (single-shot mode) is NOT usable headless** - even with an
empty prompt it still tries to initialize the TUI and fails with `bubbletea:
could not open TTY: open /dev/tty: no such device or address` when there's
no real terminal attached (confirmed 2026-07-24). Don't reach for `-p` in a
headless/CI context; use the subcommands above or the tmux flow below
instead.

## 3. Launch in tmux

```bash
tmux new-session -d -s tautest -x 140 -y 40 \
  "TAU_CONFIG_DIR=$SCRATCH/tau-config $SCRATCH/tau-test chat"
sleep 1.5   # first render takes a moment
```

Pick `-x`/`-y` to match the scenario
(narrow widths are useful for testing status-bar/layout truncation).

## 4. Read the screen

```bash
tmux capture-pane -t tautest -p
```

- If this comes back blank right after sending a key, it's a tmux redraw-timing
  artifact — just capture again. It does not mean the app crashed.
- Add `-S -N` to include N lines of scrollback if content scrolled past the
  visible pane (e.g. `-S -5`).
- Check `tmux list-sessions` if something looks wrong — confirm the session is
  still alive before assuming an app bug.

## 5. Drive it

```bash
tmux send-keys -t tautest '/model'      # literal text
tmux send-keys -t tautest Enter         # special keys: tmux's own names
tmux send-keys -t tautest Escape Up Down Home End
tmux send-keys -t tautest C-p C-l C-o   # Ctrl-chords: tmux's C-x form
```

**Limitation:** tmux `send-keys` can only emit what a legacy terminal encodes
in plain ASCII/CSI. It cannot reliably simulate Kitty-keyboard-protocol-only
events — Ctrl+Backspace, Ctrl+Shift+<letter>, etc. — there's no ASCII encoding
for them. Sending `C-h` for "Ctrl+Backspace" will NOT reach the app the same
way a real Kitty-protocol terminal would; don't trust a no-op result from that
as a real finding. Verify those specific bindings with a Go unit test instead,
using this repo's `key(code, mod tea.KeyMod)` helper
(`internal/tui2/input_test.go`), which constructs the exact `tea.KeyPressMsg`
a Kitty-aware terminal would produce and calls `m.dispatchKey(...)` directly.

## 6. Don't trigger real LLM turns

Submitting a plain chat message starts a real, billed turn against whatever
provider is configured in the copied `auth.yaml`. Stick to slash commands,
overlay navigation (Ctrl+P/L/O, `/help`, etc.), and other local-only
interactions unless the task specifically requires exercising a live turn —
and say so explicitly if you do.

## 7. Clean up

```bash
tmux send-keys -t tautest C-c   # tau's double-tap-to-quit guard: send twice
sleep 0.2
tmux send-keys -t tautest C-c
tmux kill-session -t tautest 2>/dev/null || true
```

Always kill the session when done, even after a failed test — check
`tmux list-sessions` before ending the task to make sure nothing orphaned is
still running.

## Troubleshooting

- **`skills list` prints a wall of `[error]`/`[warning]` skill-loading
  diagnostics** (oversized SKILL.md files, name/directory mismatches, a
  shadowed skill) before the actual table. This is real output from scanning
  every skill directory on the machine (`~/.agents/skills`, project
  `.agents/skills`), not a tau bug or a sign the command failed - the table
  still prints after it. If you're capturing output programmatically, don't
  assume stderr noise means the command errored; check the exit code.
- **`bubbletea: could not open TTY`** from any invocation (including `-p`) -
  you're running without a real terminal attached. Use the tmux flow above,
  or one of the non-interactive subcommands in step 2a.
- **tmux capture comes back showing unrelated content** (e.g. another
  session's output) - you likely reused a session name from a previous run
  that didn't get cleaned up. `tmux kill-session -t <name>` first, or use a
  unique name per run (see `smoke.sh`'s `tau-smoke-$$`).

## Driver script

`.Codex/skills/tmux-tui-testing/smoke.sh` packages steps 1-7 into a single
pass/fail check (see "Fastest path" above). Extend it if you find yourself
repeating the same manual sequence across sessions; keep the manual steps in
this file for anything more exploratory (reproducing a specific bug,
inspecting a specific overlay, testing at unusual widths).
