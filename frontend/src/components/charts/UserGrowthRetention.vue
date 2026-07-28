<template>
  <section class="card overflow-hidden" data-testid="user-growth-retention">
    <header class="border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-6 sm:py-5">
      <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('admin.dashboard.userGrowthRetention') }}
          </h3>
        </div>
        <div class="w-32 shrink-0">
          <Select
            :model-value="days"
            :options="rangeOptions"
            value-key="value"
            label-key="label"
            :aria-label="t('admin.dashboard.timeRange')"
            @update:model-value="onDaysChange"
          />
        </div>
      </div>
    </header>

    <div v-if="loading" class="flex h-[28rem] items-center justify-center">
      <LoadingSpinner />
    </div>

    <template v-else-if="cohorts.length > 0">
      <div class="px-4 py-6 sm:px-6 sm:py-8">
        <div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(8rem,0.7fr)_minmax(0,1fr)_minmax(8rem,0.7fr)_minmax(0,1fr)] lg:items-center lg:gap-4">
          <div class="min-w-0" data-testid="registration-stage">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.registeredUsers') }}
            </p>
            <p class="mt-2 text-3xl font-semibold tabular-nums text-blue-600 dark:text-blue-400">
              {{ formatNumber(totalRegistrations) }}
            </p>
            <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
              {{ t('admin.dashboard.periodCohort', { days }) }}
            </p>
          </div>

          <div class="flex items-center gap-3 lg:block" data-testid="registration-loss">
            <div class="h-px flex-1 bg-red-300 dark:bg-red-800 lg:w-full" />
            <div class="shrink-0 text-left lg:mt-2 lg:text-center">
              <p class="text-xs font-semibold tabular-nums text-red-600 dark:text-red-400">
                {{ t('admin.dashboard.lostUsers', { count: formatNumber(registrationLoss.count) }) }}
              </p>
              <p class="mt-0.5 text-xs tabular-nums text-red-500 dark:text-red-400">
                {{ formatRate(registrationLoss.rate) }}
              </p>
            </div>
          </div>

          <div class="min-w-0" data-testid="recharge-stage">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.rechargedUsers') }}
            </p>
            <p class="mt-2 text-3xl font-semibold tabular-nums text-violet-600 dark:text-violet-400">
              {{ formatNumber(totalPaidUsers) }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.registrationToRecharge') }}
              <span class="font-semibold tabular-nums text-violet-600 dark:text-violet-400">
                {{ formatRate(rechargeRate) }}
              </span>
            </p>
          </div>

          <div class="flex items-center gap-3 lg:block" data-testid="recharge-loss">
            <div class="h-px flex-1 bg-amber-300 dark:bg-amber-800 lg:w-full" />
            <div class="shrink-0 text-left lg:mt-2 lg:text-center">
              <p class="text-xs font-semibold tabular-nums text-amber-600 dark:text-amber-400">
                {{ t('admin.dashboard.lostUsers', { count: formatNumber(repeatLoss.count) }) }}
              </p>
              <p class="mt-0.5 text-xs tabular-nums text-amber-600 dark:text-amber-400">
                {{ formatRate(repeatLoss.rate) }}
              </p>
            </div>
          </div>

          <div class="min-w-0" data-testid="repeat-stage">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.repeatBuyers') }}
            </p>
            <p class="mt-2 text-3xl font-semibold tabular-nums text-teal-600 dark:text-teal-400">
              {{ formatNumber(totalRepeatBuyers) }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.rechargeToRepeat') }}
              <span class="font-semibold tabular-nums text-teal-600 dark:text-teal-400">
                {{ formatRate(repeatRate) }}
              </span>
            </p>
          </div>
        </div>
      </div>

      <div>
        <div class="px-4 py-5 sm:px-6">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.dashboard.conversionTrend') }}
            </h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.conversionTrendHint') }}
            </p>
          </div>
          <div v-if="chartData" class="mt-4 h-64">
            <Chart type="bar" :data="chartData" :options="chartOptions" />
          </div>
          <div v-else class="mt-4 flex h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.dashboard.noMatureCohorts') }}
          </div>
        </div>
      </div>
    </template>

    <div v-else class="flex h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
  </section>
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
import Select from '@/components/common/Select.vue'
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

interface LossMetric {
  count: number
  rate: number | null
}

const { t, locale } = useI18n()
const props = defineProps<{
  cohorts: UserRetentionPoint[]
  summary: UserRetentionSummary | null
  days?: number
  loading?: boolean
}>()
const emit = defineEmits<{
  (event: 'range-change', days: number): void
}>()

const days = computed(() => props.days ?? 7)
const rangeOptions = computed(() => [7, 30, 60, 90, 180].map((value) => ({
  value,
  label: t('admin.dashboard.lastDays', { days: value })
})))

const onDaysChange = (value: string | number | boolean | null) => {
  const nextDays = Number(value)
  if ([7, 30, 60, 90, 180].includes(nextDays) && nextDays !== days.value) {
    emit('range-change', nextDays)
  }
}

const totalRegistrations = computed(() => props.cohorts.reduce((sum, cohort) => sum + cohort.registrations, 0))
const totalPaidUsers = computed(() => props.cohorts.reduce((sum, cohort) => sum + cohort.paid_users, 0))
const totalRepeatBuyers = computed(() => props.cohorts.reduce((sum, cohort) => sum + cohort.repeat_buyers, 0))
const rechargeRate = computed(() => rate(totalPaidUsers.value, totalRegistrations.value))
const repeatRate = computed(() => rate(totalRepeatBuyers.value, totalPaidUsers.value))
const registrationLoss = computed<LossMetric>(() => loss(totalRegistrations.value, totalPaidUsers.value))
const repeatLoss = computed<LossMetric>(() => loss(totalPaidUsers.value, totalRepeatBuyers.value))
// Keep recent cohorts visible: paid_users is the cumulative count observed so far,
// so a cohort can grow as users recharge later today or on a future day.
const trendCohorts = computed(() => props.cohorts.slice(-30))

function rate(numerator: number, denominator: number): number | null {
  return denominator > 0 ? numerator * 100 / denominator : null
}

function loss(from: number, to: number): LossMetric {
  const count = Math.max(0, from - to)
  return { count, rate: rate(count, from) }
}

const formatNumber = (value: number) => value.toLocaleString(locale.value)
const formatRate = (value: number | null | undefined) => value == null ? '--' : `${value.toFixed(1)}%`
const formatAmount = (value: number | null | undefined) => {
  const amount = Number(value)
  if (!Number.isFinite(amount)) return '$0'
  return `$${amount.toLocaleString(locale.value, { maximumFractionDigits: 2 })}`
}

const isDarkMode = computed(() => document.documentElement.classList.contains('dark'))
const colors = computed(() => ({
  text: isDarkMode.value ? '#d1d5db' : '#4b5563',
  muted: isDarkMode.value ? '#9ca3af' : '#6b7280',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb'
}))

const chartData = computed(() => {
  if (!trendCohorts.value.length) return null
  return {
    labels: trendCohorts.value.map((point) => point.date),
    datasets: [
      {
        type: 'bar' as const,
        label: t('admin.dashboard.registrations'),
        data: trendCohorts.value.map((point) => point.registrations),
        backgroundColor: isDarkMode.value ? '#3b82f680' : '#60a5fa80',
        borderColor: '#3b82f6',
        borderWidth: 1,
        borderRadius: 2,
        maxBarThickness: 20,
        yAxisID: 'users'
      },
      {
        type: 'line' as const,
        label: t('admin.dashboard.rechargedUsers'),
        data: trendCohorts.value.map((point) => point.paid_users),
        borderColor: '#7c3aed',
        backgroundColor: '#7c3aed',
        pointRadius: 2,
        pointHoverRadius: 4,
        borderWidth: 2,
        tension: 0.25,
        yAxisID: 'users'
      },
      {
        type: 'line' as const,
        label: t('admin.dashboard.rechargeAmount'),
        data: trendCohorts.value.map((point) => point.recharge_amount),
        borderColor: '#059669',
        backgroundColor: '#059669',
        pointRadius: 2,
        pointHoverRadius: 4,
        borderWidth: 2,
        tension: 0.25,
        yAxisID: 'amount'
      },
      {
        type: 'line' as const,
        label: t('admin.dashboard.apiActiveUsers'),
        data: trendCohorts.value.map((point) => point.active_users),
        borderColor: '#0f766e',
        backgroundColor: '#0f766e',
        pointRadius: 2,
        pointHoverRadius: 4,
        borderWidth: 2,
        tension: 0.25,
        yAxisID: 'users'
      }
    ]
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: {
    legend: {
      position: 'bottom' as const,
      align: 'start' as const,
      labels: { color: colors.value.text, boxWidth: 10, boxHeight: 10, padding: 16, usePointStyle: true }
    },
    tooltip: {
      callbacks: {
        label: (context: any) => {
          const cohort = trendCohorts.value[context.dataIndex]
          if (context.dataset.yAxisID === 'amount') {
            return `${context.dataset.label}: ${formatAmount(Number(context.raw))}`
          }
          if (context.dataset.label === t('admin.dashboard.rechargedUsers') && cohort) {
            return `${context.dataset.label}: ${Number(context.raw).toLocaleString(locale.value)} (${formatRate(rate(cohort.paid_users, cohort.registrations))})`
          }
          return `${context.dataset.label}: ${Number(context.raw).toLocaleString(locale.value)}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: { color: colors.value.muted, maxTicksLimit: 7, maxRotation: 0 }
    },
    users: {
      position: 'left' as const,
      beginAtZero: true,
      grid: { color: colors.value.grid },
      ticks: { color: colors.value.muted, precision: 0 }
    },
    amount: {
      position: 'right' as const,
      beginAtZero: true,
      grid: { drawOnChartArea: false },
      ticks: {
        color: colors.value.muted,
        callback: (value: string | number) => formatAmount(Number(value))
      }
    }
  }
}))
</script>
