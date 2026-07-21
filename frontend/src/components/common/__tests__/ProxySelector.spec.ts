import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const testProxy = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin', () => ({
  adminAPI: { proxies: { testProxy } }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

import ProxySelector from '../ProxySelector.vue'

const buildProxy = (id: number) => ({
  id,
  name: `Proxy ${id}`,
  protocol: 'http',
  host: `proxy-${id}.example.com`,
  port: 8000 + id,
  status: 'active',
  username: null,
  expires_at: null,
  fallback_mode: 'none',
  expiry_warn_days: 7,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z'
}) as any

const mountSelector = (proxies: any[]) => mount(ProxySelector, {
  props: { modelValue: null, proxies },
  global: { stubs: { Icon: true } }
})

describe('ProxySelector', () => {
  beforeEach(() => {
    testProxy.mockReset()
  })

  it('virtualizes large lists and displays persisted geography and latency immediately', async () => {
    const proxies = Array.from({ length: 300 }, (_, index) => buildProxy(index + 1))
    Object.assign(proxies[0], {
      latency_status: 'success',
      latency_ms: 86,
      ip_address: '203.0.113.1',
      country: 'Japan',
      region: 'Tokyo',
      city: 'Tokyo'
    })
    const wrapper = mountSelector(proxies)

    await wrapper.get('.select-trigger').trigger('click')

    expect(wrapper.findAll('.select-option').length).toBeLessThan(20)
    expect(wrapper.text()).toContain('Japan · Tokyo')
    expect(wrapper.text()).toContain('86ms')
    expect(wrapper.text()).toContain('203.0.113.1')
  })

  it('limits batch testing to four concurrent requests', async () => {
    let active = 0
    let peak = 0
    const pending: Array<() => void> = []
    testProxy.mockImplementation((id: number) => new Promise((resolve) => {
      active += 1
      peak = Math.max(peak, active)
      pending.push(() => {
        active -= 1
        resolve({ success: true, message: 'ok', latency_ms: id })
      })
    }))
    const releasePending = async () => {
      pending.splice(0).forEach((resolve) => resolve())
      await flushPromises()
    }
    const wrapper = mountSelector(Array.from({ length: 20 }, (_, index) => buildProxy(index + 1)))

    await wrapper.get('.select-trigger').trigger('click')
    await wrapper.get('.batch-test-btn').trigger('click')
    await flushPromises()
    expect(testProxy).toHaveBeenCalledTimes(4)

    while (testProxy.mock.calls.length < 20) await releasePending()
    await releasePending()

    expect(testProxy).toHaveBeenCalledTimes(20)
    expect(peak).toBeLessThanOrEqual(4)
  })
})
