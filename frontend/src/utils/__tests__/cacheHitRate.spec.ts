import { describe, expect, it } from 'vitest'
import { calculateCacheHitRate } from '@/utils/cacheHitRate'

describe('calculateCacheHitRate', () => {
  it('calculates cache reads as a percentage of prompt tokens', () => {
    expect(calculateCacheHitRate(150, 30, 70)).toBeCloseTo(28)
  })

  it('returns zero when there are no prompt tokens', () => {
    expect(calculateCacheHitRate(0, 0, 0)).toBe(0)
  })
})
