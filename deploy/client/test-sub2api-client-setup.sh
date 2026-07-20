#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SETUP_SCRIPT="$SCRIPT_DIR/setup-sub2api-client.sh"
GATEWAY_URL="https://aixlau.me"
CLAUDE_URL="https://aixlau.me/antigravity"
API_KEY="sk-test-sub2api"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

[ -f "$SETUP_SCRIPT" ] || fail "missing setup script: $SETUP_SCRIPT"

assert_file() {
  [ -f "$1" ] || fail "missing file: $1"
}

assert_contains() {
  local file="$1"
  local text="$2"
  grep -Fq -- "$text" "$file" || fail "expected $file to contain: $text"
}

assert_not_contains() {
  local file="$1"
  local text="$2"
  if grep -Fq -- "$text" "$file"; then
    fail "expected $file not to contain: $text"
  fi
}

assert_json_value() {
  local file="$1"
  local filter="$2"
  local want="$3"
  local got
  got="$(jq -r "$filter" "$file")"
  [ "$got" = "$want" ] || fail "expected $file $filter to be $want, got $got"
}

run_setup() {
  local home_dir="$1"
  local input="$2"
  shift 2
  if ! HOME="$home_dir" "$SETUP_SCRIPT" --yes --api-key "$API_KEY" "$@" >"$home_dir/output.txt" 2>&1 <<<"$input"; then
    printf 'Setup failed. Output:\n' >&2
    sed -n '1,160p' "$home_dir/output.txt" >&2
    exit 1
  fi
}

run_setup_with_codex_auth_b64() {
  local home_dir="$1"
  local input="$2"
  local auth_b64="$3"
  shift 3
  if ! env HOME="$home_dir" SUB2API_CODEX_AUTH_JSON_B64="$auth_b64" "$SETUP_SCRIPT" --yes --api-key "$API_KEY" "$@" >"$home_dir/output.txt" 2>&1 <<<"$input"; then
    printf 'Setup failed. Output:\n' >&2
    sed -n '1,160p' "$home_dir/output.txt" >&2
    exit 1
  fi
}

prepare_codex_official_auth() {
  local home_dir="$1"
  mkdir -p "$home_dir/.codex"
  printf '%s\n' '{"OPENAI_API_KEY":null,"auth_mode":"chatgpt","tokens":{"refresh_token":"official-cache"}}' >"$home_dir/.codex/auth.json"
}

test_help_is_chinese() {
  local home_dir
  home_dir="$(mktemp -d)"

  HOME="$home_dir" "$SETUP_SCRIPT" --help >"$home_dir/output.txt" 2>&1

  assert_contains "$home_dir/output.txt" "用法"
  assert_contains "$home_dir/output.txt" "只配置一个本机客户端"
}

test_codex_config_creation_only() {
  local home_dir
  home_dir="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"

  run_setup "$home_dir" "" --client codex

  assert_file "$home_dir/.codex/config.toml"
  assert_file "$home_dir/.codex/auth.json"
  [ ! -e "$home_dir/.claude/settings.json" ] || fail "Claude settings should not be created for codex client"
  assert_contains "$home_dir/.codex/config.toml" 'model_provider = "xinglian"'
  assert_contains "$home_dir/.codex/config.toml" '[model_providers.xinglian]'
  assert_contains "$home_dir/.codex/config.toml" 'name = "星链 AI"'
  assert_contains "$home_dir/.codex/config.toml" "base_url = \"$GATEWAY_URL\""
  assert_contains "$home_dir/.codex/config.toml" 'wire_api = "responses"'
  assert_contains "$home_dir/.codex/config.toml" 'requires_openai_auth = true'
  assert_contains "$home_dir/.codex/config.toml" 'preferred_auth_method = "apikey"'
  assert_not_contains "$home_dir/.codex/config.toml" "experimental_bearer_token"
  assert_json_value "$home_dir/.codex/auth.json" '.auth_mode' "apikey"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
}

test_claude_config_creation_only() {
  local home_dir
  home_dir="$(mktemp -d)"

  run_setup "$home_dir" "" --client claude

  [ ! -e "$home_dir/.codex/config.toml" ] || fail "Codex config should not be created for claude client"
  [ ! -e "$home_dir/.codex/auth.json" ] || fail "Codex auth should not be created for claude client"
  assert_file "$home_dir/.claude/settings.json"
  assert_json_value "$home_dir/.claude/settings.json" '.env.ANTHROPIC_BASE_URL' "$CLAUDE_URL"
  assert_json_value "$home_dir/.claude/settings.json" '.env.ANTHROPIC_AUTH_TOKEN' "$API_KEY"
}

