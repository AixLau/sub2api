#!/usr/bin/env bash
#
# Configure local Codex and Claude Code clients for Sub2API.

set -euo pipefail

GATEWAY_URL="https://aixlau.me"
CLAUDE_BASE_URL="$GATEWAY_URL/antigravity"
DEFAULT_CODEX_MODEL="gpt-5.4"
MANAGED_BEGIN="# BEGIN SUB2API MANAGED BLOCK"
MANAGED_END="# END SUB2API MANAGED BLOCK"
YES=0
API_KEY=""
CLIENT=""
PROXY_DIRECT=""

usage() {
  cat <<'USAGE'
用法: setup-sub2api-client.sh [--client codex|claude] [--api-key sk-...] [--proxy-direct yes|no]

只配置一个本机客户端：Codex 或 Claude Code。
USAGE
}

normalize_client() {
  case "$1" in
    1|codex|Codex|CODEX)
      printf 'codex'
      ;;
    2|claude|Claude|CLAUDE|claude-code|Claude-Code|CLAUDE-CODE|cc|CC)
      printf 'claude'
      ;;
    *)
      return 1
      ;;
  esac
}

choose_client() {
  if has_tty; then
    choose_client_tty
    return
  fi

  local choice
  while :; do
    cat <<'PROMPT'
请选择要配置的客户端：
  1) Codex
  2) Claude Code
默认 Codex，↑/↓ 选择，Enter 确认。
PROMPT
    read_line '请输入 1 或 2：' choice
    if [ -z "$choice" ]; then
      CLIENT="codex"
      return 0
    fi
    if CLIENT="$(normalize_client "$choice")"; then
      return 0
    fi
    printf '输入无效，请输入 1 选择 Codex，或输入 2 选择 Claude Code。\n' >&2
  done
}

has_tty() {
  ( : </dev/tty >/dev/tty ) 2>/dev/null
}

choose_client_tty() {
  local selected=0 key rest old_stty

  render_client_menu() {
    printf '\r\033[K请选择要配置的客户端（默认 Codex，↑/↓ 选择，Enter 确认）：\n' >/dev/tty
    if [ "$selected" -eq 0 ]; then
      printf '\033[K> Codex\n\033[K  Claude Code' >/dev/tty
    else
      printf '\033[K  Codex\n\033[K> Claude Code' >/dev/tty
    fi
  }

  render_client_menu
  old_stty="$(stty -g </dev/tty 2>/dev/null || true)"
  stty raw -echo </dev/tty 2>/dev/null || true
  while :; do
    IFS= read -r -n 1 key </dev/tty || key=""
    case "$key" in
      "")
        ;;
      $'\r'|$'\n')
        break
        ;;
      1)
        selected=0
        break
        ;;
      2)
        selected=1
        break
        ;;
      $'\033')
        IFS= read -r -n 2 rest </dev/tty || rest=""
        case "$rest" in
          "[A"|"[B")
            if [ "$selected" -eq 0 ]; then
              selected=1
            else
              selected=0
            fi
            printf '\033[2A' >/dev/tty
            render_client_menu
            ;;
        esac
        ;;
    esac
  done
  if [ -n "$old_stty" ]; then
    stty "$old_stty" </dev/tty 2>/dev/null || true
  else
    stty sane </dev/tty 2>/dev/null || true
  fi
  printf '\n' >/dev/tty

  if [ "$selected" -eq 0 ]; then
    CLIENT="codex"
  else
    CLIENT="claude"
  fi
}

read_line() {
  local prompt="$1"
  local __var="$2"
  local value
  if has_tty; then
    printf '%s' "$prompt" >/dev/tty
    IFS= read -r value </dev/tty
  else
    printf '%s' "$prompt" >&2
    IFS= read -r value
  fi
  printf -v "$__var" '%s' "$value"
}

