#!/bin/bash
# 把星链API供应商的 GPT-6 / 6.1 / 6.2 系列模型写入 WorkBuddy 自定义模型配置。
# 只用 macOS 自带的 bash、curl 和 osascript（JavaScript for Automation），无需安装任何环境。
#
# 用法：
#   curl -fsSL https://aixlau.me/workbuddy/setup.sh | bash -s -- --key sk-xxx --base https://aixlau.me/v1
#   追加 --dry-run 只预览、不改文件。
# 模型筛选与转换规则必须与 frontend/src/utils/workbuddyConfig.ts 保持一致。
set -euo pipefail

KEY=""
BASE="https://aixlau.me/v1"
DRY_RUN=0
while [ $# -gt 0 ]; do
  case "$1" in
    --key) KEY="${2:-}"; shift 2 ;;
    --base) BASE="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    *) echo "[失败] 不认识的参数：$1" >&2; exit 1 ;;
  esac
done

fail() { echo "[失败] $1" >&2; exit 1; }

KEY="$(printf '%s' "$KEY" | tr -d '[:space:]')"
[ -n "$KEY" ] || fail "缺少 --key（星链API密钥，sk- 开头）"
BASE="${BASE%/}"
case "$BASE" in http://*|https://*) ;; *) fail "--base 必须以 http:// 或 https:// 开头：$BASE" ;; esac
case "$BASE" in */v1) ;; *) BASE="$BASE/v1" ;; esac
command -v osascript >/dev/null 2>&1 || fail "这个脚本只支持 macOS；Windows 请使用 setup.ps1。"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# 1. 读取密钥所在分组的模型目录。密钥通过 stdin 交给 curl，不出现在进程列表里。
STATUS="$(printf 'header = "Authorization: Bearer %s"\n' "$KEY" | curl -sS --max-time 30 -K - \
  -H 'Accept: application/json' -o "$TMP/catalog.json" -w '%{http_code}' \
  "$BASE/models?client_version=0.158.0")" || fail "无法连接 $BASE，请检查网络或 --base 地址。"
case "$STATUS" in
  200) ;;
  401|403) fail "API 密钥无效或没有权限，请到控制台复制完整的密钥（sk- 开头）。" ;;
  *) fail "获取模型列表失败：HTTP $STATUS" ;;
esac

# 2. 转换并合并到 ~/.workbuddy/models.json（JavaScript for Automation，macOS 自带）。
[ -n "${HOME:-}" ] || fail "无法确定用户主目录（HOME 未设置）。"
export WB_KEY="$KEY" WB_BASE="$BASE" WB_DRY_RUN="$DRY_RUN" WB_CATALOG="$TMP/catalog.json" WB_HOME="$HOME"
osascript -l JavaScript <<'JXA'
ObjC.import('Foundation')
const env = $.NSProcessInfo.processInfo.environment
const getenv = (name) => ObjC.unwrap(env.objectForKey(name)) || ''
const fm = $.NSFileManager.defaultManager

const PROVIDER = '星链AI'
const MODEL_PATTERN = /^gpt-6(?:\.[12])?(?:-|$)/i
const DEFAULT_MAX_OUTPUT = 128000
const DEFAULT_CONTEXT = 200000

function out(line) {
  $.NSFileHandle.fileHandleWithStandardOutput.writeData($.NSString.alloc.initWithUTF8String(line + '\n').dataUsingEncoding($.NSUTF8StringEncoding))
}
function fail(message) {
  $.NSFileHandle.fileHandleWithStandardError.writeData($.NSString.alloc.initWithUTF8String('[失败] ' + message + '\n').dataUsingEncoding($.NSUTF8StringEncoding))
  $.exit(1)
}
function readText(path) {
  const value = $.NSString.stringWithContentsOfFileEncodingError(path, $.NSUTF8StringEncoding, null)
  if (value.isNil()) return null
  return ObjC.unwrap(value).replace(/^﻿/, '') // 兼容带 BOM 的旧文件
}
function writeText(path, text) {
  // NSString 写出的 UTF-8 不带 BOM；atomically 先写临时文件再替换。
  const ok = $.NSString.alloc.initWithUTF8String(text).writeToFileAtomicallyEncodingError(path, true, $.NSUTF8StringEncoding, null)
  if (!ok) fail('无法写入 ' + path)
}

const key = getenv('WB_KEY')
const base = getenv('WB_BASE')
const dryRun = getenv('WB_DRY_RUN') === '1'

let catalog
try { catalog = JSON.parse(readText(getenv('WB_CATALOG')) || '') } catch (e) { fail('模型列表不是有效的 JSON，请确认 --base 地址正确。') }
if (!catalog || !Array.isArray(catalog.models)) fail('返回内容不是模型目录，请确认 --base 地址正确（例如 https://aixlau.me/v1）。')

