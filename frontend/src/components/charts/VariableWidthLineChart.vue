<template>
  <section class="vw-line">
    <header v-if="title || showLegend" class="vw-line__header">
      <h3 v-if="title" class="vw-line__title">
        {{ title }}
      </h3>

      <div v-if="showLegend && legendItems.length" class="vw-line__legend">
        <div
          v-for="item in legendItems"
          :key="item.name"
          class="vw-line__legend-item"
        >
          <span
            class="vw-line__legend-marker"
            :style="{ backgroundColor: item.color }"
          />
          <span>{{ item.name }}</span>
        </div>
      </div>
    </header>

    <div
      ref="bodyRef"
      class="vw-line__body"
      :style="bodyStyle"
      @pointermove="handleTooltipMove"
      @pointerleave="hideTooltip"
    >
      <div v-if="!isEmpty && layoutSnapshot.renderable" class="vw-line__grid" aria-hidden="true">
        <div
          v-for="item in gridTickItems"
          :key="item.key"
          class="vw-line__grid-row"
          :style="{ top: `${item.top}%` }"
        >
          <span class="vw-line__grid-label">{{ item.label }}</span>
          <span class="vw-line__grid-line" />
        </div>
      </div>

      <div ref="containerRef" class="vw-line__chart" />

      <div v-if="!isEmpty && layoutSnapshot.renderable" class="vw-line__x-axis" aria-hidden="true">
        <span
          v-for="item in xTickItems"
          :key="item.key"
          :class="['vw-line__x-label', `vw-line__x-label--${item.align}`]"
          :style="{ left: `${item.left}%` }"
        >
          {{ item.label }}
        </span>
      </div>

      <div
        v-if="tooltipState.visible"
        class="vw-line__tooltip-crosshair"
        :style="{ left: `${tooltipState.x}px` }"
        aria-hidden="true"
      />

      <div
        v-if="tooltipState.visible && tooltipContent"
        ref="tooltipRef"
        class="vw-line__tooltip"
        :style="tooltipStyle"
        @pointermove.stop
        v-html="tooltipContent"
      />

      <div v-if="isEmpty" class="vw-line__empty">
        {{ emptyText }}
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { Chart } from '@antv/g2'
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from 'vue'
import {
  createPlotLayout,
  createXCoordinateModel,
  findNearestXValue,
  getXPositionPercent,
  isSameXValue,
  placeTooltip,
  selectAutomaticXTicks,
  type PlotLayout,
  type XCoordinateModel,
} from './variableWidthLineChartGeometry'

defineOptions({
  name: 'VariableWidthLineChart',
})

type RawDatum = Record<string, unknown>
type FieldGetter = string | ((datum: RawDatum, index: number) => unknown)
type ScaleType = 'linear' | 'time' | 'point' | 'band'
type StrokeEffect = 'staccato' | 'smooth'
type ComparableXKind = 'date' | 'date-string' | 'number'
type ComparableX = { kind: ComparableXKind; value: number }

type InnerDatum = RawDatum & {
  __vw_x__: unknown
  __vw_y__: number
  __vw_visual_size__: number
  __vw_color__: string
  __vw_series__: string
  __vw_staccato__?: boolean
  __vw_staccato_index__?: string
  __vw_tooltip_x__?: unknown
}

const X_KEY = '__vw_x__'
const Y_KEY = '__vw_y__'
const SIZE_KEY = '__vw_visual_size__'
const COLOR_KEY = '__vw_color__'
const SERIES_KEY = '__vw_series__'

const DEFAULT_COLORS = [
  '#2563eb',
  '#059669',
  '#f97316',
  '#14b8a6',
  '#7c3aed',
]

