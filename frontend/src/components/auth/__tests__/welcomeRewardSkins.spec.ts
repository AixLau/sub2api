import { describe, expect, it, vi } from 'vitest'
import {
  isWelcomeRewardSkinId,
  pickWelcomeRewardSkinId,
  welcomeRewardSkins
} from '../welcomeRewardSkins'

describe('welcomeRewardSkins', () => {
  it('defines three distinct image skins', () => {
    expect(welcomeRewardSkins).toHaveLength(3)
    expect(new Set(welcomeRewardSkins.map((skin) => skin.id)).size).toBe(3)
    expect(welcomeRewardSkins.every((skin) => skin.coverImage.endsWith('.webp'))).toBe(true)
  })

  it('validates persisted skin identifiers', () => {
    expect(isWelcomeRewardSkinId('starlink-explorer')).toBe(true)
    expect(isWelcomeRewardSkinId('unknown')).toBe(false)
    expect(isWelcomeRewardSkinId(null)).toBe(false)
  })

  it('selects a skin using the available random source', () => {
    vi.stubGlobal('crypto', undefined)
    vi.spyOn(Math, 'random').mockReturnValue(0.999)

    expect(pickWelcomeRewardSkinId()).toBe('lucky-passage')

    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
})
