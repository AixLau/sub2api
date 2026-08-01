#!/usr/bin/env bash
#
# Configure local Codex and Claude Code clients for Sub2API.

set -euo pipefail

GATEWAY_URL="https://aixlau.me"
CLAUDE_BASE_URL="$GATEWAY_URL/antigravity"
DEFAULT_CODEX_MODEL="gpt-5.5"
MANAGED_BEGIN="# BEGIN SUB2API MANAGED BLOCK"
MANAGED_END="# END SUB2API MANAGED BLOCK"
YES=0
API_KEY=""
CLIENT=""
PROXY_DIRECT=""
AUTO_KEY=1

usage() {
  cat <<'USAGE'
用法: setup-sub2api-client.sh [--client codex|claude] [--api-key sk-...] [--proxy-direct yes|no] [--manual-key]

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
      printf '\033[K> Codex\n\033[K  Claude Code\n' >/dev/tty
    else
      printf '\033[K  Codex\n\033[K> Claude Code\n' >/dev/tty
    fi
  }

  render_client_menu
  if [ "${SUB2API_TEST_TTY_KEY+x}" = "x" ]; then
    key="$SUB2API_TEST_TTY_KEY"
    case "$key" in
      ""|$'\r'|$'\n')
        selected=0
        ;;
      1)
        selected=0
        ;;
      2)
        selected=1
        ;;
    esac
    printf '\033[3A\033[J' >/dev/tty
    render_client_menu
    CLIENT="$(if [ "$selected" -eq 0 ]; then printf 'codex'; else printf 'claude'; fi)"
    return
  fi
  old_stty="$(stty -g </dev/tty 2>/dev/null || true)"
  stty raw -echo </dev/tty 2>/dev/null || true
  while :; do
    IFS= read -r -n 1 key </dev/tty || key=""
    case "$key" in
      "")
        break
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
            printf '\033[3A\033[J' >/dev/tty
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
    --manual-key)
      AUTO_KEY=0
      shift
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

json_get_string() {
  local json="$1"
  local key="$2"
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$json" | jq -r --arg key "$key" '.[$key] // .data[$key] // empty'
    return
  fi
  printf '%s' "$json" | sed -nE 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/p' | head -n 1
}

json_get_number() {
  local json="$1"
  local key="$2"
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$json" | jq -r --arg key "$key" \
      '(.[$key] // .data[$key] // empty) | if type == "number" then floor else empty end'
    return
  fi
  printf '%s' "$json" | sed -nE 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*([0-9]+).*/\1/p' | head -n 1
}

toml_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '%s' "$value"
}

open_browser() {
  local url="$1"
  if command -v open >/dev/null 2>&1; then
    open "$url" >/dev/null 2>&1 && return 0
  elif command -v xdg-open >/dev/null 2>&1; then
    xdg-open "$url" >/dev/null 2>&1 && return 0
  fi
  return 1
}

find_callback_port() {
  local port
  for port in 38173 38174 38175 38176 38177 38178 38179; do
    if command -v nc >/dev/null 2>&1; then
      if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
        continue
      fi
    fi
    printf '%s' "$port"
    return 0
  done
  return 1
}

curl_json() {
  curl -fsS --connect-timeout 5 --max-time 10 "$@"
}

try_auto_api_key() {
  [ "$AUTO_KEY" = "1" ] || return 1
  [ -z "$API_KEY" ] || return 1
  has_tty || return 1
  command -v curl >/dev/null 2>&1 || return 1

  local port redirect_uri create_json setup_id device_code poll_token verify_url expires_in wait_seconds callback_file server_pid approved_json setup_token exchange_json key
  port="$(find_callback_port)" || return 1
  redirect_uri="http://127.0.0.1:${port}/callback"

  printf '\n正在准备自动授权，请稍候...\n'
  create_json="$(curl_json -X POST "$GATEWAY_URL/api/v1/client-setup/sessions" \
    -H 'Content-Type: application/json' \
    -d "{\"client\":\"$CLIENT\",\"redirect_uri\":\"$redirect_uri\"}" 2>/dev/null)" || return 1

  setup_id="$(json_get_string "$create_json" "setup_id")"
  device_code="$(json_get_string "$create_json" "device_code")"
  poll_token="$(json_get_string "$create_json" "poll_token")"
  verify_url="$(json_get_string "$create_json" "verify_url")"
  [ -n "$setup_id" ] && [ -n "$device_code" ] && [ -n "$poll_token" ] && [ -n "$verify_url" ] || return 1

  expires_in="$(json_get_number "$create_json" "expires_in")"
  wait_seconds=540
  case "$expires_in" in
    ''|*[!0-9]*)
      ;;
    *)
      if [ "$expires_in" -gt 30 ]; then
        wait_seconds=$((expires_in - 15))
      fi
      ;;
  esac
  [ "$wait_seconds" -le 600 ] || wait_seconds=600
  [ "$wait_seconds" -ge 30 ] || wait_seconds=30

  callback_file="$(mktemp)"
  (
    request_line=""
    while IFS= read -r line; do
      [ -z "$request_line" ] && request_line="$line"
      [ "$line" = $'\r' ] || [ -z "$line" ] && break
    done
    printf 'HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nConnection: close\r\n\r\n授权完成，可以回到终端继续。'
    printf '%s\n' "$request_line" >"$callback_file"
  ) | nc -l 127.0.0.1 "$port" >/dev/null 2>&1 &
  server_pid=$!

  cat <<AUTO

