<template>
  <section class="card overflow-hidden p-4 sm:p-5" data-testid="user-dashboard-activity">
    <div class="mb-5 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <div class="flex items-center gap-2">
          <Icon name="chartBar" size="sm" class="text-sky-600 dark:text-sky-400" :stroke-width="2" />
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('dashboard.activity.title') }}</h2>
        </div>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('dashboard.activity.lastYear') }}</p>
      </div>
      <div class="flex w-fit rounded-md bg-gray-100 p-1 dark:bg-dark-700" role="tablist" :aria-label="t('dashboard.activity.viewMode')">
        <button
          v-for="option in modeOptions"
          :key="option.value"
          type="button"
          role="tab"
          :aria-selected="mode === option.value"
          class="rounded px-2.5 py-1 text-xs font-medium transition-colors"
          :class="mode === option.value ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-600 dark:text-white' : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
          @click="mode = option.value"
        >
          {{ option.label }}
        </button>
      </div>
    </div>

    <div class="grid grid-cols-2 divide-x-0 divide-y divide-gray-100 rounded-md border border-gray-100 dark:divide-dark-700 dark:border-dark-700 sm:grid-cols-4 sm:divide-x sm:divide-y-0">
      <div v-for="stat in statItems" :key="stat.label" class="min-w-0 p-3 text-center">
        <p class="truncate text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ stat.value }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ stat.label }}</p>
      </div>
    </div>

    <div class="mt-6">
      <div class="mb-3 flex min-h-8 flex-wrap items-center justify-between gap-2">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.activity.tokenActivity') }}</h3>
        <p v-if="activeCell" class="rounded bg-gray-900 px-2.5 py-1 text-xs text-white dark:bg-gray-100 dark:text-gray-900" aria-live="polite">
          {{ activityTooltip(activeCell) }}
        </p>
        <div v-else class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400" aria-hidden="true">
          <span>{{ t('dashboard.activity.less') }}</span>
          <span v-for="level in 5" :key="level" class="h-3 w-3 rounded-sm" :class="legendClass(level - 1)" />
          <span>{{ t('dashboard.activity.more') }}</span>
        </div>
      </div>

      <div v-if="loading && !activity" class="grid h-32 place-items-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </div>
      <div v-else class="overflow-x-auto pb-1">
        <div class="min-w-[680px]">
          <div class="mb-2 grid grid-cols-12 text-[10px] text-gray-400 dark:text-gray-500">
            <span v-for="month in monthLabels" :key="month.key" :style="{ gridColumn: `${month.column} / span 1` }">{{ month.label }}</span>
          </div>
          <div
            class="grid gap-1"
            :style="{ gridTemplateColumns: `repeat(${weeks.length}, minmax(0, 1fr))`, gridTemplateRows: 'repeat(7, minmax(0, 1fr))', gridAutoFlow: 'column' }"
            role="grid"
            :aria-label="t('dashboard.activity.tokenActivity')"
          >
            <button
              v-for="cell in cells"
              :key="cell.date"
              type="button"
              role="gridcell"
              :disabled="cell.isFuture"
              :aria-label="activityTooltip(cell)"
              :title="activityTooltip(cell)"
              class="aspect-square min-h-3 rounded-[3px] outline-none ring-offset-1 transition-transform hover:scale-110 focus-visible:ring-2 focus-visible:ring-sky-500 disabled:cursor-default disabled:hover:scale-100"
              :class="cellClass(cell)"
              @mouseenter="activeCell = cell"
              @focus="activeCell = cell"
              @click="activeCell = cell"
            />
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UserDashboardActivity as Activity } from '@/api/usage'
import { formatCompactNumber } from '@/utils/format'

type ActivityMode = 'daily' | 'weekly' | 'cumulative'

interface ActivityCell {
  date: string
  value: number
  isFuture: boolean
}

const props = defineProps<{
  activity: Activity | null
  loading: boolean
}>()

const { t, locale } = useI18n()
const mode = ref<ActivityMode>('daily')
const activeCell = ref<ActivityCell | null>(null)

const modeOptions = computed(() => [
  { value: 'daily' as const, label: t('dashboard.activity.daily') },
  { value: 'weekly' as const, label: t('dashboard.activity.weekly') },
  { value: 'cumulative' as const, label: t('dashboard.activity.cumulative') },
])

