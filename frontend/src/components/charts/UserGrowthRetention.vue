<template>
  <section class="card overflow-hidden" data-testid="user-growth-retention">
    <header class="border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-6 sm:py-5">
      <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('admin.dashboard.userGrowthRetention') }}
          </h3>
          <p class="mt-1 max-w-3xl text-xs leading-5 text-gray-500 dark:text-gray-400">
            {{ t('admin.dashboard.retentionDefinition') }}
          </p>
        </div>
        <span class="shrink-0 text-xs font-medium text-gray-500 dark:text-gray-400">
          {{ t('admin.dashboard.lastDays', { days: 60 }) }}
        </span>
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
              {{ t('admin.dashboard.periodCohort') }}
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

      <div class="border-y border-amber-200 bg-amber-50 px-4 py-3 dark:border-amber-900/60 dark:bg-amber-950/20 sm:px-6" data-testid="primary-loss-alert">
        <div class="flex items-start gap-3">
          <span class="mt-1 h-2 w-2 shrink-0 rounded-full bg-amber-500" />
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.dashboard.primaryLoss') }}
            </p>
            <p class="mt-0.5 text-xs leading-5 text-gray-600 dark:text-gray-300">
              {{ primaryLossMessage }}
            </p>
          </div>
        </div>
      </div>

      <div class="grid lg:grid-cols-[minmax(0,2fr)_minmax(16rem,0.8fr)]">
        <div class="px-4 py-5 sm:px-6">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.dashboard.conversionTrend') }}
            </h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.matureCohortHint') }}
            </p>
          </div>
          <div v-if="chartData" class="mt-4 h-64">
            <Chart type="bar" :data="chartData" :options="chartOptions" />
          </div>
          <div v-else class="mt-4 flex h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.dashboard.noMatureCohorts') }}
          </div>
        </div>

        <aside class="border-t border-gray-100 px-4 py-5 dark:border-dark-700 sm:px-6 lg:border-l lg:border-t-0">
          <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.dashboard.needsAttention') }}
          </h4>
          <div class="mt-3 divide-y divide-gray-100 dark:divide-dark-700">
            <div v-for="signal in signals" :key="signal.label" class="flex items-center gap-3 py-4 first:pt-2">
              <span class="h-2 w-2 shrink-0 rounded-full" :class="signal.dotClass" />
              <div class="min-w-0 flex-1">
                <p class="text-xs font-medium text-gray-700 dark:text-gray-300">{{ signal.label }}</p>
                <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                  {{ t('admin.dashboard.comparedWithPreviousCohorts') }}
                </p>
              </div>
              <span class="shrink-0 text-sm font-semibold tabular-nums" :class="signal.valueClass">
                {{ formatDelta(signal.delta, signal.unit) }}
              </span>
            </div>
          </div>
        </aside>
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

interface TrendSignal {
  label: string
  delta: number | null
  unit: 'percent' | 'point'
  dotClass: string
  valueClass: string
}

const { t, locale } = useI18n()
const props = defineProps<{
  cohorts: UserRetentionPoint[]
  summary: UserRetentionSummary | null
  loading?: boolean
}>()

const totalRegistrations = computed(() => props.cohorts.reduce((sum, cohort) => sum + cohort.registrations, 0))
const totalPaidUsers = computed(() => props.cohorts.reduce((sum, cohort) => sum + cohort.paid_users, 0))
const totalRepeatBuyers = computed(() => props.cohorts.reduce((sum, cohort) => sum + cohort.repeat_buyers, 0))
const rechargeRate = computed(() => rate(totalPaidUsers.value, totalRegistrations.value))
const repeatRate = computed(() => rate(totalRepeatBuyers.value, totalPaidUsers.value))
const registrationLoss = computed<LossMetric>(() => loss(totalRegistrations.value, totalPaidUsers.value))
const repeatLoss = computed<LossMetric>(() => loss(totalPaidUsers.value, totalRepeatBuyers.value))
const matureCohorts = computed(() => props.cohorts.filter((cohort) => cohort.paid_rate != null))
const trendCohorts = computed(() => matureCohorts.value.slice(-30))

const primaryLossMessage = computed(() => {
  const registrationIsPrimary = registrationLoss.value.rate == null
    || repeatLoss.value.rate == null
    || registrationLoss.value.rate >= repeatLoss.value.rate
  const metric = registrationIsPrimary ? registrationLoss.value : repeatLoss.value
  return t('admin.dashboard.primaryLossMessage', {
    from: t(registrationIsPrimary ? 'admin.dashboard.registeredUsers' : 'admin.dashboard.rechargedUsers'),
    to: t(registrationIsPrimary ? 'admin.dashboard.rechargedUsers' : 'admin.dashboard.repeatBuyers'),
    count: formatNumber(metric.count),
    rate: formatRate(metric.rate)
  })
})

