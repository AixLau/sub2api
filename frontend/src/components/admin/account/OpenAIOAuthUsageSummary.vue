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
      class="h-11 w-48 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700 xl:h-14 xl:w-[370px] min-[1536px]:w-[620px]"
      aria-hidden="true"
    >
      <Skeleton data-testid="usage-summary-skeleton-shimmer" width="100%" height="100%" class="h-full w-full" />
    </div>

    <template v-else>
      <div
        data-testid="usage-summary-full"
        class="hidden h-14 w-[min(36vw,560px)] min-w-[430px] items-stretch overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800 min-[1536px]:flex"
      >
        <button
          v-for="window in windows"
          :key="window.key"
          type="button"
          :data-testid="`usage-window-full-${window.key}`"
          class="min-w-0 flex-1 border-r border-gray-200 px-3 py-2 text-left last:border-r-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700"
          :title="window.title"
          @click="showDetails = true"
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
        </button>
      </div>

      <button
        type="button"
        data-testid="usage-summary-medium"
        class="hidden h-14 w-[260px] items-stretch overflow-hidden rounded-lg border border-gray-200 bg-white text-left transition-colors hover:border-primary-300 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700 dark:hover:bg-dark-700 xl:flex min-[1536px]:hidden"
        @click="showDetails = true"
      >
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
        class="flex h-11 w-[190px] items-stretch overflow-hidden rounded-lg border border-gray-200 bg-white text-left transition-colors hover:border-primary-300 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700 dark:hover:bg-dark-700 xl:hidden"
        @click="showDetails = true"
      >
        <span
          v-for="window in windows"
          :key="window.key"
          :data-testid="`usage-window-collapsed-${window.key}`"
          class="flex min-w-0 flex-1 items-center justify-between gap-1 border-r border-gray-200 px-2 last:border-r-0 dark:border-dark-700"
        >
          <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ window.shortLabel }}</span>
          <span class="text-xs font-semibold tabular-nums text-gray-900 dark:text-white">
            {{ formatPercent(window.data.usage_percent) }}
          </span>
        </span>
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
import Skeleton from '@/components/common/Skeleton.vue'
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
  : formatCompactCurrency(value)

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
      title: `${t('admin.accounts.openaiUsageSummary.fiveHour')}: ${t('admin.accounts.openaiUsageSummary.used')} ${formatCurrency(props.summary.five_hour.used)}, ${t('admin.accounts.openaiUsageSummary.remaining')} ${props.summary.five_hour.estimated_remaining == null ? t('admin.accounts.openaiUsageSummary.pendingEstimate') : formatCurrency(props.summary.five_hour.estimated_remaining)}`
    },
    {
      key: 'seven-day',
      label: t('admin.accounts.openaiUsageSummary.sevenDay'),
      shortLabel: '7d',
      data: props.summary.seven_day,
      title: `${t('admin.accounts.openaiUsageSummary.sevenDay')}: ${t('admin.accounts.openaiUsageSummary.used')} ${formatCurrency(props.summary.seven_day.used)}, ${t('admin.accounts.openaiUsageSummary.remaining')} ${props.summary.seven_day.estimated_remaining == null ? t('admin.accounts.openaiUsageSummary.pendingEstimate') : formatCurrency(props.summary.seven_day.estimated_remaining)}`
    }
  ]
})

</script>
