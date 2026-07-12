export type PlotPadding = {
  left: number
  right: number
  top: number
  bottom: number
}

export type PlotLayout = {
  width: number
  height: number
  padding: PlotPadding
  plotLeft: number
  plotRight: number
  plotTop: number
  plotBottom: number
  plotWidth: number
  plotHeight: number
  renderable: boolean
  narrow: boolean
}

export type XScaleType = 'linear' | 'time' | 'point' | 'band'

type ComparableValue = {
  raw: unknown
  value: number
  kind: 'date' | 'number'
}

export type XCoordinateModel =
  | {
      kind: 'continuous'
      scaleType: 'time' | 'linear'
      values: unknown[]
      comparableValues: ComparableValue[]
      min: number
      max: number
      g2Domain: [number | Date, number | Date]
    }
  | {
      kind: 'categorical'
      scaleType: 'point' | 'band'
      values: unknown[]
      g2Domain: unknown[]
    }

export const createPlotLayout = (width: number, height: number): PlotLayout => {
  const safeWidth = Math.max(0, width)
  const safeHeight = Math.max(0, height)
  const narrow = safeWidth < 480
  const padding = narrow
    ? { left: 48, right: 12, top: 12, bottom: 38 }
    : { left: 56, right: 24, top: 12, bottom: 42 }
  const plotWidth = safeWidth - padding.left - padding.right
  const plotHeight = safeHeight - padding.top - padding.bottom

  return {
    width: safeWidth,
    height: safeHeight,
    padding,
    plotLeft: padding.left,
    plotRight: safeWidth - padding.right,
    plotTop: padding.top,
    plotBottom: safeHeight - padding.bottom,
    plotWidth: Math.max(0, plotWidth),
    plotHeight: Math.max(0, plotHeight),
    renderable: plotWidth > 0 && plotHeight > 0,
    narrow,
  }
}

const toComparable = (value: unknown): ComparableValue | null => {
  if (value instanceof Date && !Number.isNaN(value.getTime())) {
    return { raw: value, value: value.getTime(), kind: 'date' }
  }

  if (typeof value === 'number' && Number.isFinite(value)) {
    return { raw: value, value, kind: 'number' }
  }

  if (typeof value === 'string') {
    const date = new Date(value)
    if (!Number.isNaN(date.getTime())) {
      return { raw: value, value: date.getTime(), kind: 'date' }
    }
  }

  return null
}

const valueKey = (value: unknown): string => {
  const comparable = toComparable(value)
  if (comparable) return `${comparable.kind}:${comparable.value}`
  return `raw:${String(value)}`
}

