<template>
  <div :class="chartShellClass">
    <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">
      {{ tokenUsageTrendTitle }}
    </h3>
    <div v-if="loading" :class="['flex items-center justify-center', chartHeightClass]">
      <LoadingSpinner />
    </div>
    <div v-else-if="trendData.length > 0" ref="chartContainer" :class="chartHeightClass" />
    <div
      v-else
      :class="[
        'flex items-center justify-center text-sm text-gray-500 dark:text-gray-400',
        chartHeightClass
      ]"
    >
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { createElement } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { Line, type LineConfig } from '@ant-design/plots'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { TrendDataPoint } from '@/types'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  trendData: TrendDataPoint[]
  loading?: boolean
  surface?: 'default' | 'tremor'
  showCost?: boolean
  chartHeightClass?: string
}>(), {
  chartHeightClass: 'h-48'
})

type TokenSeriesPoint = {
  date: string
  category: string
  value: number
  source: TrendDataPoint
}

const chartContainer = ref<HTMLElement | null>(null)
const reactRoot = shallowRef<Root | null>(null)

const isDarkMode = computed(() => {
  return document.documentElement.classList.contains('dark')
})

const chartColors = computed(() => ({
  background: isDarkMode.value ? '#111827' : '#ffffff',
  title: isDarkMode.value ? '#f9fafb' : '#111827',
  text: isDarkMode.value ? '#d1d5db' : '#4b5563',
  body: isDarkMode.value ? '#e5e7eb' : '#374151',
  border: isDarkMode.value ? '#374151' : '#e5e7eb',
  grid: isDarkMode.value ? 'rgba(55, 65, 81, 0.45)' : 'rgba(229, 231, 235, 0.8)',
  input: '#2563eb',
  output: '#059669',
  cacheCreation: '#f97316',
  cacheRead: '#14b8a6',
  cacheHitRate: '#7c3aed'
}))

const chartShellClass = computed(() => props.surface === 'tremor'
  ? 'relative w-full rounded-lg border border-gray-200 bg-white p-5 text-left shadow-[0_1px_2px_rgba(15,23,42,0.04)] dark:border-gray-900 dark:bg-[#090E1A]'
  : 'card p-4')

const tokenUsageTrendTitle = computed(() => {
  const label = t('usage.tokenUsageTrend')
  return label === 'usage.tokenUsageTrend' ? 'Token 使用趋势' : label
})

const tokenLabels = computed(() => ({
  input: t('usage.trend.input'),
  output: t('usage.trend.output'),
  cacheCreation: t('usage.trend.cacheCreation'),
  cacheRead: t('usage.trend.cacheRead'),
  cacheHitRate: t('usage.trend.cacheHitRate')
}))

const tokenSeries = computed<TokenSeriesPoint[]>(() =>
  props.trendData.flatMap((data) => [
    { date: data.date, category: tokenLabels.value.input, value: data.input_tokens, source: data },
    { date: data.date, category: tokenLabels.value.output, value: data.output_tokens, source: data },
    { date: data.date, category: tokenLabels.value.cacheCreation, value: data.cache_creation_tokens, source: data },
    { date: data.date, category: tokenLabels.value.cacheRead, value: data.cache_read_tokens, source: data }
  ])
)

const tokenColorMap = computed<Record<string, string>>(() => ({
  [tokenLabels.value.input]: chartColors.value.input,
  [tokenLabels.value.output]: chartColors.value.output,
  [tokenLabels.value.cacheCreation]: chartColors.value.cacheCreation,
  [tokenLabels.value.cacheRead]: chartColors.value.cacheRead,
  [tokenLabels.value.cacheHitRate]: chartColors.value.cacheHitRate
}))

const buildChartOptions = (): LineConfig => ({
  data: tokenSeries.value,
  xField: (datum: TokenSeriesPoint) => parseDateForChart(datum.date),
  yField: 'value',
  sizeField: 'value',
  shapeField: 'trail',
  colorField: 'category',
  autoFit: true,
  padding: [12, 28, 46, 56],
  legend: {
    size: false,
    color: {
      position: 'top',
      itemLabelFill: chartColors.value.text,
      itemLabelFontSize: 12,
      itemMarker: 'line'
    }
  },
  scale: {
    y: { nice: true },
    size: { range: [1.25, 10] },
    color: {
      range: [
        chartColors.value.input,
        chartColors.value.output,
        chartColors.value.cacheCreation,
        chartColors.value.cacheRead
      ]
    }
  },
  axis: {
    x: {
      labelAutoHide: true,
      labelAutoRotate: true,
      labelFill: chartColors.value.text,
      labelFontSize: 11,
      lineStroke: chartColors.value.border,
      tickStroke: chartColors.value.border,
      labelFormatter: (value: unknown) => formatAxisDate(value)
    },
    y: {
      labelFill: chartColors.value.text,
      labelFontSize: 11,
      labelFormatter: (value: unknown) => formatTokens(Number(value)),
      gridStroke: chartColors.value.grid
    }
  },
  tooltip: {
    title: (datum: TokenSeriesPoint) => datum.date,
    items: [
      (datum: TokenSeriesPoint) => ({
        name: datum.category,
        color: tokenColorMap.value[datum.category],
        value: formatTokens(datum.value)
      })
    ]
  },
  interaction: {
    tooltip: {
      shared: true,
      crosshairs: true,
      marker: true,
      render: (_event: unknown, options: { title: string }) => buildTooltipHtml(String(options.title ?? ''))
    }
  },
  style: {
    lineCap: 'round',
    lineJoin: 'round'
  }
})

