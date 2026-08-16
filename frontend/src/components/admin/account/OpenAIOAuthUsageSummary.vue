<template>
  <section data-testid="openai-oauth-usage-summary" class="mt-3 border-y border-gray-200 py-3 dark:border-dark-700" aria-live="polite">
    <div class="mb-2 flex min-h-6 items-center justify-between gap-3">
      <div class="flex min-w-0 items-baseline gap-2">
        <h2 class="truncate text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.openaiUsageSummary.title') }}
        </h2>
        <span v-if="summary" class="flex-shrink-0 whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.openaiUsageSummary.accountCount', { count: summary.account_count }) }}
        </span>
      </div>
      <span v-if="loading" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </span>
    </div>

    <p v-if="error && summary" class="mb-2 text-xs text-red-600 dark:text-red-400">
      {{ t('admin.accounts.openaiUsageSummary.loadFailed') }}
    </p>

    <div v-if="error && !summary" class="text-sm text-red-600 dark:text-red-400">
      {{ t('admin.accounts.openaiUsageSummary.loadFailed') }}
    </div>
    <div v-else-if="!summary" class="grid grid-cols-1 gap-2 md:grid-cols-2">
      <div v-for="item in 2" :key="item" class="h-28 animate-pulse rounded-md bg-gray-100 dark:bg-dark-800" />
    </div>
    <div v-else class="grid grid-cols-1 gap-2 md:grid-cols-2">
      <article
        v-for="window in windows"
        :key="window.key"
        :data-testid="`usage-window-${window.key}`"
        class="rounded-md border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800"
      >
        <div class="mb-2 flex items-center justify-between gap-3">
          <h3 class="text-xs font-semibold uppercase text-gray-600 dark:text-gray-300">{{ window.label }}</h3>
          <span class="font-mono text-xs font-medium text-gray-700 dark:text-gray-200">
            {{ formatPercent(window.data.usage_percent) }} / {{ formatPercent(window.data.remaining_percent) }}
          </span>
        </div>

        <div
          class="relative mb-3 h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600"
          role="progressbar"
          :aria-label="window.label"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="clampPercent(window.data.usage_percent)"
        >
          <div
            class="absolute inset-0 bg-gradient-to-r from-emerald-500 via-amber-400 to-red-500"
            aria-hidden="true"
          />
          <div
            data-testid="usage-progress-mask"
            class="absolute inset-y-0 right-0 bg-gray-200 transition-[width] duration-300 dark:bg-dark-600"
            :style="{ width: `${100 - clampPercent(window.data.usage_percent)}%` }"
            aria-hidden="true"
          />
        </div>

        <dl class="grid grid-cols-1 gap-1.5 sm:grid-cols-3 sm:gap-2">
          <div class="flex min-w-0 items-baseline justify-between gap-3 sm:block">
            <dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.used') }}</dt>
            <dd class="min-w-0 text-right text-sm font-semibold text-gray-900 dark:text-white sm:mt-0.5 sm:text-left">{{ formatCurrency(window.data.used) }}</dd>
          </div>
          <div class="flex min-w-0 items-baseline justify-between gap-3 sm:block">
            <dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.remaining') }}</dt>
            <dd class="min-w-0 text-right text-sm font-semibold text-gray-900 dark:text-white sm:mt-0.5 sm:text-left">{{ formatEstimate(window.data.estimated_remaining) }}</dd>
          </div>
          <div class="flex min-w-0 items-baseline justify-between gap-3 sm:block">
            <dt class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.capacity') }}</dt>
            <dd class="min-w-0 text-right text-sm font-semibold text-gray-900 dark:text-white sm:mt-0.5 sm:text-left">{{ formatEstimate(window.data.estimated_capacity) }}</dd>
          </div>
        </dl>

        <p v-if="window.data.reference_source === 'historical' || window.data.reference_source === 'mixed'" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.openaiUsageSummary.historicalHint') }}
        </p>
        <p v-if="window.data.unestimated_account_count > 0" class="mt-2 text-xs text-amber-700 dark:text-amber-300">
          {{ t('admin.accounts.openaiUsageSummary.unestimatedHint', { count: window.data.unestimated_account_count }) }}
        </p>
        <p v-if="window.data.pending_sync_account_count > 0" class="mt-1 text-xs text-blue-700 dark:text-blue-300">
          {{ t('admin.accounts.openaiUsageSummary.pendingSyncHint', { count: window.data.pending_sync_account_count }) }}
        </p>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatCurrency } from '@/utils/format'
import type { OpenAIOAuthUsageSummary } from '@/types'

const props = defineProps<{
  summary: OpenAIOAuthUsageSummary | null
  loading: boolean
  error: string | null
}>()

const { t } = useI18n()

const windows = computed(() => props.summary ? [
  { key: 'five_hour', label: t('admin.accounts.openaiUsageSummary.fiveHour'), data: props.summary.five_hour },
  { key: 'seven_day', label: t('admin.accounts.openaiUsageSummary.sevenDay'), data: props.summary.seven_day }
] : [])

const clampPercent = (value: number) => Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0))
const formatPercent = (value: number) => `${clampPercent(value).toFixed(1)}%`
const formatEstimate = (value: number | null) => value == null ? '--' : `~${formatCurrency(value)}`
</script>