const statItems = computed(() => [
  { label: t('dashboard.activity.totalTokens'), value: formatTokens(props.activity?.total_tokens ?? 0) },
  { label: t('dashboard.activity.peakDailyTokens'), value: formatTokens(props.activity?.peak_daily_tokens ?? 0) },
  { label: t('dashboard.activity.currentStreak'), value: formatDays(props.activity?.current_streak_days ?? 0) },
  { label: t('dashboard.activity.longestStreak'), value: formatDays(props.activity?.longest_streak_days ?? 0) },
])

const windowStart = computed(() => props.activity?.window_start ?? fallbackWindowStart())
const windowEnd = computed(() => props.activity?.window_end ?? formatDate(new Date()))
const usageByDate = computed(() => new Map((props.activity?.days ?? []).map(day => [day.date, Number(day.total_tokens) || 0])))

const weeks = computed(() => Array.from({ length: 52 }, (_, index) => index))
const baseDates = computed(() => Array.from({ length: 52 * 7 }, (_, index) => formatDate(addDays(parseDate(windowStart.value), index))))

const cells = computed<ActivityCell[]>(() => {
  let runningTotal = Number(props.activity?.cumulative_tokens_before_window ?? 0)
  const values = baseDates.value.map(date => {
    const daily = usageByDate.value.get(date) ?? 0
    runningTotal += daily
    return { date, daily, cumulative: runningTotal }
  })
  const weeklyTotals = new Map<string, number>()
  for (const item of values) {
    const weekStart = formatDate(addDays(parseDate(item.date), -weekdayOffset(parseDate(item.date))))
    weeklyTotals.set(weekStart, (weeklyTotals.get(weekStart) ?? 0) + item.daily)
  }
  return values.map(item => ({
    date: item.date,
    value: mode.value === 'daily' ? item.daily : mode.value === 'weekly' ? (weeklyTotals.get(formatDate(addDays(parseDate(item.date), -weekdayOffset(parseDate(item.date))))) ?? 0) : item.cumulative,
    isFuture: item.date > windowEnd.value,
  }))
})

const maxValue = computed(() => Math.max(0, ...cells.value.filter(cell => !cell.isFuture).map(cell => cell.value)))
const monthLabels = computed(() => {
  let previousMonth = -1
  return baseDates.value.flatMap((date, index) => {
    const parsed = parseDate(date)
    const month = parsed.getMonth()
    if (month === previousMonth) return []
    previousMonth = month
    return [{ key: date, column: Math.floor(index / 7) + 1, label: new Intl.DateTimeFormat(locale.value, { month: 'short' }).format(parsed) }]
  })
})

function cellClass(cell: ActivityCell): string {
  if (cell.isFuture || cell.value <= 0) return 'bg-gray-100 dark:bg-dark-700'
  return legendClass(activityLevel(cell.value))
}

function legendClass(level: number): string {
  return [
    'bg-gray-100 dark:bg-dark-700',
    'bg-sky-100 dark:bg-sky-950',
    'bg-sky-300 dark:bg-sky-800',
    'bg-sky-500 dark:bg-sky-600',
    'bg-sky-700 dark:bg-sky-400',
  ][level] ?? 'bg-gray-100 dark:bg-dark-700'
}

function activityLevel(value: number): number {
  if (maxValue.value <= 0 || value <= 0) return 0
  const ratio = value / maxValue.value
  if (ratio <= 0.25) return 1
  if (ratio <= 0.5) return 2
  if (ratio <= 0.75) return 3
  return 4
}

function activityTooltip(cell: ActivityCell): string {
  if (cell.isFuture) return t('dashboard.activity.futureDate', { date: formatDisplayDate(cell.date) })
  return t('dashboard.activity.tooltip', {
    date: formatDisplayDate(cell.date),
    mode: t(`dashboard.activity.${mode.value}`),
    tokens: formatTokens(cell.value),
  })
}

function formatTokens(value: number): string {
  return formatCompactNumber(value)
}

function formatDays(value: number): string {
  return t('dashboard.activity.days', { count: value })
}

function formatDisplayDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium' }).format(parseDate(value))
}

function fallbackWindowStart(): string {
  const today = new Date()
  return formatDate(addDays(today, -((today.getDay() || 7) - 1) - 51 * 7))
}

function parseDate(value: string): Date {
  return new Date(`${value}T00:00:00`)
}

function formatDate(value: Date): string {
  const year = value.getFullYear()
  const month = String(value.getMonth() + 1).padStart(2, '0')
  const day = String(value.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function addDays(value: Date, days: number): Date {
  const next = new Date(value)
  next.setDate(next.getDate() + days)
  return next
}

function weekdayOffset(value: Date): number {
  return (value.getDay() + 6) % 7
}
</script>