read_secret() {
  local prompt="$1"
  local __var="$2"
  local value old_stty
  if has_tty; then
    printf '%s' "$prompt" >/dev/tty
    old_stty="$(stty -g </dev/tty 2>/dev/null || true)"
    stty -echo </dev/tty 2>/dev/null || true
    IFS= read -r value </dev/tty
    if [ -n "$old_stty" ]; then
      stty "$old_stty" </dev/tty 2>/dev/null || true
    else
      stty echo </dev/tty 2>/dev/null || true
    fi
    printf '\n' >/dev/tty
  else
    printf '%s' "$prompt" >&2
    IFS= read -r value
  fi
  printf -v "$__var" '%s' "$value"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --client)
      [ "$#" -ge 2 ] || { printf '缺少 --client 参数值。\n' >&2; exit 1; }
      if ! CLIENT="$(normalize_client "$2")"; then
        printf '无效的 --client 参数值：%s。请使用 codex 或 claude。\n' "$2" >&2
        exit 1
      fi
      shift 2
      ;;
    --api-key)
      [ "$#" -ge 2 ] || { printf '缺少 --api-key 参数值。\n' >&2; exit 1; }
      API_KEY="$2"
      shift 2
      ;;
    --yes|-y)
      YES=1
      shift
      ;;
    --proxy-direct)
      [ "$#" -ge 2 ] || { printf '缺少 --proxy-direct 参数值。\n' >&2; exit 1; }
      case "$2" in
        yes|YES|Yes|true|TRUE|True|1)
          PROXY_DIRECT="yes"
          ;;
        no|NO|No|false|FALSE|False|0)
          PROXY_DIRECT="no"
          ;;
        *)
          printf '无效的 --proxy-direct 参数值：%s。请使用 yes 或 no。\n' "$2" >&2
          exit 1
          ;;
      esac
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      printf '未知参数：%s\n' "$1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '%s' "$value"
}

need_jq_for_existing_json() {
  local file="$1"
  [ -f "$file" ] || return 0
  if ! command -v jq >/dev/null 2>&1; then
    printf '检测到已有 JSON 配置，但本机缺少 jq，无法安全合并：%s\n' "$file" >&2
    exit 1
  fi
  if ! jq empty "$file" >/dev/null 2>&1; then
    printf 'JSON 格式无效：%s。请修复该文件或恢复备份后重试。\n' "$file" >&2
    exit 1
  fi
}

backup_file() {
  local file="$1"
  [ -f "$file" ] || return 0

  local ts backup i
  ts="$(date '+%Y%m%d-%H%M%S')"
  backup="$file.bak.$ts"
  i=1
  while [ -e "$backup" ]; do
    backup="$file.bak.$ts.$i"
    i=$((i + 1))
  done
  cp "$file" "$backup"
  BACKUPS="${BACKUPS}${backup}
"
}

