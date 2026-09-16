import { describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ default: { get }, apiClient: { get } }))
import { getUpstreamBillingRatesWithEtag } from '@/api/admin/accounts'

describe('account billing rate refresh', () => {
  it('sends the current filters and ETag and preserves a not-modified result', async () => {
    get.mockResolvedValueOnce({ status: 304, headers: { etag: 'current' } })
    const controller = new AbortController()
    const result = await getUpstreamBillingRatesWithEtag(2, 50,
      { platform: 'openai', sort_by: 'upstream_billing_rate', sort_order: 'desc' },
      { etag: 'previous', signal: controller.signal })
    expect(result).toEqual({ notModified: true, etag: 'current', data: null })
    const [url, options] = get.mock.calls.at(-1)!
    expect(url).toBe('/admin/accounts/upstream-billing-rates')
    expect(options.params).toEqual({ page: 2, page_size: 50, platform: 'openai', sort_by: 'upstream_billing_rate', sort_order: 'desc' })
    expect(options.headers).toEqual({ 'If-None-Match': 'previous' })
    expect(options.signal).toBe(controller.signal)
    expect(options.validateStatus(304)).toBe(true)
    expect(options.validateStatus(500)).toBe(false)
  })

  it('returns fresh snapshots and their ETag', async () => {
    const data = { items: [{ account_id: 1, snapshot: { synced_rate_multiplier: 0.5 } }], total: 1 }
    get.mockResolvedValueOnce({ status: 200, headers: { etag: 'updated' }, data })
    await expect(getUpstreamBillingRatesWithEtag()).resolves.toEqual({ notModified: false, etag: 'updated', data })
  })
})
