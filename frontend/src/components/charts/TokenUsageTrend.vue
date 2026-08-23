<template>
  <div
    :class="chartShellClass"
    :style="chartShellStyle"
    :data-surface="surface ?? 'default'"
    data-testid="token-usage-trend"
    role="region"
    :aria-label="tokenUsageTrendTitle"
    :aria-busy="loading"
  >
    <img
      v-if="isPlayfulDashboard"
      class="token-trend__badge"
      src="/assets/dashboard/badge-good-job.png"
      alt=""
      aria-hidden="true"
      draggable="false"
      width="116"
      height="116"
      loading="lazy"
      decoding="async"
    />

    <div
      v-if="loading"
      :class="loadingContainerClass"
      :style="loadingContainerStyle"
      role="status"
      aria-live="polite"
      :aria-label="t('common.loading')"
    >
      <LoadingSpinner />
      <span class="sr-only">{{ t('common.loading') }}</span>
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
      :empty-text="emptyText"
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
import { tokenUsageColors } from '@/theme/designTokens'
import { calculateCacheHitRate } from '@/utils/cacheHitRate'
import type { TrendDataPoint } from '@/types'

const { t, locale } = useI18n()

const props = withDefaults(defineProps<{
  trendData: TrendDataPoint[]
  loading?: boolean
  surface?: 'default' | 'tremor' | 'playfulDashboard'
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
  ...tokenUsageColors
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

const chartShellClass = computed(() => {
  if (props.surface === 'tremor') {
    return 'relative w-full rounded-lg border border-line-default bg-surface-panel p-5 text-left shadow-card'
  }

  if (props.surface === 'playfulDashboard') {
    return 'token-trend token-trend--playful'
  }

  return 'card p-4'
})

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

const isPlayfulDashboard = computed(() => props.surface === 'playfulDashboard')

const chartShellStyle = computed(() => isPlayfulDashboard.value
  ? { '--token-trend-chart-height': `${chartHeight.value}px` }
  : undefined)

const loadingContainerClass = computed(() => isPlayfulDashboard.value
  ? 'token-trend__loading flex items-center justify-center'
  : `flex items-center justify-center ${props.chartHeightClass}`)

const loadingContainerStyle = computed(() => isPlayfulDashboard.value
  ? { height: `${chartHeight.value + 55}px` }
  : undefined)

const emptyText = computed(() => isPlayfulDashboard.value
  ? t('dashboard.noDataAvailable')
  : t('admin.dashboard.noDataAvailable'))

const chartDateLocale = computed(() => {
  if (!isPlayfulDashboard.value) return 'zh-CN'
  return locale.value.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US'
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
  return new Intl.DateTimeFormat(chartDateLocale.value, hasHourlyData
    ? { month: '2-digit', day: '2-digit', hour: '2-digit' }
    : { month: '2-digit', day: '2-digit' }
  ).format(date)
}

const getCacheHitRate = (data: TrendDataPoint): number => {
  return calculateCacheHitRate(data.input_tokens, data.cache_creation_tokens, data.cache_read_tokens)
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

<style scoped>
.token-trend--playful {
  position: relative;
  isolation: isolate;
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  max-width: 100%;
  padding: 18px;
  border: 1px solid rgb(var(--color-line-subtle) / 0.86);
  border-radius: 22px;
  background:
    radial-gradient(circle at 92% 8%, rgb(105 225 218 / 0.1), transparent 30%),
    linear-gradient(145deg, rgb(var(--color-surface-panel) / 0.96), rgb(var(--color-surface-panel) / 0.79));
  box-shadow: 0 16px 38px rgb(var(--color-shadow) / 0.08);
  text-align: left;
  backdrop-filter: blur(18px);
}

.token-trend--playful::before {
  position: absolute;
  inset: 0;
  z-index: 0;
  border-radius: inherit;
  background: linear-gradient(115deg, rgb(255 255 255 / 0.18), transparent 44%);
  content: '';
  pointer-events: none;
}

.token-trend--playful > :not(.token-trend__badge) {
  position: relative;
  z-index: 2;
  min-width: 0;
}

.token-trend__badge {
  position: absolute;
  top: -54px;
  right: -6px;
  z-index: 1;
  width: clamp(90px, 7vw, 116px);
  height: auto;
  aspect-ratio: 1;
  object-fit: contain;
  pointer-events: none;
  user-select: none;
}

.token-trend__loading {
  width: 100%;
  min-height: var(--token-trend-chart-height, 192px);
}

.token-trend--playful :deep(.vw-line) {
  min-width: 0;
  max-width: 100%;
}

.token-trend--playful :deep(.vw-line__header) {
  min-width: 0;
  padding-right: 88px;
}

.token-trend--playful :deep(.vw-line__title) {
  padding: 4px 9px;
  border-radius: 7px;
  background: rgb(207 247 244 / 0.72);
  color: rgb(8 132 139);
  font-size: 14px;
  font-weight: 750;
}

:global(.dark .token-trend--playful .vw-line__title) {
  background: rgb(27 108 113 / 0.35);
  color: rgb(126 231 224);
}

.token-trend--playful :deep(.vw-line__legend) {
  width: 100%;
  min-width: 0;
  max-width: 100%;
  align-content: flex-start;
  column-gap: 16px;
  row-gap: 9px;
}

.token-trend--playful :deep(.vw-line__legend-item) {
  max-width: 100%;
}

.token-trend--playful :deep(.vw-line__body),
.token-trend--playful :deep(.vw-line__chart) {
  min-width: 0;
  max-width: 100%;
}

@media (max-width: 1279px) {
  .token-trend__badge {
    top: -42px;
    right: 0;
    width: 82px;
  }

  .token-trend--playful :deep(.vw-line__header) {
    padding-right: 66px;
  }
}

@media (max-width: 1023px) {
  .token-trend__badge {
    display: none;
  }

  .token-trend--playful :deep(.vw-line__header) {
    padding-right: 0;
  }
}

@media (max-width: 639px) {
  .token-trend--playful {
    padding: 16px 12px;
  }

  .token-trend--playful :deep(.vw-line__legend) {
    column-gap: 12px;
  }

  .token-trend--playful :deep(.vw-line__legend-item) {
    gap: 6px;
    font-size: 11px;
  }
}
</style>
