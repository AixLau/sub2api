import { describe, expect, it } from 'vitest'

import {
  createPlotLayout,
  createXCoordinateModel,
  findNearestXValue,
  getXPositionPercent,
  placeTooltip,
  selectAutomaticXTicks,
} from '../variableWidthLineChartGeometry'

describe('createPlotLayout', () => {
  it.each([
    [343, { left: 48, right: 12, top: 12, bottom: 38 }],
    [479, { left: 48, right: 12, top: 12, bottom: 38 }],
    [480, { left: 56, right: 24, top: 12, bottom: 42 }],
    [960, { left: 56, right: 24, top: 12, bottom: 42 }],
  ])('uses responsive padding at %ipx', (width, padding) => {
    expect(createPlotLayout(width, 320)).toMatchObject({
      width,
      height: 320,
      padding,
      renderable: true,
    })
  })

  it('marks a body smaller than its padding as non-renderable', () => {
    expect(createPlotLayout(60, 40).renderable).toBe(false)
  })
})

describe('x coordinate model', () => {
  it('maps irregular dates by elapsed time and selects the earlier equal-distance value', () => {
    const values = [
      new Date('2026-07-09T22:00:00'),
      new Date('2026-07-09T00:00:00'),
      new Date('2026-07-09T20:00:00'),
    ]
    const model = createXCoordinateModel(values)

    expect(getXPositionPercent(model, new Date('2026-07-09T20:00:00'))).toBeCloseTo((20 / 22) * 100)
    expect(findNearestXValue(model, new Date('2026-07-09T10:00:00').getTime())).toEqual(
      new Date('2026-07-09T00:00:00'),
    )
  })

  it('centers one unique x value and gives G2 a non-zero domain', () => {
    const model = createXCoordinateModel([new Date('2026-07-09T00:00:00')])

    expect(getXPositionPercent(model, new Date('2026-07-09T00:00:00'))).toBe(50)
    expect(model.g2Domain).toHaveLength(2)
  })

  it('uses explicit point and band scale modes for categorical values', () => {
    const values = ['a', 'b', 'c']
    const pointModel = createXCoordinateModel(values, 'point')
    const bandModel = createXCoordinateModel(values, 'band')

    expect(pointModel.scaleType).toBe('point')
    expect(getXPositionPercent(pointModel, 'b')).toBe(50)
    expect(bandModel.scaleType).toBe('band')
    expect(getXPositionPercent(bandModel, 'b')).toBeCloseTo(50)
  })

  it('selects responsive automatic ticks without duplicates', () => {
    expect(selectAutomaticXTicks(['a'], false)).toEqual(['a'])
    expect(selectAutomaticXTicks(['a', 'b'], false)).toEqual(['a', 'b'])
    expect(selectAutomaticXTicks(['a', 'b', 'c', 'd'], false)).toEqual(['a', 'b', 'd'])
    expect(selectAutomaticXTicks(['a', 'b', 'c', 'd'], true)).toEqual(['a', 'd'])
  })
})

describe('tooltip placement', () => {
  it('uses measured dimensions and flips left at the right edge', () => {
    expect(placeTooltip({
      bodyWidth: 343,
      bodyHeight: 320,
      anchorX: 330,
      anchorY: 160,
      tooltipWidth: 220,
      tooltipHeight: 140,
    })).toEqual({
      visible: true,
      left: 98,
      top: 90,
      maxWidth: 327,
      maxHeight: 304,
    })
  })

  it('prefers the right side when it fits', () => {
    expect(placeTooltip({
      bodyWidth: 343,
      bodyHeight: 320,
      anchorX: 56,
      anchorY: 160,
      tooltipWidth: 220,
      tooltipHeight: 140,
    }).left).toBe(68)
  })

  it('chooses the larger side and clamps when neither side fits', () => {
    expect(placeTooltip({
      bodyWidth: 240,
      bodyHeight: 160,
      anchorX: 80,
      anchorY: 152,
      tooltipWidth: 150,
      tooltipHeight: 100,
    })).toEqual({
      visible: true,
      left: 82,
      top: 52,
      maxWidth: 224,
      maxHeight: 144,
    })
  })

  it('uses the right side for an equal-space tie', () => {
    expect(placeTooltip({
      bodyWidth: 300,
      bodyHeight: 200,
      anchorX: 150,
      anchorY: 192,
      tooltipWidth: 280,
      tooltipHeight: 80,
    }).left).toBe(12)
  })

  it('hides the tooltip when the inset leaves no available area', () => {
    expect(placeTooltip({
      bodyWidth: 16,
      bodyHeight: 320,
      anchorX: 8,
      anchorY: 100,
      tooltipWidth: 10,
      tooltipHeight: 10,
    }).visible).toBe(false)
  })
})