test_existing_codex_files_are_backed_up_and_replaced() {
  local home_dir
  home_dir="$(mktemp -d)"
  mkdir -p "$home_dir/.codex" "$home_dir/.claude"
  printf '%s\n' \
    'model = "gpt-5.4"' \
    'model_provider = "old"' \
    '' \
    '[mcp_servers.keep]' \
    'command = "keep-me"' \
    >"$home_dir/.codex/config.toml"
  printf '%s\n' '{"OPENAI_API_KEY":null,"auth_mode":"chatgpt","tokens":{"refresh_token":"official-cache"},"OTHER_KEY":"keep"}' >"$home_dir/.codex/auth.json"
  printf '%s\n' '{"env":{"EXISTING":"keep","ANTHROPIC_AUTH_TOKEN":"old"},"permissions":{"defaultMode":"auto"}}' >"$home_dir/.claude/settings.json"

  run_setup "$home_dir" "" --client codex

  assert_not_contains "$home_dir/.codex/config.toml" 'model = "gpt-5.4"'
  assert_not_contains "$home_dir/.codex/config.toml" '[mcp_servers.keep]'
  assert_not_contains "$home_dir/.codex/config.toml" 'command = "keep-me"'
  assert_contains "$home_dir/.codex/config.toml" 'model_provider = "xinglian"'
  assert_not_contains "$home_dir/.codex/config.toml" "experimental_bearer_token"
  assert_json_value "$home_dir/.codex/auth.json" '.auth_mode' "apikey"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
  [ "$(jq -r 'has("OTHER_KEY")' "$home_dir/.codex/auth.json")" = "false" ] || fail "Codex auth should be replaced, not merged"
  assert_json_value "$home_dir/.claude/settings.json" '.env.ANTHROPIC_AUTH_TOKEN' "old"

  ls "$home_dir/.codex"/config.toml.bak.* >/dev/null 2>&1 || fail "missing Codex config backup"
  ls "$home_dir/.codex"/auth.json.bak.* >/dev/null 2>&1 || fail "missing Codex auth backup"
  if ls "$home_dir/.claude"/settings.json.bak.* >/dev/null 2>&1; then
    fail "Claude settings should not be backed up for codex client"
  fi
}

test_existing_config_without_provider_is_replaced() {
  local home_dir
  home_dir="$(mktemp -d)"
  mkdir -p "$home_dir/.codex"
  prepare_codex_official_auth "$home_dir"
  printf '%s\n' \
    '[mcp_servers.keep]' \
    'command = "keep-me"' \
    >"$home_dir/.codex/config.toml"

  run_setup "$home_dir" "" --client codex

  local first_line
  first_line="$(sed -n '1p' "$home_dir/.codex/config.toml")"
  [ "$first_line" = '# Sub2API Codex config generated on 2026-07-12.' ] || fail "Codex config should be replaced with generated config"
  assert_not_contains "$home_dir/.codex/config.toml" '[mcp_servers.keep]'
  assert_not_contains "$home_dir/.codex/config.toml" 'command = "keep-me"'
}

test_idempotent_managed_block() {
  local home_dir
  home_dir="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"

  run_setup "$home_dir" "" --client codex
  run_setup "$home_dir" "" --client codex

  local count
  count="$(grep -Fc '[model_providers.xinglian]' "$home_dir/.codex/config.toml")"
  [ "$count" = "1" ] || fail "expected one managed provider block, got $count"
}

test_no_confirmation_prompt() {
  local home_dir
  home_dir="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"

  HOME="$home_dir" "$SETUP_SCRIPT" --client codex --api-key "$API_KEY" >"$home_dir/output.txt" 2>&1

  assert_file "$home_dir/.codex/config.toml"
  assert_not_contains "$home_dir/output.txt" "输入 yes"
  assert_not_contains "$home_dir/output.txt" "已取消"
}

test_interactive_choice_selects_claude_only() {
  local home_dir
  home_dir="$(mktemp -d)"

  run_setup "$home_dir" "2"

  [ ! -e "$home_dir/.codex/config.toml" ] || fail "Codex config should not be created after selecting Claude"
  assert_file "$home_dir/.claude/settings.json"
  assert_contains "$home_dir/output.txt" "请选择要配置的客户端"
  assert_contains "$home_dir/output.txt" "↑/↓ 选择，Enter 确认"
  assert_contains "$home_dir/output.txt" "Claude Code"
  assert_json_value "$home_dir/.claude/settings.json" '.env.ANTHROPIC_BASE_URL' "$CLAUDE_URL"
}