const registrationDelta = computed(() => compareTotals(props.cohorts, (cohort) => cohort.registrations))
const rechargeDelta = computed(() => compareWeightedRates(matureCohorts.value, 'paid_users', 'registrations'))
const repeatDelta = computed(() => compareWeightedRates(matureCohorts.value, 'repeat_buyers', 'paid_users'))
const signals = computed<TrendSignal[]>(() => [
  buildSignal(t('admin.dashboard.registrationTrend'), registrationDelta.value, 'percent'),
  buildSignal(t('admin.dashboard.rechargeTrend'), rechargeDelta.value, 'point'),
  buildSignal(t('admin.dashboard.repurchaseTrend'), repeatDelta.value, 'point')
])

function rate(numerator: number, denominator: number): number | null {
  return denominator > 0 ? numerator * 100 / denominator : null
}

function loss(from: number, to: number): LossMetric {
  const count = Math.max(0, from - to)
  return { count, rate: rate(count, from) }
}

function splitComparison<T>(items: T[]): [T[], T[]] {
  const recent = items.slice(-7)
  const previous = items.slice(-14, -7)
  return [recent, previous]
}

function compareTotals(items: UserRetentionPoint[], value: (item: UserRetentionPoint) => number): number | null {
  const [recent, previous] = splitComparison(items)
  const recentTotal = recent.reduce((sum, item) => sum + value(item), 0)
  const previousTotal = previous.reduce((sum, item) => sum + value(item), 0)
  return previousTotal > 0 ? (recentTotal - previousTotal) * 100 / previousTotal : null
}

function compareWeightedRates(
  items: UserRetentionPoint[],
  numeratorKey: 'paid_users' | 'repeat_buyers',
  denominatorKey: 'registrations' | 'paid_users'
): number | null {
  const [recent, previous] = splitComparison(items)
  const weightedRate = (cohorts: UserRetentionPoint[]) => rate(
    cohorts.reduce((sum, cohort) => sum + cohort[numeratorKey], 0),
    cohorts.reduce((sum, cohort) => sum + cohort[denominatorKey], 0)
  )
  const recentRate = weightedRate(recent)
  const previousRate = weightedRate(previous)
  return recentRate != null && previousRate != null ? recentRate - previousRate : null
}

function buildSignal(label: string, delta: number | null, unit: TrendSignal['unit']): TrendSignal {
  const negative = delta != null && delta < 0
  return {
    label,
    delta,
    unit,
    dotClass: negative ? 'bg-red-500' : delta == null ? 'bg-gray-400' : 'bg-emerald-500',
    valueClass: negative
      ? 'text-red-600 dark:text-red-400'
      : delta == null
        ? 'text-gray-400 dark:text-gray-500'
        : 'text-emerald-600 dark:text-emerald-400'
  }
}

const formatNumber = (value: number) => value.toLocaleString(locale.value)
const formatRate = (value: number | null | undefined) => value == null ? '--' : `${value.toFixed(1)}%`
const formatDelta = (value: number | null, unit: TrendSignal['unit']) => {
  if (value == null) return '--'
  const sign = value > 0 ? '+' : ''
  return unit === 'point' ? `${sign}${value.toFixed(1)}pt` : `${sign}${value.toFixed(1)}%`
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
        yAxisID: 'registrations'
      },
      {
        type: 'line' as const,
        label: t('admin.dashboard.rechargeConversion'),
        data: trendCohorts.value.map((point) => point.paid_rate),
        borderColor: '#7c3aed',
        backgroundColor: '#7c3aed',
        pointRadius: 2,
        pointHoverRadius: 4,
        borderWidth: 2,
        tension: 0.25,
        yAxisID: 'rate'
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
        label: (context: any) => context.dataset.yAxisID === 'rate'
          ? `${context.dataset.label}: ${formatRate(context.raw)}`
          : `${context.dataset.label}: ${Number(context.raw).toLocaleString(locale.value)}`
      }
    }
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: { color: colors.value.muted, maxTicksLimit: 7, maxRotation: 0 }
    },
    registrations: {
      position: 'left' as const,
      beginAtZero: true,
      grid: { color: colors.value.grid },
      ticks: { color: colors.value.muted, precision: 0 }
    },
    rate: {
      position: 'right' as const,
      beginAtZero: true,
      min: 0,
      max: 100,
      grid: { drawOnChartArea: false },
      ticks: { color: colors.value.muted, callback: (value: string | number) => `${value}%` }
    }
  }
}))
</script>
