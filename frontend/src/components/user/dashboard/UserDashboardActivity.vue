<template>
  <section class="mx-auto w-full max-w-[1460px]" data-testid="user-dashboard-activity">
    <div class="flex items-center justify-between gap-4">
      <h2 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('dashboard.activity.title') }}</h2>
      <div class="flex items-center gap-5 text-sm font-medium" role="tablist" :aria-label="t('dashboard.activity.viewMode')">
        <button
          v-for="option in modeOptions"
          :key="option.value"
          type="button"
          role="tab"
          class="transition-colors"
          :class="mode === option.value ? 'text-gray-900 dark:text-white' : 'text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300'"
          :aria-selected="mode === option.value"
          @click="selectMode(option.value)"
        >
          {{ option.label }}
        </button>
      </div>
    </div>

    <div v-if="loading && !activity" class="grid h-48 place-items-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('common.loading') }}
    </div>
    <div v-else ref="activityGraph" class="activity-graph relative mt-5" @mouseleave="hideActivityPreview">
      <p
        v-if="activityPreview"
        data-testid="activity-tooltip"
        class="pointer-events-none absolute z-10 max-w-[min(92%,28rem)] -translate-x-1/2 -translate-y-[calc(100%+12px)] rounded-2xl bg-gray-900 px-4 py-3 text-sm text-white shadow-lg dark:bg-gray-100 dark:text-gray-900"
        :style="{ left: `${activityPreview.x}px`, top: `${activityPreview.y}px` }"
      >
        {{ activityTooltip(activityPreview.cell) }}
      </p>
      <div class="activity-scroll overflow-x-auto pb-1 sm:overflow-hidden">
        <div class="min-w-[620px] sm:min-w-0">
          <div class="relative mb-2 h-5 text-xs text-gray-400 dark:text-gray-500">
            <span
              v-for="month in monthLabels"
              :key="month.key"
              data-testid="activity-month-label"
              class="absolute top-0 whitespace-nowrap"
              :style="{ left: `${month.offset}%` }"
            >{{ month.label }}</span>
          </div>
          <div
            class="grid gap-1.5"
            :style="gridStyle"
            role="grid"
            :aria-label="t('dashboard.activity.title')"
          >
            <button
              v-for="cell in cells"
              :key="cell.date"
              type="button"
              role="gridcell"
              :disabled="cell.isFuture"
              :aria-label="activityTooltip(cell)"
              class="aspect-square min-h-3 rounded-[5px] outline-none ring-offset-1 transition-transform hover:scale-110 focus-visible:ring-2 focus-visible:ring-sky-500 disabled:cursor-default disabled:hover:scale-100"
              :class="cellClass(cell)"
              @mouseenter="showActivityPreview(cell, $event)"
              @mousemove="showActivityPreview(cell, $event)"
              @mouseleave="hideActivityPreview"
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
const activityGraph = ref<HTMLElement | null>(null)
const activityPreview = ref<{ cell: ActivityCell, x: number, y: number } | null>(null)

const modeOptions = computed(() => [
  { value: 'daily' as const, label: t('dashboard.activity.daily') },
  { value: 'weekly' as const, label: t('dashboard.activity.weekly') },
  { value: 'cumulative' as const, label: t('dashboard.activity.cumulative') },
])

const windowStart = computed(() => props.activity?.window_start ?? fallbackWindowStart())
const windowEnd = computed(() => props.activity?.window_end ?? fallbackWindowEnd())
const currentDate = computed(() => props.activity?.current_date ?? formatDate(new Date()))
const usageByDate = computed(() => new Map((props.activity?.days ?? []).map(day => [day.date, Number(day.total_tokens) || 0])))
const baseDates = computed(() => datesBetween(windowStart.value, windowEnd.value))
const weeks = computed(() => Math.ceil(baseDates.value.length / 7))
const gridStyle = computed(() => ({
  gridTemplateColumns: `repeat(${weeks.value}, minmax(0, 1fr))`,
  gridTemplateRows: 'repeat(7, minmax(0, 1fr))',
  gridAutoFlow: 'column',
}))

const cells = computed<ActivityCell[]>(() => {
  let runningTotal = Number(props.activity?.cumulative_tokens_before_window ?? 0)
  const values = baseDates.value.map(date => {
    const daily = usageByDate.value.get(date) ?? 0
    runningTotal += daily
    return { date, daily, cumulative: runningTotal }
  })
  const weeklyTotals = new Map<string, number>()
  for (const item of values) {
    const weekStart = startOfWeek(item.date)
    weeklyTotals.set(weekStart, (weeklyTotals.get(weekStart) ?? 0) + item.daily)
  }
  return values.map(item => ({
    date: item.date,
    value: mode.value === 'daily' ? item.daily : mode.value === 'weekly' ? (weeklyTotals.get(startOfWeek(item.date)) ?? 0) : item.cumulative,
    isFuture: item.date > currentDate.value,
  }))
})

const maxValue = computed(() => Math.max(0, ...cells.value.filter(cell => !cell.isFuture).map(cell => cell.value)))
const monthLabels = computed(() => baseDates.value.flatMap((date, index) => {
  const parsed = parseDate(date)
  const previous = index > 0 ? parseDate(baseDates.value[index - 1]) : null
  if (previous && previous.getMonth() === parsed.getMonth() && previous.getFullYear() === parsed.getFullYear()) return []
  return [{
    key: date,
    offset: (Math.floor(index / 7) / weeks.value) * 100,
    label: new Intl.DateTimeFormat(locale.value, { month: 'short' }).format(parsed),
  }]
}))

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

function showActivityPreview(cell: ActivityCell, event: MouseEvent) {
  if (cell.isFuture || !activityGraph.value) {
    activityPreview.value = null
    return
  }
  const bounds = activityGraph.value.getBoundingClientRect()
  const width = Math.max(bounds.width, 1)
  const horizontalPadding = Math.min(180, Math.max(96, width / 4))
  const minX = Math.min(horizontalPadding, width / 2)
  const maxX = Math.max(minX, width - horizontalPadding)
  activityPreview.value = {
    cell,
    x: Math.min(Math.max(event.clientX - bounds.left, minX), maxX),
    y: Math.max(0, event.clientY - bounds.top),
  }
}

function hideActivityPreview() {
  activityPreview.value = null
}

function selectMode(nextMode: ActivityMode) {
  mode.value = nextMode
  hideActivityPreview()
}

function formatTokens(value: number): string {
  return formatCompactNumber(value)
}

function formatDisplayDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium' }).format(parseDate(value))
}

function fallbackWindowStart(): string {
  const today = new Date()
  return formatDate(addDays(today, -weekdayOffset(today) - 51 * 7))
}

function fallbackWindowEnd(): string {
  return formatDate(addDays(parseDate(fallbackWindowStart()), 52 * 7 - 1))
}

function datesBetween(start: string, end: string): string[] {
  const dates: string[] = []
  for (let date = parseDate(start), last = parseDate(end); date <= last; date = addDays(date, 1)) {
    dates.push(formatDate(date))
  }
  return dates
}

function startOfWeek(date: string): string {
  const value = parseDate(date)
  return formatDate(addDays(value, -weekdayOffset(value)))
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

<style scoped>
.activity-scroll::-webkit-scrollbar {
  display: none;
}

.activity-scroll {
  -ms-overflow-style: none;
  scrollbar-width: none;
}
</style>
