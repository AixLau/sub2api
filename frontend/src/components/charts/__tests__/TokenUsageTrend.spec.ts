import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

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

const { dualAxesInstances, DualAxesMock } = vi.hoisted(() => {
  const instances: any[] = []
  const Mock = vi.fn().mockImplementation((container, options) => {
    const instance = {
      container,
      options,
      render: vi.fn(),
      update: vi.fn((nextOptions) => {
        instance.options = nextOptions
      }),
      destroy: vi.fn(),
    }
    instances.push(instance)
    return instance
  })

  return {
    dualAxesInstances: instances,
    DualAxesMock: Mock,
  }
})

vi.mock('@antv/g2plot', () => ({
  DualAxes: DualAxesMock,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const trendPoint = {
  date: '2026-05-08',
  requests: 1,
  input_tokens: 200,
  output_tokens: 50,
  cache_creation_tokens: 300,
  cache_read_tokens: 500,
  cost: 2.64,
  actual_cost: 4.58,
}

describe('TokenUsageTrend', () => {
  afterEach(() => {
    DualAxesMock.mockClear()
    dualAxesInstances.splice(0)
  })

  it('does not render total usage or cost summary in the chart header', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        showCost: true,
        trendData: [
          trendPoint,
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

  it('renders the trend with AntV G2Plot DualAxes', () => {
    mount(TokenUsageTrend, {
      props: {
        trendData: [trendPoint],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(DualAxesMock).toHaveBeenCalledTimes(1)
    expect(dualAxesInstances[0].render).toHaveBeenCalledTimes(1)
    expect(dualAxesInstances[0].options).toMatchObject({
      autoFit: true,
      xField: 'date',
      yField: ['value', 'hitRate'],
      geometryOptions: [
        expect.objectContaining({
          geometry: 'line',
          seriesField: 'type',
          smooth: true,
        }),
        expect.objectContaining({
          geometry: 'line',
          smooth: true,
        }),
      ],
    })
  })

  it('maps token series and cache hit rate for G2Plot', () => {
    mount(TokenUsageTrend, {
      props: {
        trendData: [trendPoint],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const [tokenSeries, hitRateSeries] = dualAxesInstances[0].options.data

    expect(tokenSeries).toEqual([
      expect.objectContaining({ date: '2026-05-08', type: '输入', value: 200 }),
      expect.objectContaining({ date: '2026-05-08', type: '输出', value: 50 }),
      expect.objectContaining({ date: '2026-05-08', type: '缓存创建', value: 300 }),
      expect.objectContaining({ date: '2026-05-08', type: '缓存读取', value: 500 }),
    ])
    expect(hitRateSeries).toEqual([
      expect.objectContaining({ date: '2026-05-08', type: '缓存命中率', hitRate: 50 }),
    ])
  })

  it('returns 0 hit rate when all prompt tokens are zero', () => {
    mount(TokenUsageTrend, {
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

    const [, hitRateSeries] = dualAxesInstances[0].options.data
    expect(hitRateSeries[0].hitRate).toBe(0)
  })

  it('localizes tooltip footer and hides cost by default', () => {
    mount(TokenUsageTrend, {
      props: {
        trendData: [trendPoint],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const tooltipHtml = dualAxesInstances[0].options.tooltip.customContent('2026-05-08', [])

    expect(tooltipHtml).toContain('总使用: 1.05K')
    expect(tooltipHtml).not.toContain('$')
    expect(tooltipHtml).not.toContain('消费')
    expect(tooltipHtml).not.toContain('Standard')
    expect(tooltipHtml).not.toContain('标准')
  })

  it('shows total usage and cost in tooltip footer when enabled', () => {
    mount(TokenUsageTrend, {
      props: {
        showCost: true,
        trendData: [trendPoint],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const tooltipHtml = dualAxesInstances[0].options.tooltip.customContent('2026-05-08', [])

    expect(tooltipHtml).toContain('总使用: 1.05K')
    expect(tooltipHtml).toContain('消费: $4.58')
    expect(tooltipHtml).not.toContain('实际消费')
    expect(tooltipHtml).not.toContain('Standard')
    expect(tooltipHtml).not.toContain('标准')
  })

  it('allows callers to enlarge the chart height', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        chartHeightClass: 'h-80',
        trendData: [trendPoint],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.find('.h-80').exists()).toBe(true)
  })

  it('updates and destroys the G2Plot instance with the component lifecycle', async () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [trendPoint],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const instance = dualAxesInstances[0]

    await wrapper.setProps({
      trendData: [
        {
          ...trendPoint,
          date: '2026-05-09',
          input_tokens: 400,
        },
      ],
    })
    await nextTick()

    expect(instance.update).toHaveBeenCalled()
    expect(instance.options.data[0]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ date: '2026-05-09', type: '输入', value: 400 }),
      ])
    )

    wrapper.unmount()
    expect(instance.destroy).toHaveBeenCalledTimes(1)
  })
})
