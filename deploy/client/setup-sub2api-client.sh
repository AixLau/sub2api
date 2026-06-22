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
如需迁移自己的 Codex 官方登录缓存，可设置环境变量：
  SUB2API_CODEX_AUTH_JSON_B64 / SUB2API_CODEX_AUTH_JSON / SUB2API_CODEX_AUTH_FILE
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

  local port redirect_uri create_json setup_id device_code poll_token verify_url callback_file server_pid approved_json setup_token exchange_json key
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
  for i in $(seq 1 120); do
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
  printf '\n已自动获取 API Key，继续写入配置。\n'
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

has_codex_auth_input() {
  [ -n "${SUB2API_CODEX_AUTH_JSON_B64:-}" ] || \
    [ -n "${SUB2API_CODEX_AUTH_JSON:-}" ] || \
    [ -n "${SUB2API_CODEX_AUTH_FILE:-}" ]
}

decode_base64() {
  if base64 --decode >/dev/null 2>&1 <<EOF
dGVzdA==
EOF
  then
    base64 --decode
  else
    base64 -D
  fi
}

write_codex_auth_from_input() {
  local file="$1"
  local tmp

  has_codex_auth_input || return 1
  tmp="$(mktemp)"

  if [ -n "${SUB2API_CODEX_AUTH_JSON_B64:-}" ]; then
    if ! printf '%s' "$SUB2API_CODEX_AUTH_JSON_B64" | decode_base64 >"$tmp"; then
      rm -f "$tmp"
      printf 'SUB2API_CODEX_AUTH_JSON_B64 不是有效的 base64 内容。\n' >&2
      exit 1
    fi
  elif [ -n "${SUB2API_CODEX_AUTH_JSON:-}" ]; then
    printf '%s\n' "$SUB2API_CODEX_AUTH_JSON" >"$tmp"
  elif [ -n "${SUB2API_CODEX_AUTH_FILE:-}" ]; then
    if [ ! -f "$SUB2API_CODEX_AUTH_FILE" ]; then
      rm -f "$tmp"
      printf 'Codex auth 文件不存在：%s\n' "$SUB2API_CODEX_AUTH_FILE" >&2
      exit 1
    fi
    cp "$SUB2API_CODEX_AUTH_FILE" "$tmp"
  fi

  if command -v jq >/dev/null 2>&1; then
    if ! jq empty "$tmp" >/dev/null 2>&1; then
      rm -f "$tmp"
      printf 'Codex auth JSON 格式无效，请检查提供的 auth 内容。\n' >&2
      exit 1
    fi
  fi

  backup_file "$file"
  mv "$tmp" "$file"
  chmod 600 "$file" 2>/dev/null || true
  return 0
}

codex_auth_is_official() {
  local file="$1"
  [ -s "$file" ] || return 1
  if command -v jq >/dev/null 2>&1; then
    jq -e '((.auth_mode? // "") == "chatgpt") or ((.tokens.refresh_token? // "") != "")' "$file" >/dev/null 2>&1
    return
  fi
  grep -Eq '"refresh_token"[[:space:]]*:[[:space:]]*"[^"]+"' "$file" && return 0
  grep -Eq '"auth_mode"[[:space:]]*:[[:space:]]*"chatgpt"' "$file" && return 0
  return 1
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
      printf 'model_reasoning_effort = "high"\n'
      printf 'model_reasoning_summary = "auto"\n'
      printf 'model_verbosity = "medium"\n'
      printf 'disable_response_storage = true\n'
    } >"$tmp"
  fi

  {
    printf '\n%s\n' "$MANAGED_BEGIN"
    printf '[model_providers.sub2api]\n'
    printf 'name = "Sub2API"\n'
    printf 'base_url = "%s"\n' "$GATEWAY_URL"
    printf 'wire_api = "responses"\n'
    printf 'requires_openai_auth = true\n'
    printf 'experimental_bearer_token = "%s"\n' "$(toml_escape "$API_KEY")"
    printf '%s\n' "$MANAGED_END"
  } >>"$tmp"

  mv "$tmp" "$file"
  rm -f "$tmp.stripped"
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
CODEX_AUTH_STATUS="missing"

if [ "$CLIENT" = "codex" ]; then
  if codex_auth_is_official "$CODEX_AUTH"; then
    CODEX_AUTH_STATUS="present"
  elif has_codex_auth_input; then
    CODEX_AUTH_STATUS="will_import"
  else
    CODEX_AUTH_STATUS="missing"
  fi
elif [ "$CLIENT" = "claude" ]; then
  need_jq_for_existing_json "$CLAUDE_SETTINGS"
fi

cat <<SUMMARY
Sub2API 客户端配置

已选择客户端：
  $(if [ "$CLIENT" = "codex" ]; then printf 'Codex'; else printf 'Claude Code'; fi)

将写入以下配置文件：
$(if [ "$CLIENT" = "codex" ]; then
  printf '  %s\n' "$CODEX_CONFIG"
  case "$CODEX_AUTH_STATUS" in
    will_import) printf '  导入 Codex 官方登录缓存：%s\n' "$CODEX_AUTH" ;;
    present) printf '  保留已有 Codex 官方登录缓存：%s\n' "$CODEX_AUTH" ;;
    *) printf '  未提供 Codex 官方登录缓存，仅写入第三方 API 配置。\n' ;;
  esac
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
  write_codex_config "$CODEX_CONFIG"
  if codex_auth_is_official "$CODEX_AUTH"; then
    CODEX_AUTH_STATUS="present"
  elif write_codex_auth_from_input "$CODEX_AUTH"; then
    CODEX_AUTH_STATUS="imported"
  else
    CODEX_AUTH_STATUS="missing"
  fi
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
  printf '  Codex 配置：%s\n' "$CODEX_CONFIG"
  case "$CODEX_AUTH_STATUS" in
    imported) printf '  Codex 官方登录缓存已导入：%s\n' "$CODEX_AUTH" ;;
    present) printf '  Codex 官方登录缓存已保留：%s\n' "$CODEX_AUTH" ;;
    *) printf '  Codex 官方登录缓存：未提供，未写入。\n' ;;
  esac
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
