import { describe, expect, it } from 'vitest'
import { getSubscriptionQuotaBoundary } from '../subscriptionQuota'

describe('subscriptionQuota utils', () => {
  it('caps monthly quota boundary at subscription expiry when next full window would outlive the subscription', () => {
    const boundary = getSubscriptionQuotaBoundary(
      '2026-05-29T00:00:00+08:00',
      'monthly',
      '2026-06-28T16:10:55+08:00'
    )

    expect(boundary?.kind).toBe('expiry')
    expect(boundary?.at.toISOString()).toBe('2026-06-28T08:10:55.000Z')
  })

  it('returns reset boundary when the next full monthly window still fits before expiry', () => {
    const boundary = getSubscriptionQuotaBoundary(
      '2026-05-29T00:00:00+08:00',
      'monthly',
      '2026-08-01T00:00:00+08:00'
    )

    expect(boundary?.kind).toBe('reset')
    expect(boundary?.at.toISOString()).toBe('2026-06-27T16:00:00.000Z')
  })
})
