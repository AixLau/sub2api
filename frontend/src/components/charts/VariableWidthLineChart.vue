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
      class="vw-line__body"
      :style="{ height: `${height}px` }"
      @mousemove="handleTooltipMove"
      @mouseleave="hideTooltip"
    >
      <div v-if="!isEmpty" class="vw-line__grid" aria-hidden="true">
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

      <div v-if="!isEmpty" class="vw-line__x-axis" aria-hidden="true">
        <span
          v-for="item in xTickItems"
          :key="item.key"
          class="vw-line__x-label"
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
        class="vw-line__tooltip"
        :style="{ left: `${tooltipState.left}px`, top: `${tooltipState.top}px` }"
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
const PLOT_PADDING_LEFT = 56
const PLOT_PADDING_RIGHT = 24
const PLOT_PADDING_TOP = 12
const PLOT_PADDING_BOTTOM = 42
const TOOLTIP_WIDTH = 280

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

const containerRef = ref<HTMLDivElement | null>(null)
const chartRef = shallowRef<Chart | null>(null)
const tooltipState = ref({
  visible: false,
  x: 0,
  left: 0,
  top: 0,
  title: undefined as unknown,
})

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

const xComparableScale = computed(() => {
  const items = xValues.value.map((value, index) => ({
    value,
    index,
    comparable: toInterpolatableX(value),
  }))

  if (!items.length || items.some((item) => item.comparable === null)) {
    return null
  }

  const comparableItems = items as Array<{
    value: unknown
    index: number
    comparable: ComparableX
  }>
  const positions = comparableItems.map((item) => item.comparable.value)
  const min = Math.min(...positions)
  const max = Math.max(...positions)

  if (!Number.isFinite(min) || !Number.isFinite(max) || max <= min) {
    return null
  }

  return {
    items: comparableItems,
    min,
    max,
    span: max - min,
    scaleType: comparableItems.some((item) => item.comparable.kind !== 'number') ? 'time' : 'linear',
  }
})

const xDisplayTicks = computed(() => {
  if (props.xTicks?.length) return props.xTicks

  const values = xValues.value
  if (values.length <= 6) return values

  const lastIndex = values.length - 1
  const indexes = new Set<number>([0, Math.floor(lastIndex / 2), lastIndex])
  return Array.from(indexes).sort((a, b) => a - b).map((index) => values[index])
})

const getXPositionPercent = (value: unknown, fallbackIndex: number): number => {
  const scale = xComparableScale.value
  const comparable = toInterpolatableX(value)

  if (scale && comparable) {
    return clamp(((comparable.value - scale.min) / scale.span) * 100, 0, 100)
  }

  const denominator = Math.max(xValues.value.length - 1, 1)
  return (fallbackIndex / denominator) * 100
}

const xTickItems = computed(() => {
  const allValues = xValues.value

  return xDisplayTicks.value.map((value, index) => {
    const foundIndex = allValues.findIndex((candidate) => isSameXValue(candidate, value))
    const valueIndex = foundIndex >= 0 ? foundIndex : index

    return {
      key: xTickKey(value, index),
      label: props.formatX ? props.formatX(value) : String(value),
      left: getXPositionPercent(value, valueIndex),
    }
  })
})

const isSameXValue = (a: unknown, b: unknown): boolean => {
  if (a instanceof Date && b instanceof Date) return a.getTime() === b.getTime()
  return String(a) === String(b)
}

const xTickKey = (value: unknown, index: number): string =>
  `${value instanceof Date ? value.getTime() : String(value)}-${index}`

const handleTooltipMove = (event: MouseEvent) => {
  if (!props.tooltipHtml || isEmpty.value) return

  const target = event.currentTarget
  if (!(target instanceof HTMLElement)) return

  const values = xValues.value
  if (!values.length) return

  const rect = target.getBoundingClientRect()
  const plotWidth = Math.max(rect.width - PLOT_PADDING_LEFT - PLOT_PADDING_RIGHT, 1)
  const plotHeight = Math.max(rect.height - PLOT_PADDING_TOP - PLOT_PADDING_BOTTOM, 1)
  const localX = clamp(event.clientX - rect.left, PLOT_PADDING_LEFT, rect.width - PLOT_PADDING_RIGHT)
  const localY = clamp(event.clientY - rect.top, PLOT_PADDING_TOP, rect.height - PLOT_PADDING_BOTTOM)
  const progress = values.length <= 1 ? 0 : clamp((localX - PLOT_PADDING_LEFT) / plotWidth, 0, 1)
  const scale = xComparableScale.value
  const title = scale
    ? findNearestComparableXValue(scale.min + progress * scale.span)
    : values[clamp(Math.round(progress * (values.length - 1)), 0, values.length - 1)]
  const preferredLeft = localX + 16
  const left = preferredLeft + TOOLTIP_WIDTH > rect.width
    ? localX - TOOLTIP_WIDTH - 16
    : preferredLeft
  const top = clamp(localY - 96, 8, plotHeight + PLOT_PADDING_TOP - 24)

  tooltipState.value = {
    visible: true,
    x: localX,
    left: clamp(left, 8, rect.width - TOOLTIP_WIDTH - 8),
    top,
    title,
  }
}

