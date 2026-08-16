import { describe, expect, it } from 'vitest'
import { formatCompactCurrency } from '../format'
import { i18n } from '@/i18n'

describe('formatCompactCurrency', () => {
  it('uses compact USD notation for dense quota summaries', () => {
    const value = formatCompactCurrency(9528.86)

    expect(value).toBe('$9.53k')
  })

  it('keeps k notation in the Chinese locale instead of waiting for ten thousand', () => {
    const previous = i18n.global.locale.value
    try {
      i18n.global.locale.value = 'zh'
      expect(formatCompactCurrency(9528.86)).toBe('$9.53k')
    } finally {
      i18n.global.locale.value = previous
    }
  })

  it('keeps useful precision below the compact threshold', () => {
    expect(formatCompactCurrency(31.76)).toContain('31.76')
  })
})
