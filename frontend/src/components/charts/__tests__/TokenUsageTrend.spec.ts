import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import TokenUsageTrend from '../TokenUsageTrend.vue'

const messages: Record<string, string> = {
  'usage.tokenUsageTrend': 'Token 使用趋势',
  'usage.trend.input': '输入',
  'usage.trend.output': '输出',
  'usage.trend.cacheCreation': '缓存创建',
  'usage.trend.cacheRead': '缓存读取',
  'usage.trend.cacheHitRate': '缓存命中率',
  'usage.trend.totalUsage': '总使用',
  'usage.trend.actualCost': '实际消费',
  'usage.trend.cost': '消费',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('TokenUsageTrend', () => {
  it('does not render total usage or cost summary in the chart header', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        showCost: true,
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 2,
            actual_cost: 4.58,
          },
          {
            date: '2026-05-09',
            requests: 1,
            input_tokens: 100,
            output_tokens: 25,
            cache_creation_tokens: 50,
            cache_read_tokens: 25,
            cost: 0.4,
            actual_cost: 0.51,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).not.toContain('总使用')
    expect(text).not.toContain('消费')
    expect(text).not.toContain('$5.09')
  })

  it('calculates cache hit rate against all prompt tokens', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 500,
            output_tokens: 100,
            cache_creation_tokens: 0,
            cache_read_tokens: 1500,
            cost: 0.01,
            actual_cost: 0.005,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === '缓存命中率'
    )
    // Hit rate = 1500 / (500 + 1500 + 0) * 100 = 75%
    expect(hitRateDataset.data[0]).toBe(75)
  })

  it('returns 0 hit rate when all prompt tokens are zero', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 0,
            input_tokens: 0,
            output_tokens: 0,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            cost: 0,
            actual_cost: 0,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === '缓存命中率'
    )
    expect(hitRateDataset.data[0]).toBe(0)
  })

  it('includes cache_creation_tokens in denominator for Anthropic models', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 0.02,
            actual_cost: 0.01,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === '缓存命中率'
    )
    // Hit rate = 500 / (200 + 500 + 300) * 100 = 50%
    expect(hitRateDataset.data[0]).toBe(50)
  })

  it('localizes tooltip footer and hides cost by default', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 2.64,
            actual_cost: 4.58,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const footer = (wrapper.vm as any).$?.setupState.lineOptions.plugins.tooltip.callbacks.footer([
      { dataIndex: 0 },
    ])

    expect(footer).toEqual(['总使用: 1.05K'])
    expect(footer.join(' ')).not.toContain('$')
    expect(footer.join(' ')).not.toContain('消费')
    expect(footer.join(' ')).not.toContain('Standard')
    expect(footer.join(' ')).not.toContain('标准')
  })

  it('shows total usage and cost in tooltip footer when enabled', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        showCost: true,
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 2.64,
            actual_cost: 4.58,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const footer = (wrapper.vm as any).$?.setupState.lineOptions.plugins.tooltip.callbacks.footer([
      { dataIndex: 0 },
    ])

    expect(footer).toEqual(['总使用: 1.05K', '消费: $4.58'])
    expect(footer.join(' ')).not.toContain('实际消费')
    expect(footer.join(' ')).not.toContain('Standard')
    expect(footer.join(' ')).not.toContain('标准')
  })

  it('uses a visible tooltip footer color in light mode', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        showCost: true,
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 2.64,
            actual_cost: 4.58,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const tooltipOptions = (wrapper.vm as any).$?.setupState.lineOptions.plugins.tooltip

    expect(tooltipOptions.backgroundColor).toBe('#ffffff')
    expect(tooltipOptions.footerColor).toBe('#111827')
  })
})
