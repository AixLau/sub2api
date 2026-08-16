<template>
  <section
    data-testid="openai-oauth-usage-summary"
    data-layout="toolbar-compact"
    class="min-w-0"
    aria-live="polite"
    :aria-busy="loading"
  >
    <button
      v-if="error && !summary"
      type="button"
      data-testid="usage-summary-error"
      class="flex h-11 items-center gap-2 rounded-lg border border-red-200 bg-white px-3 text-sm text-red-700 transition-colors hover:bg-red-50 dark:border-red-900/60 dark:bg-dark-800 dark:text-red-300 dark:hover:bg-red-950/30"
      @click="emit('retry')"
    >
      <Icon name="exclamationCircle" size="sm" />
      <span>{{ t('admin.accounts.openaiUsageSummary.loadFailedCompact') }}</span>
      <Icon name="refresh" size="xs" />
    </button>

    <div
      v-else-if="!summary"
      data-testid="usage-summary-skeleton"
      class="h-11 w-48 animate-pulse rounded-lg border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-800 xl:h-14 xl:w-[370px] min-[1536px]:w-[620px]"
    />

    <template v-else>
      <div
        data-testid="usage-summary-full"
        class="hidden h-14 w-[min(48vw,760px)] min-w-[620px] items-stretch overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800 min-[1536px]:flex"
      >
        <header class="flex w-[156px] flex-shrink-0 items-center border-r border-gray-200 px-3 dark:border-dark-700">
          <div class="min-w-0">
            <div class="flex items-center gap-1.5">
              <span :class="['h-2 w-2 flex-shrink-0 rounded-full', status.dotClass]" />
              <h2 class="truncate text-xs font-semibold text-gray-900 dark:text-white">
                OpenAI OAuth · {{ summary.included_account_count }}
              </h2>
            </div>
            <p class="mt-1 truncate pl-3.5 text-[10px] text-gray-500 dark:text-gray-400">{{ status.label }}</p>
          </div>
        </header>

        <article
          v-for="window in windows"
          :key="window.key"
          :data-testid="`usage-window-full-${window.key}`"
          class="min-w-0 flex-1 border-r border-gray-200 px-3 py-2 dark:border-dark-700"
          :title="window.title"
        >
          <div class="flex items-baseline justify-between gap-2">
            <span class="text-[11px] font-medium text-gray-500 dark:text-gray-400">{{ window.label }}</span>
            <strong class="text-xs font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatPercent(window.data.usage_percent) }}</strong>
          </div>
          <div class="mt-0.5 flex items-center gap-2 whitespace-nowrap text-[11px] tabular-nums text-gray-600 dark:text-gray-300">
            <span>{{ t('admin.accounts.openaiUsageSummary.usedShort') }} {{ formatCompactCurrency(window.data.used) }}</span>
            <span class="text-gray-300 dark:text-dark-600">·</span>
            <span>{{ t('admin.accounts.openaiUsageSummary.remainingShort') }} {{ formatCompactEstimate(window.data.estimated_remaining) }}</span>
          </div>
          <div class="mt-1 h-1 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
            <div
              data-testid="usage-progress"
              :class="['h-full rounded-full transition-[width,background-color] duration-300', progressColor(window.data.usage_percent)]"
              :style="{ width: `${clampPercent(window.data.usage_percent)}%` }"
            />
          </div>
        </article>

        <button
          type="button"
          data-testid="usage-summary-details"
          class="flex w-14 flex-shrink-0 flex-col items-center justify-center gap-1 text-[11px] font-medium text-gray-600 transition-colors hover:bg-gray-50 hover:text-primary-700 dark:text-gray-300 dark:hover:bg-dark-700 dark:hover:text-primary-300"
          @click="showDetails = true"
        >
          <Icon v-if="loading" name="refresh" size="sm" class="animate-spin" />
          <Icon v-else name="chevronRight" size="sm" />
          <span>{{ t('admin.accounts.openaiUsageSummary.details') }}</span>
        </button>
      </div>

      <button
        type="button"
        data-testid="usage-summary-medium"
        class="hidden h-14 w-[370px] items-stretch overflow-hidden rounded-lg border border-gray-200 bg-white text-left transition-colors hover:border-primary-300 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700 dark:hover:bg-dark-700 xl:flex min-[1536px]:hidden"
        @click="showDetails = true"
      >
        <span class="flex w-[132px] flex-shrink-0 items-center gap-1.5 border-r border-gray-200 px-3 dark:border-dark-700">
          <span :class="['h-2 w-2 flex-shrink-0 rounded-full', status.dotClass]" />
          <span class="truncate text-xs font-semibold text-gray-900 dark:text-white">OpenAI OAuth · {{ summary.included_account_count }}</span>
        </span>
        <span
          v-for="window in windows"
          :key="window.key"
          :data-testid="`usage-window-medium-${window.key}`"
          class="flex min-w-0 flex-1 flex-col justify-center border-r border-gray-200 px-2 last:border-r-0 dark:border-dark-700"
          :title="window.title"
        >
          <span class="flex items-center justify-between gap-1 text-[11px]">
            <span class="font-medium text-gray-500 dark:text-gray-400">{{ window.shortLabel }}</span>
            <strong class="tabular-nums text-gray-900 dark:text-white">{{ formatPercent(window.data.usage_percent) }}</strong>
          </span>
          <span class="mt-0.5 truncate text-[10px] tabular-nums text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openaiUsageSummary.remainingShort') }} {{ formatCompactEstimate(window.data.estimated_remaining) }}
          </span>
          <span class="mt-1 h-0.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
            <span
              :class="['block h-full rounded-full', progressColor(window.data.usage_percent)]"
              :style="{ width: `${clampPercent(window.data.usage_percent)}%` }"
            />
          </span>
        </span>
      </button>

      <button
        type="button"
        data-testid="usage-summary-collapsed"
        class="flex h-11 w-[190px] items-center justify-between gap-2 rounded-lg border border-gray-200 bg-white px-3 text-sm transition-colors hover:border-primary-300 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700 dark:hover:bg-dark-700 xl:hidden"
        @click="showDetails = true"
      >
        <span class="flex min-w-0 items-center gap-2">
          <span :class="['h-2 w-2 flex-shrink-0 rounded-full', status.dotClass]" />
          <span class="truncate font-medium text-gray-700 dark:text-gray-200">
            {{ t('admin.accounts.openaiUsageSummary.collapsedLabel') }} · {{ formatPercent(highestUsagePercent) }}
          </span>
        </span>
        <Icon v-if="loading" name="refresh" size="sm" class="flex-shrink-0 animate-spin text-gray-400" />
        <Icon v-else name="chevronRight" size="sm" class="flex-shrink-0 text-gray-400" />
      </button>
    </template>

    <OpenAIOAuthUsageDetailsDialog
      :show="showDetails"
      :summary="summary"
      :error="error"
      @close="showDetails = false"
      @retry="emit('retry')"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import OpenAIOAuthUsageDetailsDialog from './OpenAIOAuthUsageDetailsDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatCompactCurrency, formatCurrency } from '@/utils/format'