const props = withDefaults(defineProps<{
  title?: string
  data?: RawDatum[]
  xField: FieldGetter
  yField: FieldGetter
  colorField: FieldGetter
  height?: number
  colors?: string[]
  yDomain?: [number, number]
  yTicks?: number[]
  xTicks?: unknown[]
  xScaleType?: ScaleType
  minLineWidth?: number
  maxLineWidth?: number
  showLegend?: boolean
  showEndDot?: boolean
  endDotSize?: number
  brushEffect?: boolean
  strokeEffect?: StrokeEffect
  emptyText?: string
  formatX?: (value: unknown) => string
  formatY?: (value: unknown) => string
  tooltipHtml?: (title: unknown, items?: unknown[]) => string
}>(), {
  title: '',
  data: () => [],
  height: 192,
  colors: () => ['#2563eb', '#059669', '#f97316', '#14b8a6', '#7c3aed'],
  minLineWidth: 1.2,
  maxLineWidth: 6.5,
  showLegend: true,
  showEndDot: false,
  endDotSize: 10,
  brushEffect: true,
  strokeEffect: 'staccato',
  emptyText: '暂无数据',
})

const bodyRef = ref<HTMLDivElement | null>(null)
const containerRef = ref<HTMLDivElement | null>(null)
const chartRef = shallowRef<Chart | null>(null)
const tooltipRef = ref<HTMLDivElement | null>(null)
const layoutSnapshot = ref<PlotLayout>(createPlotLayout(0, props.height))
const lastRenderableLayout = ref<PlotLayout | null>(null)
const tooltipState = ref({
  visible: false,
  x: 0,
  left: 0,
  top: 0,
  maxWidth: 0,
  maxHeight: 0,
  title: undefined as unknown,
})
let tooltipTicket = 0
let resizeFrame: number | null = null
let resizeObserver: ResizeObserver | null = null
let renderTicket = 0

const getFieldValue = (datum: RawDatum, field: FieldGetter, index: number): unknown =>
  typeof field === 'function' ? field(datum, index) : datum[field]

const toFiniteNumber = (value: unknown): number | null => {
  if (value === null || value === undefined || value === '') return null

  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : null
}

const normalizedData = computed<InnerDatum[]>(() => {
  const result: InnerDatum[] = []
  const segmentMap = new Map<string, number>()

  props.data.forEach((raw, index) => {
    const colorRaw = getFieldValue(raw, props.colorField, index)
    const colorName = colorRaw === null || colorRaw === undefined || colorRaw === ''
      ? 'default'
      : String(colorRaw)

    if (!segmentMap.has(colorName)) {
      segmentMap.set(colorName, 0)
    }

    const x = getFieldValue(raw, props.xField, index)
    const y = toFiniteNumber(getFieldValue(raw, props.yField, index))
    const isBreakPoint = x === null || x === undefined || x === '' || y === null

    if (isBreakPoint) {
      segmentMap.set(colorName, (segmentMap.get(colorName) ?? 0) + 1)
      return
    }

    const segmentNo = segmentMap.get(colorName) ?? 0

    result.push({
      ...raw,
      [X_KEY]: x,
      [Y_KEY]: y,
      [SIZE_KEY]: toVisualSize(y),
      [COLOR_KEY]: colorName,
      [SERIES_KEY]: `${colorName}__${segmentNo}`,
      __vw_staccato__: false,
      __vw_tooltip_x__: x,
    })
  })

  return result
})

const isEmpty = computed(() => normalizedData.value.length === 0)

const renderData = computed(() =>
  props.strokeEffect === 'staccato'
    ? buildStaccatoData(normalizedData.value)
    : normalizedData.value
)

const tooltipContent = computed(() => {
  if (!tooltipState.value.visible || !props.tooltipHtml) return ''
  return props.tooltipHtml(tooltipState.value.title)
})

const bodyStyle = computed(() => ({
  height: `${props.height}px`,
  '--vw-plot-left': `${layoutSnapshot.value.padding.left}px`,
  '--vw-plot-right': `${layoutSnapshot.value.padding.right}px`,
  '--vw-plot-top': `${layoutSnapshot.value.padding.top}px`,
  '--vw-plot-bottom': `${layoutSnapshot.value.padding.bottom}px`,
}))