const syncPlot = () => {
  if (props.loading || props.trendData.length === 0 || !chartContainer.value) {
    destroyPlot()
    return
  }

  if (!reactRoot.value) {
    reactRoot.value = createRoot(chartContainer.value)
  }

  reactRoot.value.render(createElement(Line, buildChartOptions()))
}

const destroyPlot = () => {
  if (!reactRoot.value) return
  reactRoot.value.unmount()
  reactRoot.value = null
}

onMounted(() => {
  syncPlot()
})

onBeforeUnmount(() => {
  destroyPlot()
})

watch(
  () => [props.trendData, props.loading, props.showCost, props.chartHeightClass],
  async () => {
    await nextTick()
    syncPlot()
  },
  { deep: true }
)

const parseDateForChart = (value: string): Date | string => {
  const normalizedValue = value.includes(' ') ? value.replace(' ', 'T') : value
  const date = new Date(normalizedValue)
  return Number.isNaN(date.getTime()) ? value : date
}

const formatAxisDate = (value: unknown): string => {
  const date = value instanceof Date ? value : new Date(String(value))
  if (Number.isNaN(date.getTime())) {
    return String(value)
  }

  const hasHourlyData = props.trendData.some((point) => point.date.includes(':'))
  return new Intl.DateTimeFormat('zh-CN', hasHourlyData
    ? { month: '2-digit', day: '2-digit', hour: '2-digit' }
    : { month: '2-digit', day: '2-digit' }
  ).format(date)
}

const getCacheHitRate = (data: TrendDataPoint): number => {
  const totalPromptTokens = data.input_tokens + data.cache_read_tokens + data.cache_creation_tokens
  return totalPromptTokens > 0 ? (data.cache_read_tokens / totalPromptTokens) * 100 : 0
}

const totalUsageTokens = (data: TrendDataPoint): number =>
  data.total_tokens ?? data.input_tokens + data.output_tokens + data.cache_creation_tokens + data.cache_read_tokens

const formatTokens = (value: number): string => {
  if (!Number.isFinite(value)) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const formatCost = (value: number): string => {
  if (value >= 1000) {
    return (value / 1000).toFixed(2) + 'K'
  } else if (value >= 1) {
    return value.toFixed(2)
  } else if (value >= 0.01) {
    return value.toFixed(3)
  }
  return value.toFixed(4)
}

const findTrendPoint = (title: string): TrendDataPoint | undefined => {
  const exactMatch = props.trendData.find((point) => point.date === title)
  if (exactMatch) return exactMatch

  const titleTime = dateToTime(title)
  if (titleTime === null) return undefined
  return props.trendData.find((point) => dateToTime(point.date) === titleTime)
}

const dateToTime = (value: string): number | null => {
  const parsed = parseDateForChart(value)
  if (!(parsed instanceof Date)) return null
  const time = parsed.getTime()
  return Number.isNaN(time) ? null : time
}

const buildTooltipHtml = (title: string): string => {
  const data = findTrendPoint(title)
  if (!data) {
    return `<div style="color: ${chartColors.value.body};">${escapeHtml(title)}</div>`
  }

  const rows = [
    { label: tokenLabels.value.input, value: formatTokens(data.input_tokens), color: chartColors.value.input },
    { label: tokenLabels.value.output, value: formatTokens(data.output_tokens), color: chartColors.value.output },
    { label: tokenLabels.value.cacheCreation, value: formatTokens(data.cache_creation_tokens), color: chartColors.value.cacheCreation },
    { label: tokenLabels.value.cacheRead, value: formatTokens(data.cache_read_tokens), color: chartColors.value.cacheRead },
    { label: tokenLabels.value.cacheHitRate, value: `${getCacheHitRate(data).toFixed(1)}%`, color: chartColors.value.cacheHitRate }
  ]

  const summaryRows = [
    `${t('usage.trend.totalUsage')}: ${formatTokens(totalUsageTokens(data))}`,
    ...(props.showCost ? [`${t('usage.trend.cost')}: $${formatCost(data.actual_cost)}`] : [])
  ]

  return `
    <div style="min-width: 190px; color: ${chartColors.value.body}; background: ${chartColors.value.background}; border: 1px solid ${chartColors.value.border}; border-radius: 8px; box-shadow: 0 10px 24px rgba(15, 23, 42, 0.16); padding: 12px;">
      <div style="margin-bottom: 8px; font-weight: 700; color: ${chartColors.value.title};">${escapeHtml(data.date)}</div>
      <div style="display: grid; gap: 5px;">
        ${rows.map((row) => `
          <div style="display: flex; align-items: center; justify-content: space-between; gap: 12px;">
            <span style="display: inline-flex; align-items: center; gap: 6px;">
              <span style="width: 9px; height: 9px; border-radius: 999px; background: ${row.color}; display: inline-block;"></span>
              ${escapeHtml(row.label)}
            </span>
            <span style="font-weight: 600; color: ${chartColors.value.title};">${escapeHtml(row.value)}</span>
          </div>
        `).join('')}
      </div>
      <div style="margin-top: 10px; border-top: 1px solid ${chartColors.value.border}; padding-top: 8px; display: grid; gap: 4px; color: ${chartColors.value.title}; font-weight: 600;">
        ${summaryRows.map((row) => `<div>${escapeHtml(row)}</div>`).join('')}
      </div>
    </div>
  `
}

const escapeHtml = (value: string): string =>
  value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
</script>
