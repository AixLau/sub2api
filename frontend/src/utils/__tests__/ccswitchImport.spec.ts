import { describe, expect, it } from 'vitest'
import {
  CC_SWITCH_USAGE_SCRIPT,
  GROK_CC_SWITCH_MODEL,
  OPENAI_CC_SWITCH_CODEX_MODEL,
  buildCcSwitchImportDeeplink
} from '@/utils/ccswitchImport'
import type { GroupPlatform } from '@/types'

function paramsFromDeeplink(deeplink: string): URLSearchParams {
  const query = deeplink.split('?')[1] || ''
  return new URLSearchParams(query)
}

describe('ccswitchImport utils', () => {
  it('defaults OpenAI CC Switch imports to the current Codex model', () => {
    expect(OPENAI_CC_SWITCH_CODEX_MODEL).toBe('gpt-6.1-sol')
  })

  it('defaults Grok Build imports to the current Grok model', () => {
    expect(GROK_CC_SWITCH_MODEL).toBe('grok-4.5')
  })

  const baseInput = {
    baseUrl: 'https://api.example.com',
    providerName: 'Sub2API',
    apiKey: 'sk-test',
    usageScript: 'return true'
  }

  it.each([
    ['https://api.example.com', 'https://api.example.com'],
    ['https://api.example.com/', 'https://api.example.com'],
    ['https://api.example.com/v1', 'https://api.example.com/v1'],
    ['https://api.example.com/v1/', 'https://api.example.com/v1']
  ])('keeps Codex imports on the configured endpoint for base URL %s', (baseUrl, endpoint) => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        baseUrl,
        platform: 'openai',
        clientType: 'claude'
      })
    )

    expect(params.get('resource')).toBe('provider')
    expect(params.get('app')).toBe('codex')
    expect(params.get('endpoint')).toBe(endpoint)
    expect(params.get('model')).toBe(OPENAI_CC_SWITCH_CODEX_MODEL)
    expect(atob(params.get('usageScript') || '')).toBe(baseInput.usageScript)
  })

  it.each([
    'https://api.example.com',
    'https://api.example.com/',
    'https://api.example.com/v1',
    'https://api.example.com/v1/'
  ])('imports Grok Build with one /v1 suffix for base URL %s', (baseUrl) => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        baseUrl,
        platform: 'grok',
        clientType: 'claude'
      })
    )

    expect(params.get('app')).toBe('grokbuild')
    expect(params.get('endpoint')).toBe('https://api.example.com/v1')
    expect(params.get('model')).toBe(GROK_CC_SWITCH_MODEL)
  })

  it.each([
    { platform: 'anthropic' as GroupPlatform, clientType: 'claude' as const, app: 'claude' },
    { platform: 'gemini' as GroupPlatform, clientType: 'gemini' as const, app: 'gemini' }
  ])('does not add a model parameter for $platform imports', ({ platform, clientType, app }) => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform,
        clientType
      })
    )

    expect(params.get('app')).toBe(app)
    expect(params.get('endpoint')).toBe(baseInput.baseUrl)
    expect(params.has('model')).toBe(false)
  })

  it('keeps Antigravity imports on the selected client endpoint without a model parameter', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform: 'antigravity',
        clientType: 'gemini'
      })
    )

    expect(params.get('app')).toBe('gemini')
    expect(params.get('endpoint')).toBe(`${baseInput.baseUrl}/antigravity`)
    expect(params.has('model')).toBe(false)
  })
})

describe('CC Switch long-context config', () => {
  const input = {
    baseUrl: 'https://api.example.com',
    providerName: '站点',
    apiKey: 'sk-test',
    usageScript: 'return true'
  }
  const decode = (value: string | null) =>
    new TextDecoder().decode(Uint8Array.from(atob(value || ''), (c) => c.charCodeAt(0)))

  it('imports Claude groups with 1M model pins and the compaction threshold', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({ ...input, platform: 'anthropic', clientType: 'claude' })
    )
    expect(params.get('configFormat')).toBe('json')
    const env = JSON.parse(decode(params.get('config'))).env
    expect(env.ANTHROPIC_BASE_URL).toBe('https://api.example.com')
    expect(env.ANTHROPIC_AUTH_TOKEN).toBe('sk-test')
    expect(env.ANTHROPIC_DEFAULT_OPUS_MODEL).toBe('claude-opus-5-5[1m]')
    expect(env.ANTHROPIC_DEFAULT_SONNET_MODEL).toBe('claude-sonnet-5-5[1m]')
    expect(env.ANTHROPIC_DEFAULT_FABLE_MODEL).toBe('claude-fable-5-1[1m]')
    expect(env.CLAUDE_CODE_AUTO_COMPACT_WINDOW).toBe('1000000')
    expect(env.CLAUDE_AUTOCOMPACT_PCT_OVERRIDE).toBe('80')
  })

  it('keeps non-Claude platforms on their own models in Claude Code imports', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({ ...input, platform: 'deepseek', clientType: 'claude' })
    )
    const env = JSON.parse(decode(params.get('config'))).env
    expect(env).not.toHaveProperty('ANTHROPIC_DEFAULT_OPUS_MODEL')
    expect(env.CLAUDE_CODE_AUTO_COMPACT_WINDOW).toBe('1000000')
  })

  it('imports Codex with the 1M context window and auto-compact limit', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({ ...input, platform: 'openai', clientType: 'claude' })
    )
    expect(params.get('configFormat')).toBe('toml')
    const toml = decode(params.get('config'))
    expect(toml).toContain('model_context_window = 1000000')
    expect(toml).toContain('model_auto_compact_token_limit = 800000')
    expect(toml).toContain('model = "gpt-6.1-sol"')
    expect(toml).not.toContain('disable_response_storage')
    expect(toml).toContain('base_url = "https://api.example.com"')
  })
})

describe('CC Switch usage script', () => {
  // Mirrors CC Switch: substitute the template vars as text, evaluate, read request.url.
  function usageUrlFor(baseUrl: string): string {
    const script = CC_SWITCH_USAGE_SCRIPT.split('{{baseUrl}}').join(baseUrl).split('{{apiKey}}').join('sk-test')
    // eslint-disable-next-line no-new-func
    const config = new Function(`return ${script}`)() as { request: { url: string } }
    return config.request.url
  }

  it.each([
    'https://api.example.com',
    'https://api.example.com/',
    'https://api.example.com/v1',
    'https://api.example.com/v1/'
  ])('queries exactly one /v1/usage for base URL %s', (baseUrl) => {
    expect(usageUrlFor(baseUrl)).toBe('https://api.example.com/v1/usage')
  })

  it('works against the endpoint every platform import stores', () => {
    for (const platform of ['anthropic', 'openai', 'grok', 'gemini'] as GroupPlatform[]) {
      const endpoint = paramsFromDeeplink(
        buildCcSwitchImportDeeplink({
          baseUrl: 'https://api.example.com',
          platform,
          clientType: platform === 'gemini' ? 'gemini' : 'claude',
          providerName: 'Sub2API',
          apiKey: 'sk-test',
          usageScript: CC_SWITCH_USAGE_SCRIPT
        })
      ).get('endpoint') as string
      expect(usageUrlFor(endpoint)).toBe('https://api.example.com/v1/usage')
    }
  })
})
