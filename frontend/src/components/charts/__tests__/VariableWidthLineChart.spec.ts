import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import VariableWidthLineChart from '../VariableWidthLineChart.vue'

const { chartInstances, ChartMock } = vi.hoisted(() => {
  const instances: Array<{
    options: ReturnType<typeof vi.fn>
    render: ReturnType<typeof vi.fn>
    destroy: ReturnType<typeof vi.fn>
  }> = []

  const Chart = vi.fn(() => {
    const instance = {
      options: vi.fn(),
      render: vi.fn(() => Promise.resolve()),
      forceFit: vi.fn(() => Promise.resolve()),
      destroy: vi.fn(),
    }
    instances.push(instance)
    return instance
  })

  return {
    chartInstances: instances,
    ChartMock: Chart,
  }
})

vi.mock('@antv/g2', () => ({
  Chart: ChartMock,
}))

const chartData = [
  { date: '2026-05-08', value: 200, category: '输入' },
  { date: '2026-05-09', value: 400, category: '输入' },
  { date: '2026-05-08', value: 50, category: '输出' },
  { date: '2026-05-09', value: 100, category: '输出' },
]

const mountChart = (props: Record<string, unknown> = {}) => mount(VariableWidthLineChart, {
  props: {
    title: 'Token 使用趋势',
    data: chartData,
    xField: 'date',
    yField: 'value',
    colorField: 'category',
    colors: ['#2563eb', '#059669'],
    height: 320,
    yDomain: [0, 500],
    yTicks: [0, 250, 500],
    xTicks: ['2026-05-08', '2026-05-09'],
    ...props,
  },
})

const defaultRect = {
  x: 0,
  y: 0,
  top: 0,
  right: 400,
  bottom: 320,
  left: 0,
  width: 400,
  height: 320,
  toJSON: () => ({}),
} as DOMRect

const extractLeftPercent = (style: string | undefined): number => {
  const match = style?.match(/left:\s*([0-9.]+)%/)
  return match ? Number(match[1]) : Number.NaN
}

