import type { GroupPlatform } from '@/types'
import { buildClaudeCodeEnv, codexLongContextTomlLines } from '@/utils/claudeCodeConfig'

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-5.5'
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
 * Balance query CC Switch runs against the imported provider. CC Switch fills
 * `{{baseUrl}}` with the provider's base URL as stored — Codex and Grok imports
 * carry a trailing `/v1` (see `withV1Endpoint`), Claude ones do not, and users
 * may edit it either way afterwards — then evaluates the script, so the URL
 * strips an existing `/v1` instead of blindly appending one (`/v1/v1/usage`
 * is a 404 and CC Switch shows "query failed").
 */
export const CC_SWITCH_USAGE_SCRIPT = `({
    request: {
      url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1$/, "") + "/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      return {
        isValid: response?.is_active ?? response?.isValid ?? true,
        remaining,
        unit
      };
    }
  })`

function withV1Endpoint(baseUrl: string): string {
  const normalizedBaseUrl = baseUrl.replace(/\/+$/, '')
  return normalizedBaseUrl.endsWith('/v1') ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`
}

function withoutTrailingSlashes(baseUrl: string): string {
  return baseUrl.replace(/\/+$/, '')
}

function escapeTomlBasicString(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

/** Codex config.toml with the 1M context window and auto-compaction threshold. */
function buildCodexImportToml(endpoint: string): string {
  return `model_provider = "sub2api"
model = "${OPENAI_CC_SWITCH_CODEX_MODEL}"
${codexLongContextTomlLines()}disable_response_storage = true

[model_providers.sub2api]
name = "sub2api"
base_url = "${escapeTomlBasicString(endpoint)}"
wire_api = "responses"
requires_openai_auth = true
`
}

/** Claude Code settings.json fragment with the 1M context and compaction defaults. */
function buildClaudeImportJson(endpoint: string, apiKey: string, pinClaudeModels: boolean): string {
  return JSON.stringify({ env: buildClaudeCodeEnv(endpoint, apiKey, { pinClaudeModels }) })
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
        // CC Switch's Codex provider appends the OpenAI-compatible path itself.
        // Passing /v1 here can make the client request /v1/v1/....
        endpoint: withoutTrailingSlashes(baseUrl),
        model: OPENAI_CC_SWITCH_CODEX_MODEL,
        config: buildCodexImportToml(withoutTrailingSlashes(baseUrl)),
        configFormat: 'toml'
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
  if (config.app === 'claude') {
    // Only Anthropic groups serve Claude model IDs directly; other platforms
    // routed through Claude Code keep their own model mapping.
    const pinClaudeModels = (input.platform || 'anthropic') === 'anthropic'
    config.config = buildClaudeImportJson(config.endpoint, input.apiKey, pinClaudeModels)
    config.configFormat = 'json'
  }
  const entries: [string, string][] = [
    ['resource', 'provider'],
    ['app', config.app],
    ['name', input.providerName],
    ['homepage', input.baseUrl],
    ['endpoint', config.endpoint],
    ['apiKey', input.apiKey],
    ['configFormat', config.configFormat || 'json'],
    ['usageEnabled', 'true'],
    ['usageScript', btoa(input.usageScript)],
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