const tooltipStyle = computed(() => ({
  left: `${tooltipState.value.left}px`,
  top: `${tooltipState.value.top}px`,
  maxWidth: `${tooltipState.value.maxWidth}px`,
  maxHeight: `${tooltipState.value.maxHeight}px`,
}))

const seriesNames = computed(() => {
  const seen = new Set<string>()
  const names: string[] = []

  normalizedData.value.forEach((item) => {
    const name = item[COLOR_KEY]
    if (!seen.has(name)) {
      seen.add(name)
      names.push(name)
    }
  })

  return names
})

const colorRange = computed(() => {
  const colors = props.colors.length ? props.colors : DEFAULT_COLORS
  return seriesNames.value.map((_, index) => colors[index % colors.length])
})

const legendItems = computed(() =>
  seriesNames.value.map((name, index) => ({
    name,
    color: colorRange.value[index],
  }))
)

const endDotData = computed(() => {
  const lastPointMap = new Map<string, InnerDatum>()

  normalizedData.value.forEach((item) => {
    lastPointMap.set(item[COLOR_KEY], item)
  })

  return Array.from(lastPointMap.values())
})

const isolatedPointData = computed(() => {
  const counts = new Map<string, number>()

  normalizedData.value.forEach((item) => {
    const key = item[SERIES_KEY]
    counts.set(key, (counts.get(key) ?? 0) + 1)
  })

  return normalizedData.value.filter((item) => (counts.get(item[SERIES_KEY]) ?? 0) === 1)
})

const yExtent = computed(() => {
  const values = normalizedData.value.map((item) => item[Y_KEY])
  const min = props.yDomain?.[0] ?? 0
  const max = props.yDomain?.[1] ?? Math.max(...values, 1)

  if (max === min) {
    return { min, max: min + 1 }
  }

  return { min, max }
})

const toVisualSize = (value: number): number =>
  Math.pow(Math.max(value, 0), 0.62)

const sizeExtent = computed(() => {
  if (props.yDomain) {
    const min = toVisualSize(props.yDomain[0])
    const max = toVisualSize(props.yDomain[1])
    return max === min ? { min, max: min + 1 } : { min, max }
  }

  const values = normalizedData.value.map((item) => item[SIZE_KEY])
  const min = Math.min(...values, 0)
  const max = Math.max(...values, 1)
  return max === min ? { min, max: min + 1 } : { min, max }
})

const gridTickValues = computed(() => {
  if (props.yTicks?.length) return props.yTicks

  const { min, max } = yExtent.value
  const steps = 4
  return Array.from({ length: steps + 1 }, (_, index) => min + ((max - min) * index) / steps)
})

const gridTickItems = computed(() => {
  const { min, max } = yExtent.value
  const span = max - min || 1

  return gridTickValues.value.map((value) => ({
    key: String(value),
    label: props.formatY ? props.formatY(value) : String(value),
    top: 100 - ((value - min) / span) * 100,
  }))
})

function toInterpolatableX(value: unknown): ComparableX | null {
  if (value instanceof Date) return { kind: 'date', value: value.getTime() }
  if (typeof value === 'number' && Number.isFinite(value)) return { kind: 'number', value }
  if (typeof value === 'string') {
    const date = new Date(value)
    if (!Number.isNaN(date.getTime())) {
      return { kind: 'date-string', value: date.getTime() }
    }
  }
  return null
}

const xValues = computed(() => {
  const seen = new Set<string>()
  const values: unknown[] = []

  normalizedData.value.forEach((item) => {
    const value = item[X_KEY]
    const key = value instanceof Date ? String(value.getTime()) : String(value)

    if (!seen.has(key)) {
      seen.add(key)
      values.push(value)
    }
  })

  return values
})

