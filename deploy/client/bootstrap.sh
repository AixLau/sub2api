#!/usr/bin/env bash
#
# Sub2API macOS/Linux 客户端引导脚本。
# 用法：curl -fsSL <url>/bootstrap.sh | bash

set -euo pipefail

DEFAULT_BASE_URL="https://aixlau.me/install"
BASE_URL="${SUB2API_CLIENT_SETUP_BASE_URL:-$DEFAULT_BASE_URL}"
BASE_URL="${BASE_URL%/}"
CACHE_BUST="$(date +%s 2>/dev/null || printf now)"
SOURCE_PATH="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR=""
if [ -f "$SOURCE_PATH" ]; then
  SCRIPT_DIR="$(cd "$(dirname "$SOURCE_PATH")" 2>/dev/null && pwd 2>/dev/null || true)"
fi

case "$(uname -s 2>/dev/null || printf unknown)" in
  Darwin|Linux)
    ;;
  MINGW*|MSYS*|CYGWIN*)
    printf '当前脚本适用于 macOS/Linux Bash。\n' >&2
    printf 'Windows 请运行：irm %s/bootstrap.ps1 | iex\n' "$BASE_URL" >&2
    exit 1
    ;;
  *)
    printf '当前操作系统不支持 Bash 引导脚本。\n' >&2
    exit 1
    ;;
esac

if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/setup-sub2api-client.sh" ]; then
  exec bash "$SCRIPT_DIR/setup-sub2api-client.sh" "$@"
fi

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$BASE_URL/setup-sub2api-client.sh?v=$CACHE_BUST" | bash -s -- "$@"
elif command -v wget >/dev/null 2>&1; then
  wget -qO- "$BASE_URL/setup-sub2api-client.sh?v=$CACHE_BUST" | bash -s -- "$@"
else
  printf '未找到 curl 或 wget，无法下载安装脚本。\n' >&2
  exit 1
fi
