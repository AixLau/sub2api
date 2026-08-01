import { describe, expect, it } from 'vitest'

import {
  brandColors,
  dangerColors,
  platformIdentityColors,
  providerColors,
  semanticColors,
  successColors,
  warningColors,
} from '../designTokens'

describe('design token theme invariants', () => {
  it('keeps the pre-token product palette as the site-wide base', () => {
    expect(brandColors['500']).toBe('#14B8A6')
    expect(brandColors['600']).toBe('#0D9488')
    expect(semanticColors.light['line-focus']).toBe(brandColors['500'])
    expect(semanticColors.light['status-info']).toBe(brandColors['600'])
  })

  it('keeps scrims dark instead of following inverse surfaces', () => {
    expect(semanticColors.light['surface-scrim']).toBe('#020617')
    expect(semanticColors.dark['surface-scrim']).toBe('#000000')
    expect(semanticColors.dark['surface-scrim']).not.toBe(semanticColors.dark['surface-inverse'])
  })

  it('keeps text on brand actions white in both themes', () => {
    expect(semanticColors.light['content-on-brand']).toBe('#FFFFFF')
    expect(semanticColors.dark['content-on-brand']).toBe('#FFFFFF')
  })

  it('anchors semantic statuses to the shared primitive scales', () => {
    expect(semanticColors.light['status-success']).toBe(successColors['600'])
    expect(semanticColors.dark['status-success']).toBe(successColors['300'])
    expect(semanticColors.light['status-warning']).toBe(warningColors['700'])
    expect(semanticColors.dark['status-warning']).toBe(warningColors['300'])
    expect(semanticColors.light['status-danger']).toBe(dangerColors['700'])
    expect(semanticColors.dark['status-danger']).toBe(dangerColors['300'])
  })

  it('keeps external identity colors separate from product and status colors', () => {
    expect(platformIdentityColors.gemini).not.toBe(brandColors['500'])
    expect(platformIdentityColors.openai).not.toBe(successColors['500'])
    expect(providerColors.alipay).not.toBe(brandColors['500'])
    expect(providerColors.wechat).not.toBe(successColors['500'])
  })
})