const xCoordinateModel = computed<XCoordinateModel>(() =>
  createXCoordinateModel(xValues.value, props.xScaleType),
)

const xDisplayTicks = computed(() => {
  if (props.xTicks?.length) return props.xTicks

  return selectAutomaticXTicks(xValues.value, layoutSnapshot.value.narrow)
})

const xTickItems = computed(() => {
  const allValues = xValues.value

  return xDisplayTicks.value.map((value, index) => {
    const foundIndex = allValues.findIndex((candidate) => isSameXValue(candidate, value))
    const left = foundIndex >= 0
      ? getXPositionPercent(xCoordinateModel.value, value)
      : (index / Math.max(xDisplayTicks.value.length - 1, 1)) * 100

    return {
      key: xTickKey(value, index),
      label: props.formatX ? props.formatX(value) : String(value),
      left,
      align: index === 0 ? 'start' : index === xDisplayTicks.value.length - 1 ? 'end' : 'middle',
    }
  })
})

const xTickKey = (value: unknown, index: number): string =>
  `${value instanceof Date ? value.getTime() : String(value)}-${index}`

const invalidateTooltip = () => {
  tooltipTicket += 1
  tooltipState.value = {
    ...tooltipState.value,
    visible: false,
  }
}

const positionTooltip = async (ticket: number, bodyRect: DOMRect) => {
  await nextTick()

  if (ticket !== tooltipTicket || !tooltipState.value.visible || !tooltipRef.value) return

  const tooltipRect = tooltipRef.value.getBoundingClientRect()
  const placement = placeTooltip({
    bodyWidth: bodyRect.width,
    bodyHeight: bodyRect.height,
    anchorX: tooltipState.value.x,
    anchorY: tooltipState.value.top,
    tooltipWidth: tooltipRect.width || tooltipRef.value.offsetWidth,
    tooltipHeight: tooltipRect.height || tooltipRef.value.offsetHeight,
  })

  if (ticket !== tooltipTicket) return

  tooltipState.value = {
    ...tooltipState.value,
    visible: placement.visible,
    left: placement.left,
    top: placement.top,
    maxWidth: placement.maxWidth,
    maxHeight: placement.maxHeight,
  }
}

const handleTooltipMove = (event: PointerEvent) => {
  if (!props.tooltipHtml || isEmpty.value || !layoutSnapshot.value.renderable) return

  const target = event.currentTarget
  if (!(target instanceof HTMLElement)) return

  const values = xValues.value
  if (!values.length) return

  const rect = target.getBoundingClientRect()
  const layout = layoutSnapshot.value
  const localX = clamp(event.clientX - rect.left, layout.plotLeft, layout.plotRight)
  const localY = clamp(event.clientY - rect.top, layout.plotTop, layout.plotBottom)
  const progress = layout.plotWidth > 0
    ? clamp((localX - layout.plotLeft) / layout.plotWidth, 0, 1)
    : 0
  const model = xCoordinateModel.value
  const title = model.kind === 'continuous'
    ? findNearestXValue(model, model.min + progress * (model.max - model.min))
    : values[clamp(Math.round(progress * (values.length - 1)), 0, values.length - 1)]
  const snappedX = layout.plotLeft + (
    getXPositionPercent(model, title) / 100
  ) * layout.plotWidth
  const ticket = ++tooltipTicket

  tooltipState.value = {
    ...tooltipState.value,
    visible: true,
    x: snappedX,
    left: snappedX + 12,
    top: localY,
    maxWidth: Math.max(0, rect.width - 16),
    maxHeight: Math.max(0, rect.height - 16),
    title,
  }

  void positionTooltip(ticket, rect)
}

const hideTooltip = () => {
  invalidateTooltip()
}