const findNearestComparableXValue = (target: number): unknown => {
  const scale = xComparableScale.value
  if (!scale?.items.length) return xValues.value[0]

  return scale.items.reduce((nearest, item) => {
    const currentDistance = Math.abs(item.comparable.value - target)
    const nearestDistance = Math.abs(nearest.comparable.value - target)
    return currentDistance < nearestDistance ? item : nearest
  }, scale.items[0]).value
}

const hideTooltip = () => {
  tooltipState.value = {
    ...tooltipState.value,
    visible: false,
  }
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
  const scale: Record<string, unknown> = {
    color: {
      domain: seriesNames.value,
      range: colorRange.value,
    },
    y: {
      domain: [yExtent.value.min, yExtent.value.max],
      nice: false,
    },
  }

  if (props.yTicks?.length) {
    scale.y = {
      ...(scale.y as Record<string, unknown>),
      tickMethod: () => props.yTicks,
    }
  }

  const comparableScale = xComparableScale.value

  if (props.xScaleType || props.xTicks?.length || comparableScale) {
    scale.x = {}

    if (props.xScaleType) {
      ;(scale.x as Record<string, unknown>).type = props.xScaleType
    } else if (comparableScale) {
      ;(scale.x as Record<string, unknown>).type = comparableScale.scaleType
    }

    const xScaleType = (scale.x as Record<string, unknown>).type
    if (
      comparableScale &&
      (xScaleType === 'time' || xScaleType === 'linear')
    ) {
      ;(scale.x as Record<string, unknown>).domain = comparableScale.scaleType === 'time'
        ? [new Date(comparableScale.min), new Date(comparableScale.max)]
        : [comparableScale.min, comparableScale.max]
      ;(scale.x as Record<string, unknown>).nice = false
    }

    if (props.xTicks?.length) {
      ;(scale.x as Record<string, unknown>).tickMethod = () => props.xTicks
    }
  }

  return scale
}

const buildAxis = () => {
  const commonAxis = {
    title: false,
    line: false,
    tick: false,
    label: true,
    labelFill: '#6b7280',
    labelFontSize: 11,
    grid: true,
    gridStroke: '#e5e7eb',
    gridLineWidth: 1,
    gridLineDash: [4, 8],
  }

  return {
    x: {
      ...commonAxis,
      labelFormatter: (value: unknown) =>
        props.formatX ? props.formatX(value) : String(value),
    },
    y: {
      ...commonAxis,
      labelFormatter: (value: unknown) =>
        props.formatY ? props.formatY(value) : String(value),
    },
  }
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
  showAxis = false,
}: {
  minWidthOffset?: number
  maxWidthOffset?: number
  opacity?: number
  showAxis?: boolean
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
  axis: showAxis ? buildAxis() : false,
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

let renderTicket = 0

const renderChart = async () => {
  const currentTicket = ++renderTicket

  await nextTick()

  if (currentTicket !== renderTicket) return

  const container = containerRef.value
  if (!container) return

  destroyChart()
  container.innerHTML = ''

  if (isEmpty.value) return

  const children: any[] = props.brushEffect
    ? [
        createTrailLayer({ minWidthOffset: 2, maxWidthOffset: 2.4, opacity: 0.08 }),
        createTrailLayer({ minWidthOffset: 0.9, maxWidthOffset: 1.1, opacity: 0.16 }),
        createTrailLayer({ opacity: 0.94, showAxis: true }),
      ]
    : [
        createTrailLayer({ showAxis: true }),
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
    paddingLeft: PLOT_PADDING_LEFT,
    paddingRight: PLOT_PADDING_RIGHT,
    paddingTop: PLOT_PADDING_TOP,
    paddingBottom: PLOT_PADDING_BOTTOM,
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

onMounted(() => {
  void renderChart()
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
    void renderChart()
  },
  { deep: true }
)

onBeforeUnmount(() => {
  renderTicket += 1
  destroyChart()
})
</script>

<style scoped>
.vw-line {
  width: 100%;
  background: #ffffff;
}

.vw-line__header {
  margin-bottom: 12px;
}

.vw-line__title {
  display: inline-block;
  margin: 0;
  padding: 3px 8px;
  color: #111827;
  background: #dbeafe;
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
  color: #374151;
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
}

.vw-line__tooltip-crosshair {
  position: absolute;
  top: 12px;
  bottom: 42px;
  z-index: 3;
  width: 1px;
  background: rgba(17, 24, 39, 0.48);
  pointer-events: none;
}

.vw-line__grid {
  position: absolute;
  inset: 12px 24px 42px 56px;
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
  color: #9ca3af;
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
}

.vw-line__grid-line {
  width: 100%;
  border-top: 1px dashed #e5e7eb;
}

.vw-line__chart {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
}

.vw-line__x-axis {
  position: absolute;
  right: 24px;
  bottom: 12px;
  left: 56px;
  z-index: 2;
  height: 18px;
  pointer-events: none;
}

.vw-line__x-label {
  position: absolute;
  color: #9ca3af;
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
  transform: translateX(-50%);
}

.vw-line__tooltip {
  position: absolute;
  z-index: 4;
  width: max-content;
  pointer-events: none;
}

.vw-line__empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #9ca3af;
  font-size: 14px;
  pointer-events: none;
}
</style>
