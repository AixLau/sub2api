<template>
  <section
    class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4"
    data-testid="dashboard-stats"
  >
    <template v-for="card in cards" :key="card.id">
      <div v-if="!card.hidden" class="contents" :data-testid="card.testId">
        <DashboardStatCard
          :title="card.title"
          :value="card.value"
          :description="card.description"
          :icon="card.icon"
          :theme="card.theme"
          :accent="card.accent"
          :decorative-chart="card.decorativeChart"
          :value-class="card.valueClass"
          :hidden="card.hidden"
        />
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  UserDashboardActivity,
  UserDashboardStats as UserStatsType,
} from '@/api/usage'
import {
  formatCompactNumber,
  formatCostFixed,
  formatNumberLocaleString,
} from '@/utils/format'
import DashboardStatCard from './DashboardStatCard.vue'
import type {
  DashboardStatAccent,
  DashboardStatDecoration,
  DashboardStatIcon,
  DashboardStatTheme,
} from './dashboardStatThemes'

const props = defineProps<{
  stats: UserStatsType
  balance: number
  isSimple: boolean
  activity?: UserDashboardActivity | null
}>()

const { t } = useI18n()

interface DashboardCard {
  id: string
  testId: string
  title: string
  value: string
  description: string
  icon: DashboardStatIcon
  theme: DashboardStatTheme
  accent?: DashboardStatAccent
  decorativeChart?: DashboardStatDecoration
  valueClass?: string
  hidden?: boolean
}

const cards = computed<DashboardCard[]>(() => {
  const stats = props.stats
  const todayCacheTokens = finiteNumber(stats.today_cache_creation_tokens)
    + finiteNumber(stats.today_cache_read_tokens)
  const totalCacheTokens = finiteNumber(stats.total_cache_creation_tokens)
    + finiteNumber(stats.total_cache_read_tokens)

  return [
    {
      id: 'balance',
      testId: 'balance-card',
      title: t('dashboard.balanceTotal'),
      value: formatMoney(props.balance, 2),
      description: t('dashboard.balanceAvailable'),
      icon: 'dollar',
      theme: 'pink',
      decorativeChart: 'wave',
      hidden: props.isSimple,
    },
    {
      id: 'api-keys',
      testId: 'api-keys-card',
      title: t('dashboard.apiKeys'),
      value: formatMetric(stats.total_api_keys),
      description: `${formatMetric(stats.active_api_keys)} ${t('common.active')}`,
      icon: 'key',
      theme: 'blue',
      accent: 'description',
      decorativeChart: 'wave',
    },
    {
      id: 'today-requests',
      testId: 'today-requests-card',
      title: t('dashboard.todayRequests'),
      value: formatMetric(stats.today_requests),
      description: `${t('common.total')}: ${formatFullNumber(stats.total_requests)}`,
      icon: 'chart',
      theme: 'green',
      decorativeChart: 'bars',
    },
    {
      id: 'today-cost',
      testId: 'today-cost-card',
      title: t('dashboard.todayCost'),
      value: formatMoney(stats.today_actual_cost, 4),
      description: `${t('common.total')}: ${formatMoney(stats.total_actual_cost, 4)}`,
      icon: 'dollar',
      theme: 'purple',
      accent: 'value',
      decorativeChart: 'wave',
    },
    {
      id: 'today-tokens',
      testId: 'today-tokens-card',
      title: t('dashboard.todayTokens'),
      value: formatTokens(stats.today_tokens),
      description: tokenBreakdown(
        stats.today_input_tokens,
        stats.today_output_tokens,
        todayCacheTokens,
      ),
      icon: 'cube',
      theme: 'amber',
      decorativeChart: 'wave',
    },
    {
      id: 'total-tokens',
      testId: 'total-tokens-card',
      title: t('dashboard.totalTokens'),
      value: formatTokens(stats.total_tokens),
      description: tokenBreakdown(
        stats.total_input_tokens,
        stats.total_output_tokens,
        totalCacheTokens,
      ),
      icon: 'database',
      theme: 'indigo',
      decorativeChart: 'wave',
    },
    {
      id: 'performance',
      testId: 'performance-card',
      title: t('dashboard.activity.peakDailyTokens'),
      value: formatTokens(props.activity?.peak_daily_tokens),
      description: activityStreaks(
        props.activity?.current_streak_days,
        props.activity?.longest_streak_days,
      ),
      icon: 'chart',
      theme: 'violet',
      accent: 'description',
      decorativeChart: 'wave',
    },
    {
      id: 'average-response',
      testId: 'average-response-card',
      title: t('dashboard.avgResponse'),
      value: formatDuration(stats.average_duration_ms),
      description: t('dashboard.averageTime'),
      icon: 'clock',
      theme: 'rose',
      decorativeChart: 'wave',
    },
  ]
})

function finiteNumber(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function formatMetric(value: number | null | undefined): string {
  return formatCompactNumber(finiteNumber(value))
}

function formatFullNumber(value: number | null | undefined): string {
  return formatNumberLocaleString(Math.round(finiteNumber(value)))
}

function formatMoney(value: number | null | undefined, fractionDigits: number): string {
  return `$${formatCostFixed(finiteNumber(value), fractionDigits)}`
}

function formatTokens(value: number | null | undefined): string {
  const number = finiteNumber(value)
  if (Math.abs(number) >= 1_000_000_000) return `${(number / 1_000_000_000).toFixed(2)}B`
  return formatCompactNumber(number)
}

function tokenBreakdown(input: number, output: number, cache: number): string {
  return [
    `${t('dashboard.input')}: ${formatTokens(input)}`,
    `${t('dashboard.output')}: ${formatTokens(output)}`,
    `${t('dashboard.cache')}: ${formatTokens(cache)}`,
  ].join(' / ')
}

function activityStreaks(
  current: number | null | undefined,
  longest: number | null | undefined,
): string {
  return [
    `${t('dashboard.activity.currentStreak')} ${formatDays(current)}`,
    `${t('dashboard.activity.longestStreak')} ${formatDays(longest)}`,
  ].join(' / ')
}

function formatDays(value: number | null | undefined): string {
  return t('dashboard.activity.days', { count: finiteNumber(value) })
}

function formatDuration(value: number | null | undefined): string {
  const milliseconds = Math.max(0, finiteNumber(value))
  return milliseconds >= 1000
    ? `${(milliseconds / 1000).toFixed(2)}s`
    : `${milliseconds.toFixed(0)}ms`
}
</script>
