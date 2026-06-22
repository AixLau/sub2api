#!/usr/bin/env sh
set -eu

BASE_URL="${CODEX_RECOVER_BASE_URL:-https://ai.aixlau.me/codex-recover}"
TMP_DIR="${TMPDIR:-/tmp}/codex-usage-recover"
SINCE="${CODEX_RECOVER_SINCE:-2026-05-26}"

ARCH="$(uname -m)"
case "$ARCH" in
  arm64|aarch64) BIN="codex-usage-darwin-arm64" ;;
  x86_64|amd64) BIN="codex-usage-darwin-amd64" ;;
  *)
    echo "Unsupported Mac architecture: $ARCH" >&2
    exit 1
    ;;
esac

mkdir -p "$TMP_DIR"
echo "Downloading usage tool from $BASE_URL/$BIN ..."
curl --retry 5 --retry-delay 2 --retry-all-errors -fL "$BASE_URL/$BIN" -o "$TMP_DIR/codex-usage"
echo "Downloading price table from $BASE_URL/model_prices_and_context_window.json ..."
curl --retry 5 --retry-delay 2 --retry-all-errors -fL "$BASE_URL/model_prices_and_context_window.json" -o "$TMP_DIR/model_prices_and_context_window.json"
chmod +x "$TMP_DIR/codex-usage"

echo "Scanning local Codex sessions..."
"$TMP_DIR/codex-usage" \
  --since "$SINCE" \
  --total-only \
  --status \
  --price-file "$TMP_DIR/model_prices_and_context_window.json"