正在打开浏览器完成授权。
如果浏览器没有自动打开，请手动打开：
  $verify_url

页面验证码：
  $device_code
AUTO
  open_browser "$verify_url" || true

  local i callback_line callback_query
  for i in $(seq 1 "$wait_seconds"); do
    if [ -s "$callback_file" ]; then
      callback_line="$(cat "$callback_file")"
      callback_query="${callback_line#* /callback?}"
      callback_query="${callback_query%% HTTP/*}"
      setup_token="$(printf '%s' "$callback_query" | tr '&' '\n' | sed -nE 's/^setup_token=([^& ]+).*/\1/p' | head -n 1)"
      [ -n "$setup_token" ] && break
    fi
    approved_json="$(curl_json "$GATEWAY_URL/api/v1/client-setup/sessions/$setup_id?poll_token=$poll_token" 2>/dev/null || true)"
    setup_token="$(json_get_string "$approved_json" "setup_token")"
    [ -n "$setup_token" ] && break
    sleep 1
  done
  kill "$server_pid" >/dev/null 2>&1 || true
  rm -f "$callback_file"

  [ -n "${setup_token:-}" ] || return 1
  exchange_json="$(curl_json -X POST "$GATEWAY_URL/api/v1/client-setup/exchange" \
    -H 'Content-Type: application/json' \
    -d "{\"setup_id\":\"$setup_id\",\"setup_token\":\"$setup_token\"}" 2>/dev/null)" || return 1
  key="$(json_get_string "$exchange_json" "api_key")"
  [ -n "$key" ] || return 1
  API_KEY="$key"
  printf '\n授权完成，正在写入配置...\n'
  return 0
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

write_codex_api_key_auth() {
  local file="$1"
  local tmp
  tmp="$(mktemp)"

  printf '{\n  "auth_mode": "apikey",\n  "OPENAI_API_KEY": "%s"\n}\n' "$(json_escape "$API_KEY")" >"$tmp"
  backup_file "$file"
  mv "$tmp" "$file"
  chmod 600 "$file" 2>/dev/null || true
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

  {
    printf '# Sub2API Codex config generated on 2026-07-12.\n'
    printf 'model_provider = "xinglian"\n'
    printf 'model = "%s"\n' "$DEFAULT_CODEX_MODEL"
    printf 'model_reasoning_effort = "high"\n'
    printf 'model_reasoning_summary = "auto"\n'
    printf 'model_verbosity = "medium"\n'
    printf 'disable_response_storage = true\n'
    printf 'preferred_auth_method = "apikey"\n'
    printf '\n'
    printf '[model_providers.xinglian]\n'
    printf 'name = "星链 AI"\n'
    printf 'base_url = "%s"\n' "$GATEWAY_URL"
    printf 'wire_api = "responses"\n'
    printf 'requires_openai_auth = true\n'
  } >"$tmp"

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
  if ! try_auto_api_key; then
    printf '\n自动授权未完成，改为手动输入。\n'
    read_secret '请输入你的 API Key：' API_KEY
  fi
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
  :
elif [ "$CLIENT" = "claude" ]; then
  need_jq_for_existing_json "$CLAUDE_SETTINGS"
fi

if [ "$CLIENT" = "codex" ]; then
  CLIENT_LABEL="Codex"
else
  CLIENT_LABEL="Claude Code"
fi

printf '\n正在配置 %s，请稍候...\n' "$CLIENT_LABEL"

umask 077
if [ "$CLIENT" = "codex" ]; then
  mkdir -p "$CODEX_DIR"
  backup_file "$CODEX_CONFIG"
  write_codex_config "$CODEX_CONFIG"
  write_codex_api_key_auth "$CODEX_AUTH"
elif [ "$CLIENT" = "claude" ]; then
  mkdir -p "$CLAUDE_DIR"
  backup_file "$CLAUDE_SETTINGS"
  write_claude_settings "$CLAUDE_SETTINGS"
fi

add_proxy_direct_rule

printf '\n配置完成。\n'

if [ -n "$BACKUPS" ]; then
  printf '\n备份文件：\n%s' "$BACKUPS"
fi

if [ "$CLIENT" = "codex" ]; then
  printf '\n请重启 Codex 后再测试。\n'
else
  printf '\n请重启 Claude Code 后再测试。\n'
fi
