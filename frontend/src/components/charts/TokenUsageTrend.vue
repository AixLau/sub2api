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
import { DualAxes, type DualAxesOptions } from '@antv/g2plot'
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
  type: string
  value: number
  source: TrendDataPoint
}

type HitRateSeriesPoint = {
  date: string
  type: string
  hitRate: number
  source: TrendDataPoint
}

const chartContainer = ref<HTMLElement | null>(null)
const plot = shallowRef<DualAxes | null>(null)

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
  cacheCreation: '#d97706',
  cacheRead: '#0891b2',
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
    { date: data.date, type: tokenLabels.value.input, value: data.input_tokens, source: data },
    { date: data.date, type: tokenLabels.value.output, value: data.output_tokens, source: data },
    { date: data.date, type: tokenLabels.value.cacheCreation, value: data.cache_creation_tokens, source: data },
    { date: data.date, type: tokenLabels.value.cacheRead, value: data.cache_read_tokens, source: data }
  ])
)

const hitRateSeries = computed<HitRateSeriesPoint[]>(() =>
  props.trendData.map((data) => ({
    date: data.date,
    type: tokenLabels.value.cacheHitRate,
    hitRate: getCacheHitRate(data),
    source: data
  }))
)

const tokenColorMap = computed<Record<string, string>>(() => ({
  [tokenLabels.value.input]: chartColors.value.input,
  [tokenLabels.value.output]: chartColors.value.output,
  [tokenLabels.value.cacheCreation]: chartColors.value.cacheCreation,
  [tokenLabels.value.cacheRead]: chartColors.value.cacheRead,
  [tokenLabels.value.cacheHitRate]: chartColors.value.cacheHitRate
}))

const buildChartOptions = (): DualAxesOptions => ({
  data: [tokenSeries.value, hitRateSeries.value],
  xField: 'date',
  yField: ['value', 'hitRate'],
  autoFit: true,
  padding: [12, 48, 52, 64],
  appendPadding: [0, 6, 0, 0],
  animation: false,
  legend: {
    position: 'top',
    itemName: {
      style: {
        fill: chartColors.value.text,
        fontSize: 12
      }
    }
  },
  xAxis: {
    label: {
      autoRotate: true,
      autoHide: true,
      style: {
        fill: chartColors.value.text,
        fontSize: 11
      }
    },
    line: {
      style: {
        stroke: chartColors.value.border
      }
    },
    tickLine: {
      style: {
        stroke: chartColors.value.border
      }
    }
  },
  yAxis: [
    {
      label: {
        formatter: (value: string) => formatTokens(Number(value)),
        style: {
          fill: chartColors.value.text,
          fontSize: 11
        }
      },
      grid: {
        line: {
          style: {
            stroke: chartColors.value.grid
          }
        }
      }
    },
    {
      min: 0,
      max: 100,
      label: {
        formatter: (value: string) => `${value}%`,
        style: {
          fill: chartColors.value.cacheHitRate,
          fontSize: 11
        }
      },
      grid: null
    }
  ],
  geometryOptions: [
    {
      geometry: 'line',
      seriesField: 'type',
      smooth: true,
      color: (datum: Record<string, unknown>) => tokenColorMap.value[String(datum.type)],
      lineStyle: {
        lineWidth: 2
      },
      point: {
        size: 2,
        style: (datum: Record<string, unknown>) => ({
          fill: '#ffffff',
          stroke: tokenColorMap.value[String(datum.type)],
          lineWidth: 1.5
        })
      }
    },
    {
      geometry: 'line',
      smooth: true,
      color: chartColors.value.cacheHitRate,
      lineStyle: {
        lineWidth: 2,
        lineDash: [6, 6]
      },
      point: {
        size: 2,
        style: {
          fill: '#ffffff',
          stroke: chartColors.value.cacheHitRate,
          lineWidth: 1.5
        }
      }
    }
  ],
  tooltip: {
    shared: true,
    showCrosshairs: true,
    showMarkers: true,
    domStyles: {
      'g2-tooltip': {
        backgroundColor: chartColors.value.background,
        border: `1px solid ${chartColors.value.border}`,
        borderRadius: '8px',
        boxShadow: '0 10px 24px rgba(15, 23, 42, 0.16)',
        color: chartColors.value.body,
        padding: '12px'
      }
    },
    customContent: (title: string) => buildTooltipHtml(title)
  },
  theme: {
    defaultColor: chartColors.value.input
  }
})

const syncPlot = () => {
  if (props.loading || props.trendData.length === 0 || !chartContainer.value) {
    destroyPlot()
    return
  }

  const options = buildChartOptions()
  if (plot.value) {
    plot.value.update(options)
    return
  }

  plot.value = new DualAxes(chartContainer.value, options)
  plot.value.render()
}

const destroyPlot = () => {
  if (!plot.value) return
  plot.value.destroy()
  plot.value = null
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

const getCacheHitRate = (data: TrendDataPoint): number => {
  const totalPromptTokens = data.input_tokens + data.cache_read_tokens + data.cache_creation_tokens
  return totalPromptTokens > 0 ? (data.cache_read_tokens / totalPromptTokens) * 100 : 0
}

const totalUsageTokens = (data: TrendDataPoint): number =>
  data.input_tokens + data.output_tokens + data.cache_creation_tokens + data.cache_read_tokens

const formatTokens = (value: number): string => {
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

const buildTooltipHtml = (title: string): string => {
  const data = props.trendData.find((point) => point.date === title)
  if (!data) {
    return `<div class="space-y-1 text-xs">${escapeHtml(title)}</div>`
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
    <div style="min-width: 180px; color: ${chartColors.value.body};">
      <div style="margin-bottom: 8px; font-weight: 700; color: ${chartColors.value.title};">${escapeHtml(title)}</div>
      <div style="display: grid; gap: 5px;">
        ${rows.map((row) => `
          <div style="display: flex; align-items: center; justify-content: space-between; gap: 12px;">
            <span style="display: inline-flex; align-items: center; gap: 6px;">
              <span style="width: 9px; height: 9px; border: 2px solid ${row.color}; display: inline-block;"></span>
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