test_interactive_prompts_for_client_before_api_key() {
  local home_dir
  home_dir="$(mktemp -d)"

  if ! HOME="$home_dir" "$SETUP_SCRIPT" --yes >"$home_dir/output.txt" 2>&1 <<EOF
2
$API_KEY
EOF
  then
    printf 'Setup failed. Output:\n' >&2
    sed -n '1,160p' "$home_dir/output.txt" >&2
    exit 1
  fi

  [ ! -e "$home_dir/.codex/config.toml" ] || fail "Codex config should not be created after selecting Claude first"
  assert_file "$home_dir/.claude/settings.json"
  assert_json_value "$home_dir/.claude/settings.json" '.env.ANTHROPIC_AUTH_TOKEN' "$API_KEY"
}

test_interactive_empty_choice_defaults_to_codex() {
	local home_dir
	home_dir="$(mktemp -d)"
	prepare_codex_official_auth "$home_dir"

  run_setup "$home_dir" ""

  assert_file "$home_dir/.codex/config.toml"
  [ ! -e "$home_dir/.claude/settings.json" ] || fail "Claude settings should not be created when defaulting to Codex"
	assert_contains "$home_dir/output.txt" "默认 Codex"
}

test_tty_empty_key_defaults_to_codex() {
	local home_dir
	home_dir="$(mktemp -d)"
	prepare_codex_official_auth "$home_dir"

	if ! command -v script >/dev/null 2>&1; then
		return 0
	fi

	if ! script -q /dev/null env HOME="$home_dir" SUB2API_TEST_TTY_KEY= "$SETUP_SCRIPT" --yes --api-key "$API_KEY" >"$home_dir/output.txt" 2>&1; then
		printf 'Setup failed. Output:\n' >&2
		sed -n '1,160p' "$home_dir/output.txt" >&2
		exit 1
	fi

	assert_file "$home_dir/.codex/config.toml"
	[ ! -e "$home_dir/.claude/settings.json" ] || fail "Claude settings should not be created when TTY Enter defaults to Codex"
	assert_contains "$home_dir/output.txt" "默认 Codex"
}

test_tty_auto_key_fallback_is_visible_and_timeout_bound() {
  local home_dir fakebin
  home_dir="$(mktemp -d)"
  fakebin="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"

  if ! command -v script >/dev/null 2>&1; then
    return 0
  fi

  cat >"$fakebin/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$SUB2API_FAKE_CURL_ARGS"
exit 22
SH
  chmod +x "$fakebin/curl"

  if ! printf '\r%s\n' "$API_KEY" | script -q /dev/null env HOME="$home_dir" PATH="$fakebin:$PATH" SUB2API_FAKE_CURL_ARGS="$home_dir/curl-args.txt" "$SETUP_SCRIPT" --yes >"$home_dir/output.txt" 2>&1; then
    printf 'Setup failed. Output:\n' >&2
    sed -n '1,160p' "$home_dir/output.txt" >&2
    exit 1
  fi

  assert_file "$home_dir/.codex/config.toml"
  assert_contains "$home_dir/output.txt" "正在准备自动授权"
  assert_contains "$home_dir/output.txt" "自动授权未完成，改为手动输入"
  assert_contains "$home_dir/curl-args.txt" "--connect-timeout 5"
  assert_contains "$home_dir/curl-args.txt" "--max-time 10"
}

