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

const { createRootMock, lineMock, reactRoots } = vi.hoisted(() => {
  const roots: any[] = []
  const LineMock = vi.fn(() => null)
  const CreateRootMock = vi.fn((container) => {
    const root = {
      container,
      render: vi.fn(),
      unmount: vi.fn(),
    }
    roots.push(root)
    return root
  })

  return {
    createRootMock: CreateRootMock,
    lineMock: LineMock,
    reactRoots: roots,
  }
})

vi.mock('@ant-design/plots', () => ({
  Line: lineMock,
}))

vi.mock('react-dom/client', () => ({
  createRoot: createRootMock,
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
  total_tokens: 1050,
  cost: 2.64,
  actual_cost: 4.58,
}

const getRenderedConfig = () => {
  const root = reactRoots[0]
  return root.render.mock.calls.at(-1)[0].props
}

describe('TokenUsageTrend', () => {
  afterEach(() => {
    createRootMock.mockClear()
    lineMock.mockClear()
    reactRoots.splice(0)
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
            total_tokens: 200,
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

  it('renders the trend with Ant Design Plots trail line', () => {
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

    expect(createRootMock).toHaveBeenCalledTimes(1)
    expect(reactRoots[0].render).toHaveBeenCalledTimes(1)

    const config = getRenderedConfig()
    expect(reactRoots[0].render.mock.calls[0][0].type).toBe(lineMock)
    expect(config).toMatchObject({
      autoFit: true,
      yField: 'value',
      sizeField: 'value',
      shapeField: 'trail',
      colorField: 'category',
      legend: expect.objectContaining({ size: false }),
    })
    expect(config.xField({ date: '2026-05-08' })).toEqual(new Date('2026-05-08'))
  })

  it('maps token series for Ant Design Plots', () => {
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

    const { data } = getRenderedConfig()

    expect(data).toEqual([
      expect.objectContaining({ date: '2026-05-08', category: '输入', value: 200 }),
      expect.objectContaining({ date: '2026-05-08', category: '输出', value: 50 }),
      expect.objectContaining({ date: '2026-05-08', category: '缓存创建', value: 300 }),
      expect.objectContaining({ date: '2026-05-08', category: '缓存读取', value: 500 }),
    ])
  })

  it('returns 0 hit rate in the tooltip when all prompt tokens are zero', () => {
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
            total_tokens: 0,
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

    const tooltipHtml = getRenderedConfig().interaction.tooltip.render(null, {
      title: '2026-05-08',
      items: [],
    })
    expect(tooltipHtml).toContain('缓存命中率')
    expect(tooltipHtml).toContain('0.0%')
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

    const tooltipHtml = getRenderedConfig().interaction.tooltip.render(null, {
      title: '2026-05-08',
      items: [],
    })

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

    const tooltipHtml = getRenderedConfig().interaction.tooltip.render(null, {
      title: '2026-05-08',
      items: [],
    })

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

  it('updates and unmounts the React chart with the component lifecycle', async () => {
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

    const root = reactRoots[0]

    await wrapper.setProps({
      trendData: [
        {
          ...trendPoint,
          date: '2026-05-09',
          input_tokens: 400,
          total_tokens: 1250,
        },
      ],
    })
    await nextTick()

    expect(root.render).toHaveBeenCalledTimes(2)
    expect(getRenderedConfig().data).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ date: '2026-05-09', category: '输入', value: 400 }),
      ])
    )

    wrapper.unmount()
    expect(root.unmount).toHaveBeenCalledTimes(1)
  })
})
