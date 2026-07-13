<template>
  <div :class="chartShellClass">
    <div v-if="loading" :class="['flex items-center justify-center', chartHeightClass]">
      <LoadingSpinner />
    </div>
    <VariableWidthLineChart
      v-else
      :title="tokenUsageTrendTitle"
      :data="tokenSeries"
      :x-field="xFieldGetter"
      y-field="value"
      color-field="category"
      :colors="tokenColorRange"
      :height="chartHeight"
      :empty-text="t('admin.dashboard.noDataAvailable')"
      :format-x="formatAxisDate"
      :format-y="formatTokenAxis"
      :tooltip-html="buildTooltipHtml"
      show-legend
      :brush-effect="false"
      stroke-effect="smooth"
      :min-line-width="1.2"
      :max-line-width="8"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import VariableWidthLineChart from '@/components/charts/VariableWidthLineChart.vue'
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

const chartColors = computed(() => ({
  input: '#2563eb',
  output: '#059669',
  cacheCreation: '#f97316',
  cacheRead: '#14b8a6',
  cacheHitRate: '#7c3aed'
}))

const TAILWIND_HEIGHTS: Record<string, number> = {
  'h-40': 160,
  'h-48': 192,
  'h-56': 224,
  'h-64': 256,
  'h-72': 288,
  'h-80': 320,
  'h-96': 384
}

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

const tokenColorRange = computed(() => [
  chartColors.value.input,
  chartColors.value.output,
  chartColors.value.cacheCreation,
  chartColors.value.cacheRead
])

const chartHeight = computed(() => {
  const exactHeight = TAILWIND_HEIGHTS[props.chartHeightClass]
  if (exactHeight) return exactHeight

  const arbitraryPx = props.chartHeightClass.match(/(?:^|\s)h-\[(\d+)px\](?:\s|$)/)
  if (arbitraryPx) return Number(arbitraryPx[1])

  const arbitraryRem = props.chartHeightClass.match(/(?:^|\s)h-\[(\d+(?:\.\d+)?)rem\](?:\s|$)/)
  if (arbitraryRem) return Number(arbitraryRem[1]) * 16

  return TAILWIND_HEIGHTS['h-48']
})

const xFieldGetter = (datum: Record<string, unknown>): Date | string =>
  parseDateForChart(String(datum.date ?? ''))

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

const formatTokenAxis = (value: unknown): string => formatTokens(Number(value))

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

const findTrendPoint = (title: unknown): TrendDataPoint | undefined => {
  const exactTitle = typeof title === 'string' ? title : ''
  const exactMatch = props.trendData.find((point) => point.date === exactTitle)
  if (exactMatch) return exactMatch

  const titleTime = dateToTime(title)
  if (titleTime === null) return undefined
  return props.trendData.find((point) => dateToTime(point.date) === titleTime)
}

const dateToTime = (value: unknown): number | null => {
  const parsed = value instanceof Date ? value : parseDateForChart(String(value))
  if (!(parsed instanceof Date)) return null
  const time = parsed.getTime()
  return Number.isNaN(time) ? null : time
}

const buildTooltipHtml = (title: unknown): string => {
  const data = findTrendPoint(title)
  if (!data) {
    return `<div class="token-trend-tooltip__fallback">${escapeHtml(String(title))}</div>`
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
    <div class="token-trend-tooltip">
      <div class="token-trend-tooltip__title">${escapeHtml(data.date)}</div>
      <div class="token-trend-tooltip__rows">
        ${rows.map((row) => `
          <div class="token-trend-tooltip__row">
            <span class="token-trend-tooltip__label">
              <span class="token-trend-tooltip__marker" style="--token-trend-marker: ${row.color}"></span>
              <span>${escapeHtml(row.label)}</span>
            </span>
            <span class="token-trend-tooltip__value">${escapeHtml(row.value)}</span>
          </div>
        `).join('')}
      </div>
      <div class="token-trend-tooltip__summary">
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