const buildStaccatoData = (data: InnerDatum[]): InnerDatum[] => {
  const groups = new Map<string, InnerDatum[]>()
  const groupOrder: string[] = []

  data.forEach((item) => {
    const key = item[SERIES_KEY]

    if (!groups.has(key)) {
      groups.set(key, [])
      groupOrder.push(key)
    }

    groups.get(key)?.push(item)
  })

  return groupOrder.flatMap((key) => {
    const points = groups.get(key) ?? []
    if (points.length < 2) return points

    const result: InnerDatum[] = [{ ...points[0], __vw_staccato__: false }]

    for (let index = 1; index < points.length; index += 1) {
      const previous = points[index - 1]
      const next = points[index]
      const segmentPoints = buildStaccatoSegment(previous, next, index - 1)

      result.push(...segmentPoints, { ...next, __vw_staccato__: false })
    }

    return result
  })
}

const buildStaccatoSegment = (
  previous: InnerDatum,
  next: InnerDatum,
  segmentIndex: number,
): InnerDatum[] => {
  const previousX = toInterpolatableX(previous[X_KEY])
  const nextX = toInterpolatableX(next[X_KEY])

  if (!previousX || !nextX || previousX.kind !== nextX.kind || nextX.value <= previousX.value) {
    return []
  }

  const yDelta = next[Y_KEY] - previous[Y_KEY]
  if (Math.abs(yDelta) < 0.000001) {
    return []
  }

  const direction = Math.sign(yDelta)
  const { min, max } = yExtent.value
  const ySpan = Math.max(max - min, 1)
  const amplitude = Math.min(Math.abs(yDelta) * 0.08, ySpan * 0.018)
  const pattern = [
    { t: 0.24, p: 0.20, kick: 0.05 },
    { t: 0.50, p: 0.46, kick: -0.06 },
    { t: 0.76, p: 0.74, kick: 0.04 },
  ]

  return pattern.map(({ t, p, kick }, index) => {
    const x = interpolateX(previousX, nextX, t)
    const y = clamp(
      previous[Y_KEY] + yDelta * p + direction * amplitude * kick,
      min,
      max,
    )

    return {
      ...next,
      [X_KEY]: x,
      [Y_KEY]: y,
      [SIZE_KEY]: toVisualSize(y),
      [COLOR_KEY]: next[COLOR_KEY],
      [SERIES_KEY]: next[SERIES_KEY],
      __vw_staccato__: true,
      __vw_tooltip_x__: t < 0.5 ? previous[X_KEY] : next[X_KEY],
      __vw_staccato_index__: `${next[SERIES_KEY]}-${segmentIndex}-${index}`,
    }
  })
}

const interpolateX = (
  previous: ComparableX,
  next: ComparableX,
  progress: number,
): Date | number | string => {
  const value = previous.value + (next.value - previous.value) * progress
  if (previous.kind === 'date') return new Date(value)
  if (previous.kind === 'date-string') return new Date(value).toISOString()
  return value
}

const clamp = (value: number, min: number, max: number): number =>
  Math.min(Math.max(value, min), max)

const buildViewScale = () => {
  const xModel = xCoordinateModel.value
  const scale: Record<string, unknown> = {
    color: {
      domain: seriesNames.value,
      range: colorRange.value,
    },
    y: {
      domain: [yExtent.value.min, yExtent.value.max],
      nice: false,
    },
    x: xModel.kind === 'continuous'
      ? {
          type: xModel.scaleType,
          domain: xModel.g2Domain,
          nice: false,
        }
      : {
          type: xModel.scaleType,
          domain: xModel.g2Domain,
          ...(xModel.scaleType === 'band'
            ? { paddingInner: 0, paddingOuter: 0 }
            : { padding: 0 }),
        },
  }

  if (props.yTicks?.length) {
    scale.y = {
      ...(scale.y as Record<string, unknown>),
      tickMethod: () => props.yTicks,
    }
  }

  return scale
}

