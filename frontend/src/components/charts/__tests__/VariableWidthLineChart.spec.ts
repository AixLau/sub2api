import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

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

describe('VariableWidthLineChart', () => {
  afterEach(() => {
    ChartMock.mockClear()
    chartInstances.splice(0)
  })

  it('renders a blue-highlighted title and custom Vue legend', async () => {
    const wrapper = mountChart()
    await nextTick()

    expect(wrapper.find('.vw-line__title').text()).toBe('Token 使用趋势')
    expect(wrapper.find('.vw-line__title').classes()).toContain('vw-line__title')
    expect(wrapper.findAll('.vw-line__legend-item').map((item) => item.text())).toEqual(['输入', '输出'])
    expect(wrapper.find('.vw-line__legend-marker').attributes('style')).toContain('background-color: rgb(37, 99, 235)')
  })

  it('creates a native G2 trail line with size mapped to the y field', async () => {
    mountChart()
    await nextTick()

    expect(ChartMock).toHaveBeenCalledWith(expect.objectContaining({
      autoFit: true,
      height: 320,
    }))

    const options = chartInstances[0].options.mock.calls[0][0]
    expect(options).toMatchObject({
      type: 'view',
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
    expect(mainLayer.axis).toMatchObject({
      x: expect.objectContaining({
        line: false,
        tick: false,
        gridLineDash: [4, 8],
      }),
      y: expect.objectContaining({
        line: false,
        tick: false,
        gridLineDash: [4, 8],
      }),
    })
    expect(options.children[0]).toMatchObject({
      type: 'line',
      encode: {
        x: '__vw_x__',
        y: '__vw_y__',
        color: '__vw_color__',
        series: '__vw_series__',
        size: '__vw_y__',
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
      domain: [0, 500],
    })
    expect(options.children.some((child: Record<string, unknown>) => child.type === 'point')).toBe(false)
    expect(chartInstances[0].render).toHaveBeenCalledTimes(1)
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

  it('expands original points into staccato brush segments by default', async () => {
    mountChart()
    await nextTick()

    const options = chartInstances[0].options.mock.calls[0][0]
    const inputSeries = options.data.filter((point: Record<string, unknown>) => point.__vw_series__ === '输入__0')

    expect(options.data.length).toBeGreaterThan(chartData.length)
    expect(inputSeries.length).toBeGreaterThan(2)
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
    expect(options.children[0].scale.size.range).toEqual([1.5, 12])
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

    await wrapper.find('.vw-line__body').trigger('mousemove', {
      clientX: 60,
      clientY: 120,
    })
    await nextTick()

    expect(wrapper.find('.vw-line__tooltip').exists()).toBe(true)
    expect(wrapper.find('.vw-line__tooltip').html()).toContain('<div>2026-05-08</div>')
    expect(wrapper.find('.vw-line__tooltip-crosshair').exists()).toBe(true)
    expect(tooltipHtml).toHaveBeenCalledWith('2026-05-08')

    await wrapper.find('.vw-line__body').trigger('mouseleave')
    await nextTick()

    expect(wrapper.find('.vw-line__tooltip').exists()).toBe(false)
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

  it('does not create a G2 chart when there is no data', async () => {
    const wrapper = mountChart({ data: [] })
    await nextTick()

    expect(wrapper.text()).toContain('暂无数据')
    expect(ChartMock).not.toHaveBeenCalled()
  })
})
