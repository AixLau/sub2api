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
  grep -Fq "$text" "$file" || fail "expected $file to contain: $text"
}

assert_not_contains() {
  local file="$1"
  local text="$2"
  if grep -Fq "$text" "$file"; then
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

  run_setup "$home_dir" "" --client codex

  assert_file "$home_dir/.codex/config.toml"
  assert_file "$home_dir/.codex/auth.json"
  [ ! -e "$home_dir/.claude/settings.json" ] || fail "Claude settings should not be created for codex client"
  assert_contains "$home_dir/.codex/config.toml" 'model_provider = "sub2api"'
  assert_contains "$home_dir/.codex/config.toml" '[model_providers.sub2api]'
  assert_contains "$home_dir/.codex/config.toml" "base_url = \"$GATEWAY_URL\""
  assert_contains "$home_dir/.codex/config.toml" 'wire_api = "responses"'
  assert_contains "$home_dir/.codex/config.toml" 'requires_openai_auth = true'
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

test_existing_config_preserved_and_backed_up() {
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
  printf '%s\n' '{"OTHER_KEY":"keep","OPENAI_API_KEY":"old"}' >"$home_dir/.codex/auth.json"
  printf '%s\n' '{"env":{"EXISTING":"keep","ANTHROPIC_AUTH_TOKEN":"old"},"permissions":{"defaultMode":"auto"}}' >"$home_dir/.claude/settings.json"

  run_setup "$home_dir" "" --client codex

  assert_contains "$home_dir/.codex/config.toml" 'model = "gpt-5.4"'
  assert_contains "$home_dir/.codex/config.toml" '[mcp_servers.keep]'
  assert_contains "$home_dir/.codex/config.toml" 'command = "keep-me"'
  assert_contains "$home_dir/.codex/config.toml" 'model_provider = "sub2api"'
  assert_json_value "$home_dir/.codex/auth.json" '.OTHER_KEY' "keep"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
  assert_json_value "$home_dir/.claude/settings.json" '.env.ANTHROPIC_AUTH_TOKEN' "old"

  ls "$home_dir/.codex"/config.toml.bak.* >/dev/null 2>&1 || fail "missing Codex config backup"
  ls "$home_dir/.codex"/auth.json.bak.* >/dev/null 2>&1 || fail "missing Codex auth backup"
  if ls "$home_dir/.claude"/settings.json.bak.* >/dev/null 2>&1; then
    fail "Claude settings should not be backed up for codex client"
  fi
}

test_idempotent_managed_block() {
  local home_dir
  home_dir="$(mktemp -d)"

  run_setup "$home_dir" "" --client codex
  run_setup "$home_dir" "" --client codex

  local count
  count="$(grep -Fc '[model_providers.sub2api]' "$home_dir/.codex/config.toml")"
  [ "$count" = "1" ] || fail "expected one managed provider block, got $count"
}

test_no_confirmation_prompt() {
  local home_dir
  home_dir="$(mktemp -d)"

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

  run_setup "$home_dir" ""

  assert_file "$home_dir/.codex/config.toml"
  [ ! -e "$home_dir/.claude/settings.json" ] || fail "Claude settings should not be created when defaulting to Codex"
  assert_contains "$home_dir/output.txt" "默认 Codex"
}

test_prompts_for_api_key_in_chinese() {
  local home_dir
  home_dir="$(mktemp -d)"

  if ! HOME="$home_dir" "$SETUP_SCRIPT" --yes --client codex >"$home_dir/output.txt" 2>&1 <<<"$API_KEY"; then
    printf 'Setup failed. Output:\n' >&2
    sed -n '1,160p' "$home_dir/output.txt" >&2
    exit 1
  fi

  assert_contains "$home_dir/output.txt" "请输入你的 API Key"
  assert_json_value "$home_dir/.codex/auth.json" '.OPENAI_API_KEY' "$API_KEY"
}

test_proxy_direct_rule_can_be_added_to_clash_config() {
  local home_dir
  home_dir="$(mktemp -d)"
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
test_existing_config_preserved_and_backed_up
test_idempotent_managed_block
test_no_confirmation_prompt
test_interactive_choice_selects_claude_only
test_interactive_prompts_for_client_before_api_key
test_interactive_empty_choice_defaults_to_codex
test_prompts_for_api_key_in_chinese
test_proxy_direct_rule_can_be_added_to_clash_config
test_proxy_direct_rule_is_skipped_when_disabled
test_malformed_json_stops_safely

printf 'All client setup tests passed.\n'