const str = (v) => (typeof v === 'string' ? v.trim() : '')
const positiveInt = (v) => (typeof v === 'number' && isFinite(v) && v > 0 ? Math.floor(v) : 0)

const entries = []
const seen = {}
for (const model of catalog.models) {
  if (!model || typeof model !== 'object') continue
  const id = str(model.slug)
  if (!id || !MODEL_PATTERN.test(id) || seen[id]) continue
  if (str(model.visibility) === 'hide' || model.supported_in_api === false) continue
  seen[id] = true

  const context = positiveInt(model.context_window) || DEFAULT_CONTEXT
  const maxContext = Math.max(positiveInt(model.max_context_window), context)
  const allEfforts = []
  for (const level of Array.isArray(model.supported_reasoning_levels) ? model.supported_reasoning_levels : []) {
    const effort = str(level && level.effort)
    if (effort && allEfforts.indexOf(effort) < 0) allEfforts.push(effort)
  }
  const efforts = allEfforts.filter((e) => e !== 'none')
  let defaultEffort = str(model.default_reasoning_level)
  if (efforts.indexOf(defaultEffort) < 0) defaultEffort = efforts.indexOf('medium') >= 0 ? 'medium' : (efforts[0] || '')
  const modalities = Array.isArray(model.input_modalities) ? model.input_modalities.map(str) : ['text', 'image']

  const entry = {
    apiKey: key,
    contextWindow: { defaultLength: maxContext, supportedLengths: maxContext > context ? [context, maxContext] : [context] },
    id,
    maxInputTokens: maxContext,
    maxOutputTokens: DEFAULT_MAX_OUTPUT,
    name: (str(model.display_name) || id) + ' · ' + PROVIDER,
    supportsImages: modalities.indexOf('image') >= 0,
    supportsReasoning: efforts.length > 0,
    supportsToolCall: true,
    url: base + '/chat/completions',
    vendor: PROVIDER
  }
  if (efforts.length) entry.reasoning = { canDisableThinking: allEfforts.indexOf('none') >= 0, defaultEffort, supportedEfforts: efforts }
  entries.push(entry)
}
if (!entries.length) fail('当前密钥所在分组没有 GPT-6 / 6.1 / 6.2 系列模型，请换用 OpenAI 分组的密钥。')

// 用 bash 传入的 $HOME，与 ~ 的含义一致（NSHomeDirectory 会忽略 HOME 环境变量）。
const home = getenv('WB_HOME')
if (!home) fail('无法确定用户主目录。')
const dir = home + '/.workbuddy'
const path = dir + '/models.json'
let kept = []
if (fm.fileExistsAtPath(path)) {
  const raw = (readText(path) || '').trim()
  if (raw) {
    let existing
    try { existing = JSON.parse(raw) } catch (e) { fail(path + ' 不是有效的 JSON，为避免覆盖你的配置已停止。请先修复或改名该文件后重试。') }
    if (!Array.isArray(existing)) fail(path + ' 的顶层不是数组，不符合 WorkBuddy 的格式，已停止以免覆盖。')
    kept = existing.filter((item) => !(item && (item.vendor === PROVIDER || str(item.url).indexOf(base + '/') === 0)))
  }
}
const content = JSON.stringify(kept.concat(entries), null, 2) + '\n'

if (dryRun) {
  out(content)
  out('[预览] 将写入 ' + path + '：新增/更新 ' + entries.length + ' 个模型，保留其他模型 ' + kept.length + ' 个。')
} else {
  fm.createDirectoryAtPathWithIntermediateDirectoriesAttributesError(dir, true, $(), null)
  let backup = ''
  if (fm.fileExistsAtPath(path)) {
    const now = new Date()
    const pad = (n) => (n < 10 ? '0' : '') + n
    const stamp = '' + now.getFullYear() + pad(now.getMonth() + 1) + pad(now.getDate()) + pad(now.getHours()) + pad(now.getMinutes()) + pad(now.getSeconds())
    backup = path + '.bak.' + stamp
    for (let n = 1; fm.fileExistsAtPath(backup); n++) backup = path + '.bak.' + stamp + '-' + n
    if (!fm.copyItemAtPathToPathError(path, backup, null)) fail('无法备份 ' + path)
  }
  writeText(path, content)
  fm.setAttributesOfItemAtPathError($({ NSFilePosixPermissions: 0o600 }), path, null) // 文件含 API 密钥，仅本人可读

  out('[完成] 已写入 ' + path)
  if (backup) out('       原文件备份：' + backup)
  out('       已添加 ' + entries.length + ' 个模型：' + entries.map((e) => e.id).join('、'))
  if (kept.length) out('       保留其他模型 ' + kept.length + ' 个')
  out('下一步：完全退出并重新打开 WorkBuddy，在模型列表里选择名称带“· ' + PROVIDER + '”的模型。')
}
JXA