const buildLineScale = (
  minWidthOffset = 0,
  maxWidthOffset = 0,
) => {
  const size: Record<string, unknown> = {
    type: 'linear',
    range: [
      props.minLineWidth + minWidthOffset,
      props.maxLineWidth + maxWidthOffset,
    ],
  }

  if (props.yDomain) {
    size.domain = [sizeExtent.value.min, sizeExtent.value.max]
  }

  return { size }
}

const createTrailLayer = ({
  minWidthOffset = 0,
  maxWidthOffset = 0,
  opacity = 1,
}: {
  minWidthOffset?: number
  maxWidthOffset?: number
  opacity?: number
} = {}) => ({
  type: 'line',
  encode: {
    x: X_KEY,
    y: Y_KEY,
    color: COLOR_KEY,
    series: SERIES_KEY,
    size: SIZE_KEY,
    shape: 'trail',
  },
  scale: buildLineScale(minWidthOffset, maxWidthOffset),
  axis: false,
  style: {
    lineCap: 'round',
    lineJoin: 'round',
    opacity,
  },
  tooltip: false,
  legend: false,
})

const buildTooltipInteraction = () => {
  return {}
}

const destroyChart = () => {
  if (!chartRef.value) return

  chartRef.value.destroy()
  chartRef.value = null
}

const renderChart = async () => {
  const currentTicket = ++renderTicket

  await nextTick()

  if (currentTicket !== renderTicket) return

  const container = containerRef.value
  if (!container) return

  destroyChart()
  container.innerHTML = ''

  if (isEmpty.value || !layoutSnapshot.value.renderable) return

  const children: any[] = props.brushEffect
      ? [
        createTrailLayer({ minWidthOffset: 2, maxWidthOffset: 2.4, opacity: 0.08 }),
        createTrailLayer({ minWidthOffset: 0.9, maxWidthOffset: 1.1, opacity: 0.16 }),
        createTrailLayer({ opacity: 0.94 }),
      ]
    : [
        createTrailLayer(),
      ]

  if (props.showEndDot) {
    children.push({
      type: 'point',
      data: endDotData.value,
      encode: {
        x: X_KEY,
        y: Y_KEY,
        color: COLOR_KEY,
        shape: 'point',
        size: props.endDotSize,
      },
      style: {
        stroke: 'transparent',
        lineWidth: 0,
        fillOpacity: 1,
      },
      tooltip: false,
      legend: false,
      axis: false,
    })
  } else if (isolatedPointData.value.length) {
    children.push({
      type: 'point',
      data: isolatedPointData.value,
      encode: {
        x: X_KEY,
        y: Y_KEY,
        color: COLOR_KEY,
        shape: 'point',
        size: 5,
      },
      style: {
        stroke: '#ffffff',
        lineWidth: 1.5,
        fillOpacity: 0.96,
      },
      tooltip: false,
      legend: false,
      axis: false,
    })
  }

  const chart = new Chart({
    container,
    autoFit: true,
    height: props.height,
  })

  chart.options({
    type: 'view',
    autoFit: true,
    height: props.height,
    data: renderData.value,
    // G2 applies a 16px theme margin by default. Keep every part of this
    // component on the explicit PlotLayout so lines, ticks and hover overlays
    // share exactly the same coordinate system (including both endpoints).
    margin: 0,
    inset: 0,
    paddingLeft: layoutSnapshot.value.padding.left,
    paddingRight: layoutSnapshot.value.padding.right,
    paddingTop: layoutSnapshot.value.padding.top,
    paddingBottom: layoutSnapshot.value.padding.bottom,
    scale: buildViewScale(),
    axis: false,
    legend: false,
    interaction: buildTooltipInteraction(),
    animate: false,
    children,
  } as any)

  chartRef.value = chart

  await chart.render()
}

let layoutTicket = 0

const readBodyRect = (): DOMRect | null => {
  const body = bodyRef.value
  if (!body) return null
  return body.getBoundingClientRect()
}

