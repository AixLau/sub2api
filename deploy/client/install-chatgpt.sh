#!/usr/bin/env bash
#
# Standalone ChatGPT desktop installer launcher for macOS and Windows shells.

set -euo pipefail

MAC_DOWNLOAD_URL="https://persistent.oaistatic.com/codex-app-prod/ChatGPT.dmg"
WINDOWS_STORE_PRODUCT_ID="9PLM9XGG6VKS"
WINDOWS_STORE_URI="ms-windows-store://pdp/?ProductId=${WINDOWS_STORE_PRODUCT_ID}"
OFFICIAL_DOWNLOAD_PAGE="https://chatgpt.com/download/"

DRY_RUN=0
DOWNLOAD_DIR=""

usage() {
  cat <<'USAGE'
用法: install-chatgpt.sh [--download-dir 目录] [--dry-run]

独立安装 ChatGPT 桌面端：
  macOS       下载并打开 OpenAI 官方最新 DMG
  Windows     在 Git Bash / WSL 中打开 Microsoft Store
  Linux       提示当前没有官方桌面安装包

参数:
  --download-dir  macOS 安装包保存目录，默认使用 ~/Downloads
  --dry-run       只显示将执行的操作，不下载或打开页面
  --help, -h      显示帮助
USAGE
}

fail() {
  printf '错误：%s\n' "$1" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --download-dir)
      [ "$#" -ge 2 ] || fail "缺少 --download-dir 参数值。"
      DOWNLOAD_DIR="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "未知参数：$1"
      ;;
  esac
done

is_wsl() {
  [ -n "${WSL_INTEROP:-}" ] || [ -n "${WSL_DISTRO_NAME:-}" ] ||
    uname -r 2>/dev/null | grep -qiE '(microsoft|wsl)'
}

open_windows_store() {
  printf '检测到 Windows，正在打开 Microsoft Store 的 ChatGPT 页面...\n'
  if [ "$DRY_RUN" = "1" ]; then
    printf '将打开：%s\n' "$WINDOWS_STORE_URI"
    return 0
  fi

  if command -v powershell.exe >/dev/null 2>&1; then
    if powershell.exe -NoLogo -NoProfile -NonInteractive -Command \
      "Start-Process '${WINDOWS_STORE_URI}'" >/dev/null 2>&1; then
      printf 'Microsoft Store 已打开，请在商店中确认安装。\n'
      return 0
    fi
  fi

  if command -v cmd.exe >/dev/null 2>&1; then
    if cmd.exe /c start "" "$WINDOWS_STORE_URI" >/dev/null 2>&1; then
      printf 'Microsoft Store 已打开，请在商店中确认安装。\n'
      return 0
    fi
  fi

  printf '无法启动 Microsoft Store，请打开官方页面：\n  %s\n' "$OFFICIAL_DOWNLOAD_PAGE" >&2
  return 1
}

next_download_path() {
  local dir="$1"
  local candidate="$dir/ChatGPT.dmg"
  local timestamp index

  if [ ! -e "$candidate" ] && [ ! -L "$candidate" ] && [ ! -e "$candidate.part" ]; then
    printf '%s' "$candidate"
    return 0
  fi

  timestamp="$(date '+%Y%m%d-%H%M%S')"
  candidate="$dir/ChatGPT-$timestamp.dmg"
  index=1
  while [ -e "$candidate" ] || [ -L "$candidate" ] || [ -e "$candidate.part" ]; do
    candidate="$dir/ChatGPT-$timestamp-$index.dmg"
    index=$((index + 1))
  done
  printf '%s' "$candidate"
}

download_macos_installer() {
  local target partial

  [ -n "${HOME:-}" ] || fail "无法确定用户主目录。"
  if [ -z "$DOWNLOAD_DIR" ]; then
    DOWNLOAD_DIR="$HOME/Downloads"
  fi

  target="$(next_download_path "$DOWNLOAD_DIR")"
  partial="$target.part"

  printf '检测到 macOS。\n'
  printf '官方安装包：%s\n' "$MAC_DOWNLOAD_URL"
  printf '保存位置：%s\n' "$target"
  if [ "$DRY_RUN" = "1" ]; then
    printf 'Dry run 完成，未下载或打开安装包。\n'
    return 0
  fi

  mkdir -p "$DOWNLOAD_DIR"
  if command -v curl >/dev/null 2>&1; then
    if ! curl -fL --retry 2 --retry-delay 2 --connect-timeout 15 --max-time 1800 \
      --progress-bar -o "$partial" "$MAC_DOWNLOAD_URL"; then
      printf '\n下载失败。官方地址本身不要求 VPN，但当前网络可能无法稳定访问。\n' >&2
      printf '可稍后重试或打开官方页面：%s\n' "$OFFICIAL_DOWNLOAD_PAGE" >&2
      printf '未完成文件保留在：%s\n' "$partial" >&2
      return 1
    fi
  elif command -v wget >/dev/null 2>&1; then
    if ! wget --timeout=20 --tries=3 -O "$partial" "$MAC_DOWNLOAD_URL"; then
      printf '\n下载失败。官方地址本身不要求 VPN，但当前网络可能无法稳定访问。\n' >&2
      printf '可稍后重试或打开官方页面：%s\n' "$OFFICIAL_DOWNLOAD_PAGE" >&2
      printf '未完成文件保留在：%s\n' "$partial" >&2
      return 1
    fi
  else
    fail "未找到 curl 或 wget，无法下载安装包。"
  fi

  if ! hdiutil imageinfo "$partial" >/dev/null 2>&1; then
    printf '下载完成，但文件不是有效的 DMG：%s\n' "$partial" >&2
    printf '请通过官方页面重新下载：%s\n' "$OFFICIAL_DOWNLOAD_PAGE" >&2
    return 1
  fi

  if [ -e "$target" ] || [ -L "$target" ]; then
    printf '目标文件在下载期间已出现，未覆盖该文件：%s\n' "$target" >&2
    printf '已下载文件保留在：%s\n' "$partial" >&2
    return 1
  fi
  mv -n "$partial" "$target"
  if [ -e "$partial" ]; then
    printf '无法将下载文件移动到目标位置，文件保留在：%s\n' "$partial" >&2
    return 1
  fi

  printf '安装包下载并校验完成：%s\n' "$target"
  if open "$target" >/dev/null 2>&1; then
    printf '安装包已打开，请按 macOS 提示完成安装。\n'
  else
    printf '无法自动打开安装包，请手动打开：%s\n' "$target" >&2
    return 1
  fi
}

os_name="$(uname -s 2>/dev/null || printf unknown)"
case "$os_name" in
  Darwin)
    download_macos_installer
    ;;
  MINGW*|MSYS*|CYGWIN*)
    open_windows_store
    ;;
  Linux)
    if is_wsl; then
      open_windows_store
    else
      printf '检测到 Linux；OpenAI 当前未提供 Linux 桌面安装包。\n' >&2
      printf '请使用网页版：%s\n' "$OFFICIAL_DOWNLOAD_PAGE" >&2
      exit 1
    fi
    ;;
  *)
    fail "不支持的操作系统：$os_name"
    ;;
esac
