#!/usr/bin/env bash
# E2E smoke test for the Tau Web UI.
#
# This script builds Tau, starts it with an ephemeral web port, then verifies
# that the embedded SPA is served and the health endpoint responds. If websocat
# is installed, it also verifies the WebSocket init handshake.
#
# Usage:
#   TAU_PROVIDER=openai TAU_MODEL=gpt-5.4 ./scripts/e2e-webui.sh
#
# The provider/model env vars are optional if you already have a config file.

set -euo pipefail

cd "$(dirname "$0")/.."

echo "Building tau..."
go build -o /tmp/tau-e2e ./cmd/tau

TAIL_PID=""
cleanup() {
  if [ -n "${TAIL_PID:-}" ]; then kill "$TAIL_PID" 2>/dev/null || true; fi
  rm -f /tmp/tau-e2e /tmp/tau-e2e.log /tmp/tau-health.json
}
trap cleanup EXIT

ARGS=("--no-web")
if [ -n "${TAU_PROVIDER:-}" ]; then ARGS+=("--provider" "$TAU_PROVIDER"); fi
if [ -n "${TAU_MODEL:-}" ]; then ARGS+=("--model" "$TAU_MODEL"); fi

echo "Starting tau with web UI..."
/tmp/tau-e2e "${ARGS[@]}" > /tmp/tau-e2e.log 2>&1 &
TAIL_PID=$!

URL=""
for _ in $(seq 1 30); do
  URL=$(grep -oE 'http://127\.0\.0\.1:[0-9]+' /tmp/tau-e2e.log | tail -1 || true)
  if [ -n "$URL" ]; then break; fi
  sleep 0.2
done

if [ -z "$URL" ]; then
  echo "Tau did not print a web URL. Last log lines:"
  tail -20 /tmp/tau-e2e.log
  exit 1
fi

echo "Web UI available at $URL"

# Verify health endpoint.
if ! curl -sf "$URL/health" > /tmp/tau-health.json; then
  echo "Health check failed"
  tail -20 /tmp/tau-e2e.log
  exit 1
fi
echo "Health: $(cat /tmp/tau-health.json)"

# Verify SPA fallback.
if ! curl -sf "$URL/chat" | grep -q "Tau Web UI"; then
  echo "SPA fallback failed"
  exit 1
fi
echo "SPA fallback OK"

# Verify WebSocket init if websocat is available.
if command -v websocat >/dev/null 2>&1; then
  MSG=$(echo '{"type":"SubmitChatPromptCommand","payload":{"session_id":"e2e","request_id":"e2e-1","prompt":"hello","submitted_at":"2026-06-27T00:00:00Z"}}' | timeout 5 websocat "$URL/ws" 2>/dev/null | head -1 || true)
  if echo "$MSG" | grep -q '"type":"init"'; then
    echo "WebSocket init OK: $MSG"
  else
    echo "WebSocket init failed: ${MSG:-(empty)}"
    exit 1
  fi
else
  echo "websocat not installed; skipping WebSocket command round-trip"
fi

echo "E2E smoke test passed."