const uniqueValues = (values: unknown[]): unknown[] => {
  const seen = new Set<string>()
  return values.filter((value) => {
    const key = valueKey(value)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

export const createXCoordinateModel = (
  inputValues: unknown[],
  requestedScale?: XScaleType,
): XCoordinateModel => {
  const values = uniqueValues(inputValues)
  const comparableValues = values
    .map((raw) => toComparable(raw))
    .filter((value): value is ComparableValue => value !== null)
  const canUseContinuous = values.length > 0 && comparableValues.length === values.length

  if (canUseContinuous && requestedScale !== 'point' && requestedScale !== 'band') {
    const scaleType = requestedScale === 'linear'
      ? 'linear'
      : comparableValues.some((item) => item.kind === 'date')
        ? 'time'
        : 'linear'
    const sortedValues = [...comparableValues].sort((a, b) => a.value - b.value)
    const min = sortedValues[0].value
    const max = sortedValues.at(-1)?.value ?? min
    const domainMin = min === max ? min - 0.5 : min
    const domainMax = min === max ? max + 0.5 : max

    return {
      kind: 'continuous',
      scaleType,
      values,
      comparableValues: sortedValues,
      min,
      max,
      g2Domain: scaleType === 'time'
        ? [new Date(domainMin), new Date(domainMax)]
        : [domainMin, domainMax],
    }
  }

  return {
    kind: 'categorical',
    scaleType: requestedScale === 'band' ? 'band' : 'point',
    values,
    g2Domain: values,
  }
}

export const isSameXValue = (left: unknown, right: unknown): boolean => {
  const leftComparable = toComparable(left)
  const rightComparable = toComparable(right)
  if (leftComparable && rightComparable) return leftComparable.value === rightComparable.value
  return String(left) === String(right)
}

export const getXPositionPercent = (
  model: XCoordinateModel,
  value: unknown,
): number => {
  if (model.kind === 'continuous') {
    const comparable = toComparable(value)
    if (!comparable || model.max === model.min) return 50
    return Math.min(100, Math.max(0, ((comparable.value - model.min) / (model.max - model.min)) * 100))
  }

  const index = model.values.findIndex((candidate) => isSameXValue(candidate, value))
  if (model.values.length <= 1 || index < 0) return 50
  return model.scaleType === 'band'
    ? ((index + 0.5) / model.values.length) * 100
    : (index / (model.values.length - 1)) * 100
}

export const findNearestXValue = (
  model: XCoordinateModel,
  target: number,
): unknown => {
  if (model.kind === 'categorical') {
    const index = Math.min(Math.max(Math.round(target), 0), Math.max(model.values.length - 1, 0))
    return model.values[index]
  }

  return model.comparableValues.reduce((nearest, candidate) => {
    const distance = Math.abs(candidate.value - target)
    const nearestDistance = Math.abs(nearest.value - target)
    return distance < nearestDistance ? candidate : nearest
  }, model.comparableValues[0])?.raw
}

export const selectAutomaticXTicks = <T>(values: T[], narrow: boolean): T[] => {
  if (values.length <= 2) return values

  const indexes = narrow
    ? [0, values.length - 1]
    : [0, Math.floor((values.length - 1) / 2), values.length - 1]
  return [...new Set(indexes)].map((index) => values[index])
}

export type TooltipPlacementInput = {
  bodyWidth: number
  bodyHeight: number
  anchorX: number
  anchorY: number
  tooltipWidth: number
  tooltipHeight: number
}

export type TooltipPlacement = {
  visible: boolean
  left: number
  top: number
  maxWidth: number
  maxHeight: number
}

const EDGE_INSET = 8
const ANCHOR_GAP = 12

const clamp = (value: number, min: number, max: number): number =>
  Math.min(Math.max(value, min), max)

export const placeTooltip = ({
  bodyWidth,
  bodyHeight,
  anchorX,
  anchorY,
  tooltipWidth,
  tooltipHeight,
}: TooltipPlacementInput): TooltipPlacement => {
  const maxWidth = Math.max(0, bodyWidth - EDGE_INSET * 2)
  const maxHeight = Math.max(0, bodyHeight - EDGE_INSET * 2)

  if (maxWidth <= 0 || maxHeight <= 0) {
    return { visible: false, left: 0, top: 0, maxWidth, maxHeight }
  }

  const safeTooltipWidth = Math.max(0, Math.min(tooltipWidth, maxWidth))
  const safeTooltipHeight = Math.max(0, Math.min(tooltipHeight, maxHeight))
  const rightSpace = bodyWidth - EDGE_INSET - (anchorX + ANCHOR_GAP)
  const leftSpace = anchorX - ANCHOR_GAP - EDGE_INSET
  const preferredLeft = rightSpace >= safeTooltipWidth
    ? anchorX + ANCHOR_GAP
    : leftSpace >= safeTooltipWidth
      ? anchorX - ANCHOR_GAP - safeTooltipWidth
      : rightSpace >= leftSpace
        ? anchorX + ANCHOR_GAP
        : anchorX - ANCHOR_GAP - safeTooltipWidth
  const maxLeft = Math.max(EDGE_INSET, bodyWidth - EDGE_INSET - safeTooltipWidth)
  const maxTop = Math.max(EDGE_INSET, bodyHeight - EDGE_INSET - safeTooltipHeight)

  return {
    visible: true,
    left: clamp(preferredLeft, EDGE_INSET, maxLeft),
    top: clamp(anchorY - safeTooltipHeight / 2, EDGE_INSET, maxTop),
    maxWidth,
    maxHeight,
  }
}
