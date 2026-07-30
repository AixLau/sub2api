import { describe, expect, it } from 'vitest'
import { supportLiquidGlassPreset } from '../liquidGlassPresets'

describe('supportLiquidGlassPreset', () => {
  it('keeps the production support dialog aligned with the baseline preview', () => {
    expect(supportLiquidGlassPreset).toEqual({
      radius: 28,
      border: 0.07,
      lightness: 50,
      blend: 'difference',
      alpha: 0.93,
      blur: 11,
      scale: -180,
      frost: 0.05,
      containerClass: 'relative z-10 w-[360px] max-w-[calc(100vw-32px)] overflow-hidden'
    })
  })
})
