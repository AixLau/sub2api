import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

const { getStats } = vi.hoisted(() => ({ getStats: vi.fn() }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { getStats }
  }
}))

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="line-chart" />'
  }
}))

import AccountStatsModal from '../AccountStatsModal.vue'

describe('admin AccountStatsModal trend chart', () => {
  it('keeps the right-side axes available for scaling without rendering tick labels', () => {
    const wrapper = mount(AccountStatsModal, {
      props: { show: false, account: null },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          LoadingSpinner: true,
          Icon: true,
          ModelDistributionChart: true,
          EndpointDistributionChart: true
        }
      }
    })

    const options = (wrapper.vm as any).$?.setupState.lineChartOptions
    expect((wrapper.vm as any).$?.setupState.statsRange).toBe('24h')
    expect((wrapper.vm as any).$?.setupState.statsRangeOptions).toHaveLength(3)
    expect(options.scales.y1.ticks.display).toBe(false)
    expect(options.scales.y2.ticks.display).toBe(false)
    expect(options.scales.y3.ticks.display).toBe(false)
    expect(options.scales.y1.display).toBe(true)
    expect(options.scales.y2.display).toBe(true)
    expect(options.scales.y3.display).toBe(true)
  })
})