test_tty_auto_key_accepts_api_envelope() {
  local home_dir fakebin
  home_dir="$(mktemp -d)"
  fakebin="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"

  if ! command -v script >/dev/null 2>&1; then
    return 0
  fi

  cat >"$fakebin/curl" <<'SH'
#!/usr/bin/env bash
args="$*"
printf '%s\n' "$args" >>"$SUB2API_FAKE_CURL_ARGS"
if [[ "$args" == *"/api/v1/client-setup/sessions"* && "$args" == *"-X POST"* ]]; then
  printf '%s\n' '{"code":0,"message":"success","data":{"client":"codex","device_code":"ABCD-1234","expires_in":600,"poll_token":"poll-123","redirect_uri":"http://127.0.0.1:38173/callback","setup_id":"setup-123","status":"pending","verify_url":"https://aixlau.me/client-setup?client=codex&device_code=ABCD-1234&setup_id=setup-123"}}'
elif [[ "$args" == *"/api/v1/client-setup/sessions/setup-123?poll_token=poll-123"* ]]; then
  printf '%s\n' '{"code":0,"message":"success","data":{"setup_token":"setup-token-123","status":"approved"}}'
elif [[ "$args" == *"/api/v1/client-setup/exchange"* ]]; then
  printf '%s\n' '{"code":0,"message":"success","data":{"api_key":"sk-auto-envelope-test"}}'
else
  exit 22
fi
SH
  cat >"$fakebin/nc" <<'SH'
#!/usr/bin/env bash
if [ "${1:-}" = "-z" ]; then
  exit 1
fi
sleep 10
SH
  cat >"$fakebin/open" <<'SH'
#!/usr/bin/env bash
exit 0
SH
  chmod +x "$fakebin/curl" "$fakebin/nc" "$fakebin/open"

  if ! printf '\r' | script -q /dev/null env HOME="$home_dir" PATH="$fakebin:$PATH" SUB2API_FAKE_CURL_ARGS="$home_dir/curl-args.txt" "$SETUP_SCRIPT" --yes >"$home_dir/output.txt" 2>&1; then
    printf 'Setup failed. Output:\n' >&2
    sed -n '1,200p' "$home_dir/output.txt" >&2
    exit 1
  fi

  assert_file "$home_dir/.codex/config.toml"
  assert_contains "$home_dir/output.txt" "正在打开浏览器完成授权"
  assert_contains "$home_dir/output.txt" "页面验证码"
  assert_contains "$home_dir/output.txt" "授权完成，正在写入配置"
  assert_not_contains "$home_dir/output.txt" "请输入你的 API Key"
  assert_not_contains "$home_dir/.codex/config.toml" "experimental_bearer_token"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "sk-auto-envelope-test"
}

test_prompts_for_api_key_in_chinese() {
	local home_dir
	home_dir="$(mktemp -d)"
	prepare_codex_official_auth "$home_dir"

  if ! HOME="$home_dir" "$SETUP_SCRIPT" --yes --client codex >"$home_dir/output.txt" 2>&1 <<<"$API_KEY"; then
    printf 'Setup failed. Output:\n' >&2
    sed -n '1,160p' "$home_dir/output.txt" >&2
    exit 1
  fi

  assert_contains "$home_dir/output.txt" "请输入你的 API Key"
  assert_not_contains "$home_dir/.codex/config.toml" "experimental_bearer_token"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
}

test_success_output_is_simple_and_does_not_leak_shell_fragments() {
  local home_dir
  home_dir="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"

  run_setup "$home_dir" "" --client codex

  assert_contains "$home_dir/output.txt" "配置完成"
  assert_contains "$home_dir/output.txt" "请重启 Codex 后再测试。"
  assert_not_contains "$home_dir/output.txt" "command substitution"
  assert_not_contains "$home_dir/output.txt" "will_import)"
  assert_not_contains "$home_dir/output.txt" "imported)"
  assert_not_contains "$home_dir/output.txt" "官方登录缓存"
  assert_not_contains "$home_dir/output.txt" "API Key"
  assert_not_contains "$home_dir/output.txt" "将写入以下配置文件"
  assert_not_contains "$home_dir/output.txt" "$API_KEY"
}

test_codex_without_official_login_cache_creates_api_key_auth() {
  local home_dir
  home_dir="$(mktemp -d)"

  run_setup "$home_dir" "" --client codex

  assert_file "$home_dir/.codex/config.toml"
  assert_file "$home_dir/.codex/auth.json"
  assert_json_value "$home_dir/.codex/auth.json" '.auth_mode' "apikey"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
  assert_not_contains "$home_dir/.codex/config.toml" "experimental_bearer_token"
  assert_not_contains "$home_dir/output.txt" "官方登录缓存"
}

test_codex_existing_api_key_auth_is_replaced() {
  local home_dir
  home_dir="$(mktemp -d)"
  mkdir -p "$home_dir/.codex"
  printf '%s\n' '{"OPENAI_API_KEY":"sk-old-key"}' >"$home_dir/.codex/auth.json"

  run_setup "$home_dir" "" --client codex

  assert_json_value "$home_dir/.codex/auth.json" '.auth_mode' "apikey"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
  assert_not_contains "$home_dir/.codex/config.toml" "experimental_bearer_token"
  ls "$home_dir/.codex"/auth.json.bak.* >/dev/null 2>&1 || fail "missing Codex auth backup"
}

