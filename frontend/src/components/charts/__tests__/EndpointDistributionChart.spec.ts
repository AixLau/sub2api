import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import EndpointDistributionChart from '../EndpointDistributionChart.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'usage.endpointDistribution': 'Endpoint Distribution',
        'usage.endpoint': 'Endpoint',
        'admin.dashboard.requests': 'Requests',
        'admin.dashboard.tokens': 'Tokens',
        'admin.dashboard.actual': 'Actual',
        'admin.dashboard.standard': 'Standard',
        'admin.dashboard.metricTokens': 'By Tokens',
        'admin.dashboard.metricActualCost': 'By Actual Cost',
      }[key] ?? key),
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

vi.mock('@/api/admin/dashboard', () => ({
  getUserBreakdown: vi.fn(),
}))

describe('EndpointDistributionChart', () => {
  it('renders the polished ring without changing endpoint data', () => {
    const wrapper = mount(EndpointDistributionChart, {
      props: {
        endpointStats: [
          { endpoint: '/v1/messages', requests: 8, total_tokens: 1200, actual_cost: 0.8, cost: 1.2 },
          { endpoint: '/v1/chat/completions', requests: 4, total_tokens: 600, actual_cost: 0.4, cost: 0.6 },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
          UserBreakdownSubTable: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.get('.chart-data').text())
    expect(chartData.labels).toEqual(['/v1/messages', '/v1/chat/completions'])
    expect(chartData.datasets[0].data).toEqual([1200, 600])
    expect(chartData.datasets[0]).toMatchObject({
      borderRadius: 8,
      spacing: 2,
      hoverOffset: 4,
    })
    expect(wrapper.get('[data-testid="endpoint-ring-center"]').text()).toContain('1.80K')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    expect(options.cutout).toBe('72%')
    expect(options.animation.duration).toBe(700)
  })
})
