import type { GroupPlatform } from '@/types'
import {
  buildClaudeCodeEnv,
  codexLongContextTomlLines
} from '@/utils/claudeCodeConfig'
import { PROVIDER_DISPLAY_NAME, PROVIDER_ICON_URL } from '@/utils/providerBrand'

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-6.1-sol'
export const GROK_CC_SWITCH_MODEL = 'grok-4.5'

export type CcSwitchClientType = 'claude' | 'gemini'

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
  /** Provider config CC Switch merges into the imported provider. */
  config?: string
  configFormat?: 'json' | 'toml'
}

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType: CcSwitchClientType
  providerName: string
  apiKey: string
  usageScript: string
}

/**
 * Balance query CC Switch runs against the imported provider: the Sub2API
 * template from CC Switch's deeplink integration guide (identical to the
 * in-app template since v4.0.10). `{{baseUrl}}` is the provider address as
 * stored — Codex and Grok imports carry `/v1`, Claude ones do not — so the URL
 * strips an existing `/v1` before appending `/v1/usage`. Unlimited
 * subscription groups report `remaining: -1`, which is hidden rather than shown.
 */
export const CC_SWITCH_USAGE_SCRIPT = `({
  request: {
    url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1$/, "") + "/v1/usage",
    method: "GET",
    headers: {
      "Authorization": "Bearer {{apiKey}}",
      "User-Agent": "cc-switch/1.0"
    }
  },
  extractor: function (response) {
    var quota = response.quota || {};
    var result = {
      isValid: response.isValid !== false,
      unit: response.unit || quota.unit || "USD"
    };
    if (typeof response.planName === "string") result.planName = response.planName;
    if (typeof response.remaining === "number" && response.remaining >= 0) {
      result.remaining = response.remaining;
    }
    if (quota.limit > 0) {
      result.used = quota.used;
      result.total = quota.limit;
    }
    if (!result.isValid) {
      result.invalidMessage = response.status
        ? String(response.status)
        : "查询失败";
    }
    return result;
  }
})`

function withV1Endpoint(baseUrl: string): string {
  const normalizedBaseUrl = baseUrl.replace(/\/+$/, '')
  return normalizedBaseUrl.endsWith('/v1') ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`
}

function escapeTomlBasicString(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

/**
 * Codex config.toml for the CC Switch import. CC Switch treats it as untrusted
 * and keeps only a whitelist (src-tauri/src/deeplink/codex_link.rs): the
 * provider id is always rewritten to `custom`, and global tables such as
 * `[desktop]` or `[features]` are dropped, so only provider-scoped keys go here.
 */
function buildCodexImportToml(endpoint: string): string {
  return `model_provider = "sub2api"
model = "${OPENAI_CC_SWITCH_CODEX_MODEL}"
review_model = "${OPENAI_CC_SWITCH_CODEX_MODEL}"
model_reasoning_effort = "high"
${codexLongContextTomlLines()}
[model_providers.sub2api]
name = "${escapeTomlBasicString(PROVIDER_DISPLAY_NAME)}"
base_url = "${escapeTomlBasicString(endpoint)}"
wire_api = "responses"
requires_openai_auth = true
`
}

/**
 * Claude Code settings.json fragment with the 1M context and compaction
 * defaults. Model aliases stay unset: for genuine Claude models CC Switch's
 * guide says to leave them empty so Claude Code uses its own model list.
 */
function buildClaudeImportJson(endpoint: string, apiKey: string): string {
  return JSON.stringify({ env: buildClaudeCodeEnv(endpoint, apiKey) })
}

/** UTF-8 safe base64 (btoa alone throws on non-Latin-1 characters). */
function encodeBase64(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

export function resolveCcSwitchImportConfig(
  platform: GroupPlatform | undefined | null,
  clientType: CcSwitchClientType,
  baseUrl: string
): CcSwitchImportConfig {
  switch (platform || 'anthropic') {
    case 'antigravity':
      return {
        app: clientType === 'gemini' ? 'gemini' : 'claude',
        endpoint: `${baseUrl.replace(/\/+$/, '')}/antigravity`
      }
    case 'openai':
      return {
        app: 'codex',
        // CC Switch writes this verbatim as base_url; Codex appends /responses,
        // so OpenAI-format providers carry /v1 (per CC Switch's integration guide).
        endpoint: withV1Endpoint(baseUrl),
        model: OPENAI_CC_SWITCH_CODEX_MODEL
      }
    case 'gemini':
      return {
        app: 'gemini',
        endpoint: baseUrl
      }
    case 'grok':
      return {
        app: 'grokbuild',
        endpoint: withV1Endpoint(baseUrl),
        model: GROK_CC_SWITCH_MODEL
      }
    default:
      return {
        app: 'claude',
        endpoint: baseUrl
      }
  }
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const config = resolveCcSwitchImportConfig(input.platform, input.clientType, input.baseUrl)
  if (config.app === 'codex') {
    config.config = buildCodexImportToml(config.endpoint)
    config.configFormat = 'toml'
  }
  if (config.app === 'claude') {
    config.config = buildClaudeImportJson(config.endpoint, input.apiKey)
    config.configFormat = 'json'
  }
  const entries: [string, string][] = [
    ['resource', 'provider'],
    ['app', config.app],
    ['name', input.providerName],
    ['homepage', input.baseUrl],
    ['endpoint', config.endpoint],
    ['iconUrl', PROVIDER_ICON_URL],
    ['apiKey', input.apiKey],
    ['configFormat', config.configFormat || 'json'],
    ['usageEnabled', 'true'],
    ['usageScript', encodeBase64(input.usageScript)],
    ['usageAutoInterval', '30']
  ]

  if (config.model) {
    entries.splice(2, 0, ['model', config.model])
  }

  if (config.config) {
    entries.push(['config', encodeBase64(config.config)])
  }

  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}
