<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.openaiUsageSummary.detailsTitle')"
    width="normal"
    @close="emit('close')"
  >
    <div v-if="summary" data-testid="openai-oauth-usage-details" class="space-y-5">
      <div
        v-if="error"
        class="flex items-center justify-between gap-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
      >
        <span>{{ t('admin.accounts.openaiUsageSummary.loadFailed') }}</span>
        <button type="button" class="inline-flex items-center gap-1 font-medium hover:underline" @click="emit('retry')">
          <Icon name="refresh" size="xs" />
          {{ t('admin.accounts.openaiUsageSummary.retry') }}
        </button>
      </div>

      <dl class="grid grid-cols-3 divide-x divide-gray-200 rounded-md border border-gray-200 bg-gray-50/70 py-3 dark:divide-dark-700 dark:border-dark-700 dark:bg-dark-800/60">
        <div class="px-3 text-center">
          <dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.totalAccounts') }}</dt>
          <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ summary.account_count }}</dd>
        </div>
        <div class="px-3 text-center">
          <dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.includedAccounts') }}</dt>
          <dd class="mt-1 text-lg font-semibold tabular-nums text-primary-700 dark:text-primary-300">{{ summary.included_account_count }}</dd>
        </div>
        <div class="px-3 text-center">
          <dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.excludedAccounts') }}</dt>
          <dd class="mt-1 text-lg font-semibold tabular-nums text-gray-700 dark:text-gray-200">{{ summary.excluded_account_count }}</dd>
        </div>
      </dl>

      <section
        v-for="window in windows"
        :key="window.key"
        :data-testid="`usage-details-${window.key}`"
        class="border-t border-gray-200 pt-4 first:border-t-0 first:pt-0 dark:border-dark-700"
      >
        <div class="flex items-center justify-between gap-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ window.label }}</h3>
          <span :class="['rounded px-2 py-0.5 text-[11px] font-medium', window.sourceClass]">
            {{ window.sourceLabel }}
          </span>
        </div>

        <div
          class="mt-3 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600"
          role="progressbar"
          :aria-label="window.label"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="clampPercent(window.data.usage_percent)"
        >
          <div
            data-testid="usage-details-progress"
            :class="['h-full rounded-full transition-[width,background-color] duration-300', progressColor(window.data.usage_percent)]"
            :style="{ width: `${clampPercent(window.data.usage_percent)}%` }"
          />
        </div>

        <dl class="mt-3 grid grid-cols-2 gap-x-6 gap-y-2.5 text-sm">
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.used') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatCurrency(window.data.used) }}</dd>
          </div>
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.remaining') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatEstimate(window.data.estimated_remaining) }}</dd>
          </div>
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.capacity') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatEstimate(window.data.estimated_capacity) }}</dd>
          </div>
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.usagePercent') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatPercent(window.data.usage_percent) }}</dd>
          </div>
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.remainingPercent') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatPercent(window.data.remaining_percent) }}</dd>
          </div>
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.referenceCapacity') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatEstimate(window.data.reference_capacity) }}</dd>
          </div>
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.currentSamples') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">
              {{ window.data.current_sample_account_count }} / {{ summary.included_account_count }}
            </dd>
          </div>
          <div class="flex items-baseline justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.estimatedCoverage') }}</dt>
            <dd class="font-medium tabular-nums text-gray-900 dark:text-white">
              {{ window.data.estimated_account_count }} / {{ summary.included_account_count }}
            </dd>
          </div>
        </dl>

        <div v-if="window.hints.length" class="mt-3 space-y-1 text-xs text-gray-500 dark:text-gray-400">
          <p v-for="hint in window.hints" :key="hint">{{ hint }}</p>
        </div>
      </section>

      <div class="flex items-center justify-between border-t border-gray-200 pt-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
        <span>{{ t('admin.accounts.openaiUsageSummary.updatedAt') }}</span>
        <span class="tabular-nums">{{ formatDateTime(summary.generated_at) }}</span>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatCurrency, formatDateTime } from '@/utils/format'
import type { OpenAIOAuthUsageSummary, OpenAIOAuthUsageWindowSummary } from '@/types'

const props = defineProps<{
  show: boolean
  summary: OpenAIOAuthUsageSummary | null
  error: string | null
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'retry'): void
}>()

const { t } = useI18n()

const clampPercent = (value: number) => Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0))
const formatPercent = (value: number) => `${clampPercent(value).toFixed(1)}%`
const formatEstimate = (value: number | null) => value == null
  ? t('admin.accounts.openaiUsageSummary.pendingEstimate')
  : `~${formatCurrency(value)}`

const progressColor = (value: number) => {
  const percent = clampPercent(value)
  if (percent >= 95) return 'bg-red-500'
  if (percent >= 85) return 'bg-orange-500'
  if (percent >= 70) return 'bg-amber-500'
  return 'bg-primary-500'
}

const sourceMeta = (data: OpenAIOAuthUsageWindowSummary) => {
  const hints: string[] = []
  if (data.reference_source === 'historical' || data.reference_source === 'mixed') {
    hints.push(t('admin.accounts.openaiUsageSummary.historicalHint'))
  }
  if (data.unestimated_account_count > 0) {
    hints.push(t('admin.accounts.openaiUsageSummary.unestimatedHint', { count: data.unestimated_account_count }))
  }
  if (data.pending_sync_account_count > 0) {
    hints.push(t('admin.accounts.openaiUsageSummary.pendingSyncHint', { count: data.pending_sync_account_count }))
  }

  const sources = {
    current: {
      label: t('admin.accounts.openaiUsageSummary.sourceCurrent'),
      className: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    },
    historical: {
      label: t('admin.accounts.openaiUsageSummary.sourceHistorical'),
      className: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
    },
    mixed: {
      label: t('admin.accounts.openaiUsageSummary.sourceMixed'),
      className: 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
    },
    unavailable: {
      label: t('admin.accounts.openaiUsageSummary.sourceUnavailable'),
      className: 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    }
  } as const
  const source = sources[data.reference_source]
  return { hints, sourceLabel: source.label, sourceClass: source.className }
}

const windows = computed(() => {
  if (!props.summary) return []
  return [
    {
      key: 'five-hour',
      label: t('admin.accounts.openaiUsageSummary.fiveHourWindow'),
      data: props.summary.five_hour,
      ...sourceMeta(props.summary.five_hour)
    },
    {
      key: 'seven-day',
      label: t('admin.accounts.openaiUsageSummary.sevenDayWindow'),
      data: props.summary.seven_day,
      ...sourceMeta(props.summary.seven_day)
    }
  ]
})
</script>
