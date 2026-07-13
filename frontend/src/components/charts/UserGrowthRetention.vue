<template>
  <div class="card p-4">
    <div class="mb-4 flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.dashboard.userGrowthRetention') }}
        </h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.dashboard.retentionDefinition') }}
        </p>
      </div>
      <div class="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-5 lg:w-[34rem]">
        <div v-for="period in summaryPeriods" :key="period.label" class="sm:text-right">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ period.label }}</p>
          <p class="text-base font-semibold text-gray-900 dark:text-white">
            {{ formatRate(period.value) }}
          </p>
        </div>
      </div>
    </div>
    <div v-if="loading" class="flex h-64 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="cohorts.length > 0 && chartData" class="h-64">
      <Chart type="bar" :data="chartData" :options="chartOptions" />
    </div>
    <div v-else class="flex h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  BarController,
  BarElement,
  CategoryScale,
  Legend,
  LinearScale,
  LineController,
  LineElement,
  PointElement,
  Tooltip
} from 'chart.js'
import { Chart } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { UserRetentionPoint, UserRetentionSummary } from '@/api/admin/dashboard'

ChartJS.register(
  BarController,
  BarElement,
  CategoryScale,
  Legend,
  LinearScale,
  LineController,
  LineElement,
  PointElement,
  Tooltip
)

const { t } = useI18n()
const props = defineProps<{
  cohorts: UserRetentionPoint[]
  summary: UserRetentionSummary | null
  loading?: boolean
}>()

const summaryPeriods = computed(() => [
  { label: 'D1', value: props.summary?.d1_rate },
  { label: 'D7', value: props.summary?.d7_rate },
  { label: 'D30', value: props.summary?.d30_rate },
  { label: t('admin.dashboard.paidConversion'), value: props.summary?.paid_rate },
  { label: t('admin.dashboard.repeatPurchase'), value: props.summary?.repeat_buy_rate }
])

const formatRate = (value: number | null | undefined) =>
  value == null ? '--' : `${value.toFixed(1)}%`

const isDarkMode = computed(() => document.documentElement.classList.contains('dark'))
const colors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb'
}))

const chartData = computed(() => {
  if (!props.cohorts.length) return null
  return {
    labels: props.cohorts.map((point) => point.date),
    datasets: [
      {
        type: 'bar' as const,
        label: t('admin.dashboard.registrations'),
        data: props.cohorts.map((point) => point.registrations),
        backgroundColor: '#3b82f680',
        borderColor: '#3b82f6',
        borderWidth: 1,
        yAxisID: 'registrations'
      },
      ...(['d1_rate', 'd7_rate', 'd30_rate', 'paid_rate', 'repeat_buy_rate'] as const).map((key, index) => ({
        type: 'line' as const,
        label: ['D1', 'D7', 'D30', t('admin.dashboard.paidConversion'), t('admin.dashboard.repeatPurchase')][index],
        data: props.cohorts.map((point) => point[key]),
        borderColor: ['#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899'][index],
        backgroundColor: ['#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899'][index],
        borderDash: index >= 3 ? [5, 3] : undefined,
        pointRadius: 1.5,
        borderWidth: 2,
        tension: 0.25,
        spanGaps: false,
        yAxisID: 'retention'
      }))
    ]
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: {
    legend: { labels: { color: colors.value.text, boxWidth: 12 } },
    tooltip: {
      callbacks: {
        label: (context: any) => context.dataset.yAxisID === 'retention'
          ? `${context.dataset.label}: ${formatRate(context.raw)}`
          : `${context.dataset.label}: ${Number(context.raw).toLocaleString()}`
      }
    }
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: { color: colors.value.text, maxTicksLimit: 10, maxRotation: 0 }
    },
    registrations: {
      position: 'left' as const,
      beginAtZero: true,
      grid: { color: colors.value.grid },
      ticks: { color: colors.value.text, precision: 0 }
    },
    retention: {
      position: 'right' as const,
      beginAtZero: true,
      min: 0,
      max: 100,
      grid: { drawOnChartArea: false },
      ticks: { color: colors.value.text, callback: (value: string | number) => `${value}%` }
    }
  }
}))
</script>