write_codex_config() {
  local file="$1"
  local tmp
  tmp="$(mktemp)"

  if [ -f "$file" ]; then
    awk -v begin="$MANAGED_BEGIN" -v end="$MANAGED_END" '
      $0 == begin { skip = 1; next }
      $0 == end { skip = 0; next }
      !skip { print }
    ' "$file" >"$tmp.stripped"

    awk '
      BEGIN { provider_set = 0; in_table = 0 }
      /^\[/ { in_table = 1 }
      !in_table && /^[[:space:]]*model_provider[[:space:]]*=/ {
        if (!provider_set) {
          print "model_provider = \"sub2api\""
          provider_set = 1
        }
        next
      }
      { print }
      END {
        if (!provider_set) {
          print "model_provider = \"sub2api\""
        }
      }
    ' "$tmp.stripped" >"$tmp"
  else
    {
      printf 'model_provider = "sub2api"\n'
      printf 'model = "%s"\n' "$DEFAULT_CODEX_MODEL"
    } >"$tmp"
  fi

  {
    printf '\n%s\n' "$MANAGED_BEGIN"
    printf '[model_providers.sub2api]\n'
    printf 'name = "Sub2API"\n'
    printf 'base_url = "%s"\n' "$GATEWAY_URL"
    printf 'wire_api = "responses"\n'
    printf 'requires_openai_auth = true\n'
    printf '%s\n' "$MANAGED_END"
  } >>"$tmp"

  mv "$tmp" "$file"
  rm -f "$tmp.stripped"
  chmod 600 "$file" 2>/dev/null || true
}

write_codex_auth() {
  local file="$1"
  local tmp
  tmp="$(mktemp)"

  if [ -f "$file" ]; then
    jq --arg key "$API_KEY" '.OPENAI_API_KEY = $key' "$file" >"$tmp"
  elif command -v jq >/dev/null 2>&1; then
    jq -n --arg key "$API_KEY" '{OPENAI_API_KEY: $key}' >"$tmp"
  else
    printf '{\n  "OPENAI_API_KEY": "%s"\n}\n' "$(json_escape "$API_KEY")" >"$tmp"
  fi

  mv "$tmp" "$file"
  chmod 600 "$file" 2>/dev/null || true
}

write_claude_settings() {
  local file="$1"
  local tmp
  tmp="$(mktemp)"

  if [ -f "$file" ]; then
    jq --arg base "$CLAUDE_BASE_URL" --arg token "$API_KEY" '
      if type != "object" then
        error("settings root must be an object")
      elif (.env? != null and (.env | type) != "object") then
        error("settings env must be an object")
      else
        .env = ((.env // {}) + {
          ANTHROPIC_BASE_URL: $base,
          ANTHROPIC_AUTH_TOKEN: $token
        })
      end
    ' "$file" >"$tmp"
  elif command -v jq >/dev/null 2>&1; then
    jq -n --arg base "$CLAUDE_BASE_URL" --arg token "$API_KEY" \
      '{env: {ANTHROPIC_BASE_URL: $base, ANTHROPIC_AUTH_TOKEN: $token}}' >"$tmp"
  else
    printf '{\n  "env": {\n    "ANTHROPIC_BASE_URL": "%s",\n    "ANTHROPIC_AUTH_TOKEN": "%s"\n  }\n}\n' \
      "$(json_escape "$CLAUDE_BASE_URL")" "$(json_escape "$API_KEY")" >"$tmp"
  fi

  mv "$tmp" "$file"
  chmod 600 "$file" 2>/dev/null || true
}

add_proxy_direct_rule() {
  local file="$HOME/.config/clash/config.yaml"
  local rule="  - DOMAIN-SUFFIX,aixlau.me,DIRECT"
  local tmp

  [ "$PROXY_DIRECT" = "yes" ] || return 0
  [ -f "$file" ] || return 0
  if grep -Fq "$rule" "$file"; then
    return 0
  fi

  backup_file "$file"
  tmp="$(mktemp)"
  awk -v rule="$rule" '
    BEGIN { inserted = 0 }
    /^[[:space:]]*rules:[[:space:]]*$/ {
      print
      print rule
      inserted = 1
      next
    }
    { print }
    END {
      if (!inserted) {
        print "rules:"
        print rule
      }
    }
  ' "$file" >"$tmp"
  mv "$tmp" "$file"
}

if [ -z "$CLIENT" ]; then
  choose_client
fi

if [ -z "$API_KEY" ]; then
  read_secret '请输入你的 API Key：' API_KEY
fi

if [ -z "$API_KEY" ]; then
  printf 'API Key 不能为空。\n' >&2
  exit 1
fi

CODEX_DIR="$HOME/.codex"
CLAUDE_DIR="$HOME/.claude"
CODEX_CONFIG="$CODEX_DIR/config.toml"
CODEX_AUTH="$CODEX_DIR/auth.json"
CLAUDE_SETTINGS="$CLAUDE_DIR/settings.json"
BACKUPS=""

if [ "$CLIENT" = "codex" ]; then
  need_jq_for_existing_json "$CODEX_AUTH"
elif [ "$CLIENT" = "claude" ]; then
  need_jq_for_existing_json "$CLAUDE_SETTINGS"
fi

cat <<SUMMARY
Sub2API 客户端配置

已选择客户端：
  $(if [ "$CLIENT" = "codex" ]; then printf 'Codex'; else printf 'Claude Code'; fi)

将写入以下配置文件：
$(if [ "$CLIENT" = "codex" ]; then
  printf '  %s\n  %s\n' "$CODEX_CONFIG" "$CODEX_AUTH"
else
  printf '  %s\n' "$CLAUDE_SETTINGS"
fi)

接口地址：
  $(if [ "$CLIENT" = "codex" ]; then printf '%s' "$GATEWAY_URL"; else printf '%s' "$CLAUDE_BASE_URL"; fi)
SUMMARY

umask 077
if [ "$CLIENT" = "codex" ]; then
  mkdir -p "$CODEX_DIR"
  backup_file "$CODEX_CONFIG"
  backup_file "$CODEX_AUTH"
  write_codex_config "$CODEX_CONFIG"
  write_codex_auth "$CODEX_AUTH"
elif [ "$CLIENT" = "claude" ]; then
  mkdir -p "$CLAUDE_DIR"
  backup_file "$CLAUDE_SETTINGS"
  write_claude_settings "$CLAUDE_SETTINGS"
fi

add_proxy_direct_rule

cat <<DONE

配置已写入。

已配置：
$(if [ "$CLIENT" = "codex" ]; then
  printf '  Codex 配置：%s\n  Codex API Key：%s\n' "$CODEX_CONFIG" "$CODEX_AUTH"
else
  printf '  Claude Code 配置：%s\n' "$CLAUDE_SETTINGS"
fi)

接口地址：
  $(if [ "$CLIENT" = "codex" ]; then printf 'Codex:        %s' "$GATEWAY_URL"; else printf 'Claude Code:  %s' "$CLAUDE_BASE_URL"; fi)
DONE

if [ -n "$BACKUPS" ]; then
  printf '\n备份文件：\n%s' "$BACKUPS"
fi

if [ "$CLIENT" = "codex" ]; then
  printf '\n请重启 Codex 后再测试新配置。\n'
else
  printf '\n请重启 Claude Code 后再测试新配置。\n'
fi
