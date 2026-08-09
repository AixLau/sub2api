import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import TokenUsageTrend from '../TokenUsageTrend.vue'
import { tokenUsageColors } from '@/theme/designTokens'

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

const VariableWidthLineChartStub = defineComponent({
  name: 'VariableWidthLineChart',
  props: {
    title: String,
    data: Array,
    xField: [String, Function],
    yField: [String, Function],
    colorField: [String, Function],
    colors: Array,
    height: Number,
    yDomain: Array,
    yTicks: Array,
    xTicks: Array,
    showLegend: Boolean,
    brushEffect: Boolean,
    strokeEffect: String,
    minLineWidth: Number,
    maxLineWidth: Number,
    emptyText: String,
    tooltipHtml: Function,
    formatX: Function,
    formatY: Function,
  },
  template: '<div class="variable-width-line-chart" />',
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

const mountTrend = (props: Record<string, unknown> = {}) => mount(TokenUsageTrend, {
  props: {
    trendData: [trendPoint],
    ...props,
  },
  global: {
    stubs: {
      LoadingSpinner: true,
      VariableWidthLineChart: VariableWidthLineChartStub,
    },
  },
})

const getChart = (wrapper: ReturnType<typeof mountTrend>) =>
  wrapper.findComponent(VariableWidthLineChartStub)

describe('TokenUsageTrend', () => {
  it('does not render total usage or cost summary in the chart header', () => {
    const wrapper = mountTrend({
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
    })

    const text = wrapper.text()
    expect(text).not.toContain('总使用')
    expect(text).not.toContain('消费')
    expect(text).not.toContain('$5.09')
  })

  it('renders the trend through the native G2 variable-width line component', () => {
    const wrapper = mountTrend()
    const chart = getChart(wrapper)

    expect(chart.exists()).toBe(true)
    expect(chart.props()).toMatchObject({
      title: 'Token 使用趋势',
      yField: 'value',
      colorField: 'category',
      showLegend: true,
      brushEffect: false,
      strokeEffect: 'smooth',
      minLineWidth: 1.2,
      maxLineWidth: 8,
    })
    expect(chart.props('xField')).toEqual(expect.any(Function))
    expect(chart.props('formatX')).toEqual(expect.any(Function))
    expect(chart.props('formatY')).toEqual(expect.any(Function))
    expect(chart.props('tooltipHtml')).toEqual(expect.any(Function))
    expect(chart.props('colors')).toEqual([
      tokenUsageColors.input,
      tokenUsageColors.output,
      tokenUsageColors.cacheCreation,
      tokenUsageColors.cacheRead,
    ])
    expect((chart.props('xField') as (datum: { date: string }) => unknown)({ date: '2026-05-08' })).toEqual(new Date('2026-05-08'))
  })

  it('maps token series for the native G2 chart', () => {
    const wrapper = mountTrend()
    const data = getChart(wrapper).props('data')

    expect(data).toEqual([
      expect.objectContaining({ date: '2026-05-08', category: '输入', value: 200 }),
      expect.objectContaining({ date: '2026-05-08', category: '输出', value: 50 }),
      expect.objectContaining({ date: '2026-05-08', category: '缓存创建', value: 300 }),
      expect.objectContaining({ date: '2026-05-08', category: '缓存读取', value: 500 }),
    ])
  })

  it('returns 0 hit rate in the tooltip when all prompt tokens are zero', () => {
    const wrapper = mountTrend({
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
    })

    const tooltipHtml = (getChart(wrapper).props('tooltipHtml') as (title: string) => string)('2026-05-08')
    expect(tooltipHtml).toContain('缓存命中率')
    expect(tooltipHtml).toContain('0.0%')
  })

  it('localizes tooltip footer and hides cost by default', () => {
    const wrapper = mountTrend()
    const tooltipHtml = (getChart(wrapper).props('tooltipHtml') as (title: string) => string)('2026-05-08')

    expect(tooltipHtml).toContain('class="token-trend-tooltip"')
    expect(tooltipHtml).toContain('class="token-trend-tooltip__title"')
    expect(tooltipHtml).toContain('class="token-trend-tooltip__rows"')
    expect(tooltipHtml).toContain('class="token-trend-tooltip__row"')
    expect(tooltipHtml).toContain('class="token-trend-tooltip__label"')
    expect(tooltipHtml).toContain('class="token-trend-tooltip__marker"')
    expect(tooltipHtml).toContain('class="token-trend-tooltip__value"')
    expect(tooltipHtml).toContain('class="token-trend-tooltip__summary"')
    expect(tooltipHtml).toContain(`--token-trend-marker: ${tokenUsageColors.input}`)
    expect(tooltipHtml).not.toContain('background: #111827')
    expect(tooltipHtml).toContain('总使用: 1.05K')
    expect(tooltipHtml).not.toContain('$')
    expect(tooltipHtml).not.toContain('消费')
    expect(tooltipHtml).not.toContain('Standard')
    expect(tooltipHtml).not.toContain('标准')
  })

  it('shows total usage and cost in tooltip footer when enabled', () => {
    const wrapper = mountTrend({ showCost: true })
    const tooltipHtml = (getChart(wrapper).props('tooltipHtml') as (title: string) => string)('2026-05-08')

    expect(tooltipHtml).toContain('总使用: 1.05K')
    expect(tooltipHtml).toContain('消费: $4.58')
    expect(tooltipHtml).not.toContain('实际消费')
    expect(tooltipHtml).not.toContain('Standard')
    expect(tooltipHtml).not.toContain('标准')
  })

  it('escapes dynamic tooltip text while keeping semantic markup', () => {
    const wrapper = mountTrend({
      trendData: [{ ...trendPoint, date: '<script>&"\'' }],
    })
    const tooltipHtml = (getChart(wrapper).props('tooltipHtml') as (title: string) => string)('<script>&"\'')

    expect(tooltipHtml).toContain('&lt;script&gt;&amp;&quot;&#039;')
    expect(tooltipHtml).not.toContain('<script>')
  })

  it('allows callers to enlarge the chart height', () => {
    const wrapper = mountTrend({ chartHeightClass: 'h-80' })

    expect(getChart(wrapper).props('height')).toBe(320)
  })

  it('uses localized no-data text inside the native chart component', () => {
    const wrapper = mountTrend({ trendData: [] })

    expect(getChart(wrapper).props('data')).toEqual([])
    expect(getChart(wrapper).props('emptyText')).toBe('No data available')
  })
})