const applyMeasuredLayout = async (width: number, height: number) => {
  const ticket = ++layoutTicket
  invalidateTooltip()

  const nextLayout = createPlotLayout(width, height || props.height)
  const previousRenderable = lastRenderableLayout.value
  layoutSnapshot.value = nextLayout

  if (!nextLayout.renderable) {
    destroyChart()
    return
  }

  const needsRebuild = !chartRef.value || previousRenderable?.narrow !== nextLayout.narrow
  lastRenderableLayout.value = nextLayout

  if (needsRebuild) {
    await renderChart()
    if (ticket !== layoutTicket) return
    if (chartRef.value) {
      await chartRef.value.forceFit()
      if (ticket !== layoutTicket) return
    }
  } else if (chartRef.value) {
    await nextTick()
    if (ticket !== layoutTicket) return
    await chartRef.value.forceFit()
    if (ticket !== layoutTicket) return
  }
}

const measureBody = () => {
  const rect = readBodyRect()
  if (!rect) return
  void applyMeasuredLayout(rect.width, rect.height)
}

const scheduleLayoutUpdate = () => {
  invalidateTooltip()
  if (resizeFrame !== null) {
    cancelAnimationFrame(resizeFrame)
  }
  resizeFrame = requestAnimationFrame(() => {
    resizeFrame = null
    measureBody()
  })
}

onMounted(() => {
  measureBody()

  if (typeof ResizeObserver !== 'undefined' && bodyRef.value) {
    resizeObserver = new ResizeObserver(() => {
      scheduleLayoutUpdate()
    })
    resizeObserver.observe(bodyRef.value)
  } else {
    window.addEventListener('resize', scheduleLayoutUpdate)
  }
})

watch(
  () => [
    normalizedData.value,
    props.height,
    props.colors,
    props.yDomain,
    props.yTicks,
    props.xTicks,
    props.xScaleType,
    props.minLineWidth,
    props.maxLineWidth,
    props.showEndDot,
    props.endDotSize,
    props.brushEffect,
    props.strokeEffect,
    props.tooltipHtml,
    props.formatX,
    props.formatY,
  ],
  () => {
    invalidateTooltip()
    void renderChart()
  },
  { deep: true }
)

onBeforeUnmount(() => {
  renderTicket += 1
  layoutTicket += 1
  invalidateTooltip()
  if (resizeFrame !== null) {
    cancelAnimationFrame(resizeFrame)
    resizeFrame = null
  }
  resizeObserver?.disconnect()
  resizeObserver = null
  window.removeEventListener('resize', scheduleLayoutUpdate)
  destroyChart()
})
</script>

<style scoped>
.vw-line {
  width: 100%;
  min-width: 0;
  max-width: 100%;
  background: transparent;
  --vw-title-text: #111827;
  --vw-title-surface: #dbeafe;
  --vw-body-text: #374151;
  --vw-muted-text: #6b7280;
  --vw-grid: #e5e7eb;
  --vw-crosshair: rgba(17, 24, 39, 0.48);
  --vw-tooltip-surface: #ffffff;
  --vw-tooltip-border: #d1d5db;
  --vw-tooltip-text: #374151;
  --vw-tooltip-value: #111827;
}

:global(.dark) .vw-line {
  --vw-title-text: #dbeafe;
  --vw-title-surface: rgba(37, 99, 235, 0.24);
  --vw-body-text: #d1d5db;
  --vw-muted-text: #9ca3af;
  --vw-grid: #374151;
  --vw-crosshair: rgba(209, 213, 219, 0.48);
  --vw-tooltip-surface: #111827;
  --vw-tooltip-border: #374151;
  --vw-tooltip-text: #d1d5db;
  --vw-tooltip-value: #f9fafb;
}

.vw-line__header {
  margin-bottom: 12px;
}

