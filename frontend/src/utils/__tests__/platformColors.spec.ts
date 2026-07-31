import { describe, expect, it } from 'vitest'

import { platformAccentColor, platformBadgeLightClass, platformPickerClass } from '../platformColors'
import { brandColors, platformIdentityColors } from '@/theme/designTokens'

describe('platform color helpers', () => {
  it('normalizes provider aliases to a single platform identity', () => {
    expect(platformAccentColor('claude')).toBe(platformIdentityColors.anthropic)
    expect(platformAccentColor('google')).toBe(platformIdentityColors.gemini)
    expect(platformAccentColor('xAI')).toBe(platformIdentityColors.grok)
  })

  it('uses the product brand only as the unknown-platform fallback', () => {
    expect(platformAccentColor('unknown')).toBe(brandColors['500'])
    expect(platformBadgeLightClass('unknown')).toContain('slate')
  })

  it('keeps selected platform controls on platform tokens', () => {
    expect(platformPickerClass('openai', true)).toContain('platform-openai')
    expect(platformPickerClass('grok', true)).toContain('platform-grok')
  })
})
