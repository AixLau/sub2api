import { describe, expect, it } from 'vitest'
import type { GroupPlatform } from '@/types'
import { buildMagpieImportLink, supportsMagpieImport } from '@/utils/magpieImport'

function fragmentParams(link: string): URLSearchParams {
  expect(link.startsWith('https://usemagpie.ai/import#')).toBe(true)
  return new URLSearchParams(link.split('#')[1])
}

const base = {
  siteOrigin: 'https://aixlau.me/',
  apiKey: 'sk-test'
}

describe('buildMagpieImportLink', () => {
  it.each(['https://aixlau.me', 'https://aixlau.me/', 'https://aixlau.me/v1', 'https://aixlau.me/v1/'])(
    'declares chat, responses and anthropic for OpenAI groups from base URL %s',
    (baseUrl) => {
      const params = fragmentParams(buildMagpieImportLink({ ...base, baseUrl, platform: 'openai' }))
      expect(params.get('name')).toBe('星链AI')
      expect(params.get('id')).toBe('xinglian')
      expect(params.get('chat')).toBe('https://aixlau.me/v1')
      expect(params.get('responses')).toBe('https://aixlau.me/v1')
      expect(params.get('anthropic')).toBe('https://aixlau.me')
      expect(params.get('key')).toBe('sk-test')
      expect(params.get('catalog')).toBe('openai')
      expect(params.get('website')).toBe('https://aixlau.me')
      expect(params.get('keys')).toBe('https://aixlau.me/keys')
      expect(params.get('icon')).toBe('https://aixlau.me/logo.png')
      expect(params.has('gemini')).toBe(false)
    }
  )

  it('defaults to the Anthropic catalog when the group has no platform', () => {
    const params = fragmentParams(buildMagpieImportLink({ ...base, baseUrl: 'https://aixlau.me' }))
    expect(params.get('catalog')).toBe('anthropic')
    expect(params.get('anthropic')).toBe('https://aixlau.me')
  })

  it('uses the Gemini API for Gemini groups', () => {
    const params = fragmentParams(
      buildMagpieImportLink({ ...base, baseUrl: 'https://aixlau.me', platform: 'gemini' })
    )
    expect(params.get('gemini')).toBe('https://aixlau.me/v1beta')
    expect(params.get('catalog')).toBe('google')
    expect(params.has('chat')).toBe(false)
  })

  it('routes Antigravity groups through the /antigravity prefix', () => {
    const params = fragmentParams(
      buildMagpieImportLink({ ...base, baseUrl: 'https://aixlau.me', platform: 'antigravity' })
    )
    expect(params.get('anthropic')).toBe('https://aixlau.me/antigravity')
    expect(params.get('gemini')).toBe('https://aixlau.me/antigravity/v1beta')
    expect(params.has('catalog')).toBe(false)
  })

  it('omits the catalog for platforms without a models.dev mapping', () => {
    const params = fragmentParams(
      buildMagpieImportLink({ ...base, baseUrl: 'https://aixlau.me', platform: 'composite' as GroupPlatform })
    )
    expect(params.has('catalog')).toBe(false)
    expect(params.get('chat')).toBe('https://aixlau.me/v1')
  })
})

describe('supportsMagpieImport', () => {
  it('excludes the non-chat TypeSafe platform', () => {
    expect(supportsMagpieImport('typesafe')).toBe(false)
    expect(supportsMagpieImport('openai')).toBe(true)
    expect(supportsMagpieImport(undefined)).toBe(true)
  })
})