test_codex_replaces_existing_official_login_cache() {
  local home_dir
  home_dir="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"

  run_setup "$home_dir" "" --client codex

  assert_json_value "$home_dir/.codex/auth.json" '.auth_mode' "apikey"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
  ls "$home_dir/.codex"/auth.json.bak.* >/dev/null 2>&1 || fail "missing Codex auth backup"
  assert_not_contains "$home_dir/output.txt" "官方登录缓存"
}

test_codex_auth_input_env_is_ignored_and_api_key_auth_is_written() {
  local home_dir auth_json auth_b64
  home_dir="$(mktemp -d)"
  auth_json='{"OPENAI_API_KEY":null,"auth_mode":"chatgpt","tokens":{"refresh_token":"private-cache"}}'
  auth_b64="$(printf '%s' "$auth_json" | base64 | tr -d '\n')"

  run_setup_with_codex_auth_b64 "$home_dir" "" "$auth_b64" --client codex

  assert_file "$home_dir/.codex/config.toml"
  assert_file "$home_dir/.codex/auth.json"
  assert_not_contains "$home_dir/.codex/auth.json" '"private-cache"'
  assert_json_value "$home_dir/.codex/auth.json" '.auth_mode' "apikey"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
  assert_not_contains "$home_dir/.codex/config.toml" "experimental_bearer_token"
  assert_not_contains "$home_dir/output.txt" "官方登录缓存"
}

test_proxy_direct_rule_can_be_added_to_clash_config() {
  local home_dir
  home_dir="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"
  mkdir -p "$home_dir/.config/clash"
  cat >"$home_dir/.config/clash/config.yaml" <<'YAML'
mixed-port: 7890
rules:
  - MATCH,Proxy
YAML

  run_setup "$home_dir" "" --client codex --proxy-direct yes

  assert_contains "$home_dir/.config/clash/config.yaml" "  - DOMAIN-SUFFIX,aixlau.me,DIRECT"
  assert_contains "$home_dir/.config/clash/config.yaml" "  - MATCH,Proxy"
  ls "$home_dir/.config/clash"/config.yaml.bak.* >/dev/null 2>&1 || fail "missing Clash config backup"
}

test_proxy_direct_rule_is_skipped_when_disabled() {
  local home_dir
  home_dir="$(mktemp -d)"
  prepare_codex_official_auth "$home_dir"
  mkdir -p "$home_dir/.config/clash"
  cat >"$home_dir/.config/clash/config.yaml" <<'YAML'
mixed-port: 7890
rules:
  - MATCH,Proxy
YAML

  run_setup "$home_dir" "" --client codex --proxy-direct no

  assert_not_contains "$home_dir/.config/clash/config.yaml" "DOMAIN-SUFFIX,aixlau.me,DIRECT"
}

test_malformed_json_stops_safely() {
  local home_dir
  home_dir="$(mktemp -d)"
  mkdir -p "$home_dir/.claude"
  printf '{bad json\n' >"$home_dir/.claude/settings.json"

  if HOME="$home_dir" "$SETUP_SCRIPT" --yes --client claude --api-key "$API_KEY" >"$home_dir/output.txt" 2>&1; then
    fail "setup should fail on malformed existing Claude settings"
  fi

  assert_contains "$home_dir/.claude/settings.json" "{bad json"
  assert_contains "$home_dir/output.txt" "JSON 格式无效"
}

test_help_is_chinese
test_codex_config_creation_only
test_claude_config_creation_only
test_existing_codex_files_are_backed_up_and_replaced
test_existing_config_without_provider_is_replaced
test_idempotent_managed_block
test_no_confirmation_prompt
test_interactive_choice_selects_claude_only
test_interactive_prompts_for_client_before_api_key
test_interactive_empty_choice_defaults_to_codex
test_tty_empty_key_defaults_to_codex
test_tty_auto_key_fallback_is_visible_and_timeout_bound
test_tty_auto_key_accepts_api_envelope
test_prompts_for_api_key_in_chinese
test_success_output_is_simple_and_does_not_leak_shell_fragments
test_codex_without_official_login_cache_creates_api_key_auth
test_codex_existing_api_key_auth_is_replaced
test_codex_replaces_existing_official_login_cache
test_codex_auth_input_env_is_ignored_and_api_key_auth_is_written
test_proxy_direct_rule_can_be_added_to_clash_config
test_proxy_direct_rule_is_skipped_when_disabled
test_malformed_json_stops_safely

printf 'All client setup tests passed.\n'
