#!/bin/sh
set -eu

cd "$(dirname "$0")"

SINCE="${CODEX_RECOVER_SINCE:-2026-05-26}"
ARCH="$(uname -m)"

case "$ARCH" in
  arm64|aarch64) BIN="./bin/codex-usage-darwin-arm64" ;;
  x86_64|amd64) BIN="./bin/codex-usage-darwin-amd64" ;;
  *)
    echo "Unsupported Mac architecture: $ARCH"
    echo
    echo "Press Enter to exit."
    read -r _
    exit 1
    ;;
esac

xattr -dr com.apple.quarantine . 2>/dev/null || true
chmod +x "$BIN" 2>/dev/null || true

echo "Scanning local Codex sessions..."
echo
AMOUNT="$("$BIN" --since "$SINCE" --total-only --status --price-file "./model_prices_and_context_window.json")"
echo
echo "Final usage cost: ${AMOUNT} USD"
echo
echo "Press Enter to exit."
read -r _