import type { OpenAIOAuthUsageSummary } from '@/types'

const props = defineProps<{
  summary: OpenAIOAuthUsageSummary | null
  loading: boolean
  error: string | null
}>()

const emit = defineEmits<{
  (event: 'retry'): void
}>()

const { t } = useI18n()
const showDetails = ref(false)

const clampPercent = (value: number) => Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0))
const formatPercent = (value: number) => `${clampPercent(value).toFixed(1)}%`
const formatCompactEstimate = (value: number | null) => value == null
  ? t('admin.accounts.openaiUsageSummary.pendingEstimateShort')
  : `~${formatCompactCurrency(value)}`

const progressColor = (value: number) => {
  const percent = clampPercent(value)
  if (percent >= 95) return 'bg-red-500'
  if (percent >= 85) return 'bg-orange-500'
  if (percent >= 70) return 'bg-amber-500'
  return 'bg-primary-500'
}

const windows = computed(() => {
  if (!props.summary) return []
  return [
    {
      key: 'five-hour',
      label: t('admin.accounts.openaiUsageSummary.fiveHour'),
      shortLabel: '5h',
      data: props.summary.five_hour,
      title: `${t('admin.accounts.openaiUsageSummary.fiveHour')}: ${t('admin.accounts.openaiUsageSummary.used')} ${formatCurrency(props.summary.five_hour.used)}, ${t('admin.accounts.openaiUsageSummary.remaining')} ${props.summary.five_hour.estimated_remaining == null ? t('admin.accounts.openaiUsageSummary.pendingEstimate') : `~${formatCurrency(props.summary.five_hour.estimated_remaining)}`}`
    },
    {
      key: 'seven-day',
      label: t('admin.accounts.openaiUsageSummary.sevenDay'),
      shortLabel: '7d',
      data: props.summary.seven_day,
      title: `${t('admin.accounts.openaiUsageSummary.sevenDay')}: ${t('admin.accounts.openaiUsageSummary.used')} ${formatCurrency(props.summary.seven_day.used)}, ${t('admin.accounts.openaiUsageSummary.remaining')} ${props.summary.seven_day.estimated_remaining == null ? t('admin.accounts.openaiUsageSummary.pendingEstimate') : `~${formatCurrency(props.summary.seven_day.estimated_remaining)}`}`
    }
  ]
})

const highestUsagePercent = computed(() => props.summary
  ? Math.max(clampPercent(props.summary.five_hour.usage_percent), clampPercent(props.summary.seven_day.usage_percent))
  : 0)

const status = computed(() => {
  if (props.error) {
    return { label: t('admin.accounts.openaiUsageSummary.statusError'), dotClass: 'bg-red-500' }
  }
  if (props.loading) {
    return { label: t('admin.accounts.openaiUsageSummary.statusRefreshing'), dotClass: 'animate-pulse bg-blue-500' }
  }
  if (!props.summary) {
    return { label: t('admin.accounts.openaiUsageSummary.statusUnavailable'), dotClass: 'bg-gray-400' }
  }
  const pending = props.summary.five_hour.pending_sync_account_count + props.summary.seven_day.pending_sync_account_count
  if (pending > 0) {
    return { label: t('admin.accounts.openaiUsageSummary.statusPendingSync'), dotClass: 'bg-blue-500' }
  }
  const unestimated = props.summary.five_hour.unestimated_account_count + props.summary.seven_day.unestimated_account_count
  if (unestimated > 0) {
    return { label: t('admin.accounts.openaiUsageSummary.statusPartial'), dotClass: 'bg-amber-500' }
  }
  const historical = [props.summary.five_hour.reference_source, props.summary.seven_day.reference_source]
    .some(source => source === 'historical' || source === 'mixed')
  if (historical) {
    return { label: t('admin.accounts.openaiUsageSummary.historicalStatus'), dotClass: 'bg-gray-400' }
  }
  return { label: t('admin.accounts.openaiUsageSummary.statusCurrent'), dotClass: 'bg-emerald-500' }
})
</script>