describe('VariableWidthLineChart', () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(defaultRect)
  })

  afterEach(() => {
    ChartMock.mockClear()
    chartInstances.splice(0)
    vi.restoreAllMocks()
  })

  it('renders a blue-highlighted title and custom Vue legend', async () => {
    const wrapper = mountChart()
    await nextTick()

    expect(wrapper.find('.vw-line__title').text()).toBe('Token 使用趋势')
    expect(wrapper.find('.vw-line__title').classes()).toContain('vw-line__title')
    expect(wrapper.findAll('.vw-line__legend-item').map((item) => item.text())).toEqual(['输入', '输出'])
    expect(wrapper.find('.vw-line__legend-marker').attributes('style')).toContain('background-color: rgb(37, 99, 235)')
  })

  it('creates a native G2 trail line with compressed dynamic width derived from the y field', async () => {
    mountChart()
    await nextTick()

    expect(ChartMock).toHaveBeenCalledWith(expect.objectContaining({
      autoFit: true,
      height: 320,
    }))

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options).toMatchObject({
      type: 'view',
      margin: 0,
      inset: 0,
      legend: false,
      scale: {
        color: {
          domain: ['输入', '输出'],
          range: ['#2563eb', '#059669'],
        },
        y: {
          domain: [0, 500],
          nice: false,
        },
      },
      axis: false,
    })

    expect(options.children).toHaveLength(3)
    const mainLayer = options.children[2]
    expect(mainLayer.axis).toBe(false)
    expect(options.children[0]).toMatchObject({
      type: 'line',
      encode: {
        x: '__vw_x__',
        y: '__vw_y__',
        color: '__vw_color__',
        series: '__vw_series__',
        size: '__vw_visual_size__',
        shape: 'trail',
      },
      legend: false,
      tooltip: false,
      style: {
        lineCap: 'round',
        lineJoin: 'round',
      },
    })
    expect(options.interaction).toEqual({})
    expect(options.children[0].scale.size).toMatchObject({
      type: 'linear',
      range: [3.2, 8.9],
    })
    expect(mainLayer.scale.size.range).toEqual([1.2, 6.5])
    const originalPoints = options.data.filter((point: Record<string, unknown>) => point.__vw_staccato__ === false)
    const inputPoints = originalPoints.filter((point: Record<string, unknown>) => point.__vw_color__ === '输入')
    const lowVisualSize = Number(inputPoints[0].__vw_visual_size__)
    const highVisualSize = Number(inputPoints[1].__vw_visual_size__)
    expect(highVisualSize).toBeGreaterThan(lowVisualSize)
    expect(highVisualSize / lowVisualSize).toBeLessThan(400 / 200)
    expect(options.children.some((child: Record<string, unknown>) => child.type === 'point')).toBe(false)
    expect(chartInstances[0].render).toHaveBeenCalledTimes(1)
  })

  it('uses custom HTML overlays as the only axis renderer and shares the plot layout', async () => {
    const wrapper = mountChart()
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options.children.every((child: Record<string, unknown>) => child.axis === false)).toBe(true)
    expect(wrapper.find('.vw-line__body').attributes('style')).toContain('--vw-plot-left')
    expect(wrapper.find('.vw-line__x-label--start').exists()).toBe(true)
    expect(wrapper.find('.vw-line__x-label--end').exists()).toBe(true)
  })

  it('owns semantic Token tooltip styles in the shared themed surface', () => {
    const source = readFileSync(resolve(process.cwd(), 'src/components/charts/VariableWidthLineChart.vue'), 'utf8')

    expect(source).toContain(':deep(.token-trend-tooltip__row)')
    expect(source).toContain(':deep(.token-trend-tooltip__marker)')
    expect(source).toContain('background: var(--token-trend-marker)')
    expect(source).toMatch(/\.vw-line__tooltip\s*\{[\s\S]*?pointer-events: none;/)
  })

  it('can render endpoint dots when explicitly enabled', async () => {
    mountChart({ showEndDot: true })
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options.children.at(-1)).toMatchObject({
      type: 'point',
      encode: expect.objectContaining({
        x: '__vw_x__',
        y: '__vw_y__',
        color: '__vw_color__',
      }),
    })
  })

  it('uses the same y domain for the G2 line scale and custom grid labels when yDomain is automatic', async () => {
    mountChart({
      data: [
        { date: '2026-07-08T00:00:00', value: 7_910_000, category: '缓存读取' },
        { date: '2026-07-08T15:00:00', value: 25_540_000, category: '缓存读取' },
        { date: '2026-07-08T23:00:00', value: 31_660_000, category: '缓存读取' },
      ],
      xField: (datum: Record<string, unknown>) => new Date(String(datum.date)),
      yDomain: undefined,
      yTicks: undefined,
      xTicks: undefined,
    })
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options.scale.y).toMatchObject({
      domain: [0, 31_660_000],
      nice: false,
    })
  })

  it('renders isolated single-point series without enabling endpoint dots', async () => {
    mountChart({
      data: [
        { date: '2026-07-09T00:00:00', value: 200, category: '输入' },
      ],
      xField: (datum: Record<string, unknown>) => new Date(String(datum.date)),
      xTicks: undefined,
      showEndDot: false,
    })
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    const pointLayers = options.children.filter((child: Record<string, unknown>) => child.type === 'point')

    expect(pointLayers).toHaveLength(1)
    expect(pointLayers[0].data).toEqual([
      expect.objectContaining({ __vw_color__: '输入', __vw_y__: 200 }),
    ])
  })

  it('expands original points into staccato brush segments by default', async () => {
    mountChart()
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    const inputSeries = options.data.filter((point: Record<string, unknown>) => point.__vw_series__ === '输入__0')

    expect(options.data.length).toBeGreaterThan(chartData.length)
    expect(inputSeries.length).toBeGreaterThan(2)
    expect(inputSeries.filter((point: Record<string, unknown>) => point.__vw_staccato__ === true)).toHaveLength(3)
    expect(inputSeries[0].__vw_y__).toBe(200)
    expect(inputSeries.at(-1).__vw_y__).toBe(400)
    expect(inputSeries.some((point: Record<string, unknown>) => point.__vw_staccato__ === true)).toBe(true)
  })

  it('can render smooth data without staccato expansion', async () => {
    mountChart({ strokeEffect: 'smooth' })
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options.data).toHaveLength(chartData.length)
    expect(options.data.some((point: Record<string, unknown>) => point.__vw_staccato__ === true)).toBe(false)
  })

  it('uses a single trail layer when brushEffect is disabled', async () => {
    mountChart({ brushEffect: false, showEndDot: false })
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options.children).toHaveLength(1)
    expect(options.children[0].scale.size.range).toEqual([1.2, 6.5])
  })

  it('uses an external tooltip renderer when provided', async () => {
    const tooltipHtml = vi.fn((title: unknown) => `<div>${String(title)}</div>`)
    const rectSpy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 0,
      y: 0,
      top: 0,
      right: 400,
      bottom: 320,
      left: 0,
      width: 400,
      height: 320,
      toJSON: () => ({}),
    } as DOMRect)
    const wrapper = mountChart({ tooltipHtml })
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options.children[0].tooltip).toBe(false)
    expect(options.interaction).toEqual({})

    await wrapper.find('.vw-line__body').trigger('pointermove', {
      clientX: 60,
      clientY: 120,
    })
    await nextTick()

    expect(wrapper.find('.vw-line__tooltip').exists()).toBe(true)
    expect(wrapper.find('.vw-line__tooltip').html()).toContain('<div>2026-05-08</div>')
    expect(wrapper.find('.vw-line__tooltip-crosshair').exists()).toBe(true)
    expect(tooltipHtml).toHaveBeenCalledWith('2026-05-08')

    await wrapper.find('.vw-line__body').trigger('pointerleave')
    await nextTick()

    expect(wrapper.find('.vw-line__tooltip').exists()).toBe(false)
    rectSpy.mockRestore()
  })

  it('positions datetime x-axis labels by elapsed time instead of point index', async () => {
    const wrapper = mountChart({
      data: [
        { date: '2026-07-09T00:00:00', value: 10, category: '输入' },
        { date: '2026-07-09T20:00:00', value: 30, category: '输入' },
        { date: '2026-07-09T21:00:00', value: 20, category: '输入' },
        { date: '2026-07-09T22:00:00', value: 40, category: '输入' },
      ],
      xField: (datum: Record<string, unknown>) => new Date(String(datum.date)),
      xTicks: [
        new Date('2026-07-09T00:00:00'),
        new Date('2026-07-09T20:00:00'),
        new Date('2026-07-09T21:00:00'),
        new Date('2026-07-09T22:00:00'),
      ],
    })
    await nextTick()

    const labels = wrapper.findAll('.vw-line__x-label')
    const labelPositions = labels.map((label) => extractLeftPercent(label.attributes('style')))

    expect(labels.map((label) => label.text())).toHaveLength(4)
    expect(labelPositions[0]).toBeCloseTo(0, 3)
    expect(labelPositions[1]).toBeCloseTo((20 / 22) * 100, 3)
    expect(labelPositions[2]).toBeCloseTo((21 / 22) * 100, 3)
    expect(labelPositions[3]).toBeCloseTo(100, 3)
  })

  it('selects tooltip values by nearest datetime rather than nearest point index', async () => {
    const tooltipHtml = vi.fn((title: unknown) => `<div>${String(title)}</div>`)
    const rectSpy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 0,
      y: 0,
      top: 0,
      right: 400,
      bottom: 320,
      left: 0,
      width: 400,
      height: 320,
      toJSON: () => ({}),
    } as DOMRect)
    const wrapper = mountChart({
      data: [
        { date: '2026-07-09T00:00:00', value: 10, category: '输入' },
        { date: '2026-07-09T20:00:00', value: 30, category: '输入' },
        { date: '2026-07-09T21:00:00', value: 20, category: '输入' },
        { date: '2026-07-09T22:00:00', value: 40, category: '输入' },
      ],
      xField: (datum: Record<string, unknown>) => new Date(String(datum.date)),
      xTicks: undefined,
      tooltipHtml,
    })
    await nextTick()

    await wrapper.find('.vw-line__body').trigger('pointermove', {
      clientX: 184,
      clientY: 120,
    })
    await nextTick()

    const title = tooltipHtml.mock.calls[0][0]
    expect(title).toEqual(new Date('2026-07-09T00:00:00'))
    expect(wrapper.find('.vw-line__tooltip-crosshair').attributes('style')).toContain('left: 48px')

    rectSpy.mockRestore()
  })

  it('recreates the chart when props change and destroys it on unmount', async () => {
    const wrapper = mountChart()
    await nextTick()

    const firstInstance = chartInstances[0]
    await wrapper.setProps({
      data: [
        ...chartData,
        { date: '2026-05-10', value: 150, category: '输出' },
      ],
    })
    await nextTick()

    expect(firstInstance.destroy).toHaveBeenCalledTimes(1)
    expect(chartInstances).toHaveLength(2)

    wrapper.unmount()
    expect(chartInstances[1].destroy).toHaveBeenCalledTimes(1)
  })

  it('refits within a breakpoint and rebuilds then fits when crossing 480px', async () => {
    let width = 520
    let notifyResize: ((entries: ResizeObserverEntry[]) => void) | undefined
    class TestResizeObserver {
      observe = vi.fn()
      disconnect = vi.fn()

      constructor(callback: (entries: ResizeObserverEntry[]) => void) {
        notifyResize = callback
      }
    }

    vi.stubGlobal('ResizeObserver', TestResizeObserver)
    const rectSpy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      ...defaultRect,
      width,
      right: width,
    }))
    const wrapper = mountChart()
    await nextTick()

    const firstInstance = chartInstances[0]
    width = 540
    notifyResize?.([{ contentRect: { width, height: 320 } } as ResizeObserverEntry])
    await new Promise((resolve) => setTimeout(resolve, 32))
    await nextTick()

    expect(firstInstance.forceFit).toHaveBeenCalled()

    width = 400
    notifyResize?.([{ contentRect: { width, height: 320 } } as ResizeObserverEntry])
    await new Promise((resolve) => setTimeout(resolve, 32))
    await nextTick()

    expect(chartInstances).toHaveLength(2)
    expect(chartInstances[1].forceFit).toHaveBeenCalled()
    rectSpy.mockRestore()
    wrapper.unmount()
  })

  it('clears an active tooltip before domain changes rerender the chart', async () => {
    const tooltipHtml = vi.fn((title: unknown) => `<div>${String(title)}</div>`)
    const wrapper = mountChart({ tooltipHtml })
    await nextTick()

    await wrapper.find('.vw-line__body').trigger('pointermove', {
      clientX: 160,
      clientY: 120,
    })
    await nextTick()
    expect(wrapper.find('.vw-line__tooltip-crosshair').exists()).toBe(true)

    await wrapper.setProps({ yDomain: [0, 1000] })
    await nextTick()

    expect(wrapper.find('.vw-line__tooltip-crosshair').exists()).toBe(false)
    expect(wrapper.find('.vw-line__tooltip').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not create a G2 chart when there is no data', async () => {
    const wrapper = mountChart({ data: [] })
    await nextTick()

    expect(wrapper.text()).toContain('暂无数据')
    expect(ChartMock).not.toHaveBeenCalled()
  })
})