.vw-line__title {
  display: inline-block;
  margin: 0;
  padding: 3px 8px;
  color: var(--vw-title-text);
  background: var(--vw-title-surface);
  border-radius: 4px;
  font-size: 14px;
  font-weight: 700;
  line-height: 1.35;
}

.vw-line__legend {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 20px;
  margin-top: 12px;
}

.vw-line__legend-item {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--vw-body-text);
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
}

.vw-line__legend-marker {
  width: 14px;
  height: 3px;
  border-radius: 999px;
}

.vw-line__body {
  position: relative;
  width: 100%;
  min-width: 0;
  max-width: 100%;
}

.vw-line__tooltip-crosshair {
  position: absolute;
  top: var(--vw-plot-top);
  bottom: var(--vw-plot-bottom);
  z-index: 3;
  width: 1px;
  background: var(--vw-crosshair);
  pointer-events: none;
}

.vw-line__grid {
  position: absolute;
  inset: var(--vw-plot-top) var(--vw-plot-right) var(--vw-plot-bottom) var(--vw-plot-left);
  z-index: 0;
  pointer-events: none;
}

.vw-line__grid-row {
  position: absolute;
  right: 0;
  left: 0;
  display: flex;
  align-items: center;
  transform: translateY(-50%);
}

.vw-line__grid-label {
  position: absolute;
  right: calc(100% + 8px);
  color: var(--vw-muted-text);
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
}

.vw-line__grid-line {
  width: 100%;
  border-top: 1px dashed var(--vw-grid);
}

.vw-line__chart {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  max-width: 100%;
  overflow: hidden;
}

.vw-line__x-axis {
  position: absolute;
  right: var(--vw-plot-right);
  bottom: 12px;
  left: var(--vw-plot-left);
  z-index: 2;
  height: 18px;
  pointer-events: none;
}

.vw-line__x-label {
  position: absolute;
  max-width: 100%;
  overflow: hidden;
  color: var(--vw-muted-text);
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
  transform: translateX(-50%);
}

.vw-line__x-label--start {
  transform: translateX(0);
}

.vw-line__x-label--end {
  transform: translateX(-100%);
}

.vw-line__tooltip {
  position: absolute;
  z-index: 4;
  box-sizing: border-box;
  width: max-content;
  overflow-y: auto;
  background: var(--vw-tooltip-surface);
  border: 1px solid var(--vw-tooltip-border);
  border-radius: 7px;
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.22);
  color: var(--vw-tooltip-text);
  padding: 10px 12px;
  /* The tooltip is visual-only. Let pointer moves continue to reach the chart
     body even when the tooltip happens to pass underneath the cursor. */
  pointer-events: none;
  font-size: 12px;
  line-height: 1.35;
}

:deep(.token-trend-tooltip) {
  min-width: 0;
}

:deep(.token-trend-tooltip__title) {
  margin-bottom: 8px;
  color: var(--vw-muted-text);
  font-size: 12px;
  font-weight: 600;
}

:deep(.token-trend-tooltip__rows) {
  display: grid;
  gap: 6px;
}

:deep(.token-trend-tooltip__row) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

:deep(.token-trend-tooltip__label) {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  color: var(--vw-tooltip-text);
}

:deep(.token-trend-tooltip__marker) {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: 999px;
  background: var(--token-trend-marker);
}

:deep(.token-trend-tooltip__value) {
  flex: 0 0 auto;
  color: var(--vw-tooltip-value);
  font-weight: 700;
}

:deep(.token-trend-tooltip__summary) {
  display: grid;
  gap: 4px;
  margin-top: 9px;
  padding-top: 8px;
  border-top: 1px solid var(--vw-tooltip-border);
  color: var(--vw-tooltip-value);
  font-weight: 700;
}

:deep(.token-trend-tooltip__fallback) {
  color: var(--vw-tooltip-text);
}

.vw-line__empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--vw-muted-text);
  font-size: 14px;
  pointer-events: none;
}
</style>
