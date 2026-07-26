<template>
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
    <div :class="statCardClass">
      <div class="rounded-lg bg-blue-50 p-2 text-blue-600 ring-1 ring-blue-500/10 dark:bg-blue-400/10 dark:text-blue-400 dark:ring-blue-400/20">
        <Icon name="document" size="md" />
      </div>
      <div>
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('usage.totalRequests') }}</p>
        <p :class="valueClass"><NumberTicker :value="stats?.total_requests || 0" :format-fn="formatCount" /></p>
        <p class="text-xs text-gray-400">{{ t('usage.inSelectedRange') }}</p>
      </div>
    </div>
    <div :class="statCardClass">
      <div class="rounded-lg bg-amber-50 p-2 text-amber-600 ring-1 ring-amber-500/10 dark:bg-amber-400/10 dark:text-amber-400 dark:ring-amber-400/20"><svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m21 7.5-9-5.25L3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9" /></svg></div>
      <div>
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('usage.totalTokens') }}</p>
        <p :class="valueClass"><NumberTicker :value="stats?.total_tokens || 0" :format-fn="formatTokens" /></p>
        <p class="flex flex-wrap items-center gap-x-1 text-xs text-gray-500">
          <span>{{ t('usage.in') }}: {{ formatTokens(stats?.total_input_tokens || 0) }}</span>
          <span>/</span>
          <span>{{ t('usage.out') }}: {{ formatTokens(stats?.total_output_tokens || 0) }}</span>
          <span>/</span>
          <span class="group relative inline-flex cursor-help items-center gap-0.5" tabindex="0">
            <span>{{ cacheLabel() }}: {{ formatTokens(stats?.total_cache_tokens || 0) }}</span>
            <svg
              class="h-3.5 w-3.5 text-gray-400"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            <span
              class="pointer-events-none absolute left-1/2 top-full z-30 mt-2 w-56 -translate-x-1/2 rounded-lg border border-gray-200 bg-white p-3 text-left text-xs text-gray-700 opacity-0 shadow-lg transition-opacity duration-150 group-hover:opacity-100 group-focus:opacity-100 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200"
            >
              <span class="mb-2 block font-medium text-gray-900 dark:text-white">
                {{ cacheDetailLabel() }}
              </span>
              <span class="flex items-center justify-between gap-3">
                <span>{{ t('usage.cacheCreationTokensLabel') }}</span>
                <span class="tabular-nums">
                  {{ formatTokens(stats?.total_cache_creation_tokens || 0) }}
                </span>
              </span>
              <span class="mt-1 flex items-center justify-between gap-3">
                <span>{{ t('usage.cacheReadTokensLabel') }}</span>
                <span class="tabular-nums">
                  {{ formatTokens(stats?.total_cache_read_tokens || 0) }}
                </span>
              </span>
            </span>
          </span>
        </p>
      </div>
    </div>
    <div v-if="showCost" :class="statCardClass">
      <div class="rounded-lg bg-emerald-50 p-2 text-emerald-600 ring-1 ring-emerald-500/10 dark:bg-emerald-400/10 dark:text-emerald-400 dark:ring-emerald-400/20">
        <Icon name="dollar" size="md" />
      </div>
      <div class="min-w-0 flex-1">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('usage.totalCost') }}</p>
        <p :class="[valueClass, 'text-emerald-600 dark:text-emerald-400']">
          <NumberTicker :value="stats?.total_actual_cost || 0" prefix="$" :format-fn="formatCost" />
        </p>
        <p v-if="showAccountCost || showStandardCost" class="text-xs text-gray-400">
          <template v-if="showAccountCost && totalAccountCost != null">
            <span class="text-orange-500">{{ t('usage.accountCost') }} ${{ totalAccountCost.toFixed(4) }}</span>
            <span v-if="showStandardCost"> · </span>
          </template>
          <span v-if="showStandardCost">
            {{ t('usage.standardCost') }}
            <span :class="{ 'line-through': strikeStandardCost }">${{ (stats?.total_cost || 0).toFixed(4) }}</span>
          </span>
        </p>
      </div>
    </div>
    <div :class="statCardClass">
      <div class="rounded-lg bg-violet-50 p-2 text-violet-600 ring-1 ring-violet-500/10 dark:bg-violet-400/10 dark:text-violet-400 dark:ring-violet-400/20">
        <Icon name="clock" size="md" />
      </div>
      <div><p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('usage.avgDuration') }}</p><p :class="valueClass"><NumberTicker :value="stats?.average_duration_ms || 0" :format-fn="formatDuration" /></p></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminUsageStatsResponse } from '@/api/admin/usage'
import type { UsageStatsResponse } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import NumberTicker from '@/components/inspira/NumberTicker.vue'

const props = withDefaults(defineProps<{
  stats: (AdminUsageStatsResponse | UsageStatsResponse) | null
  showAccountCost?: boolean
  showStandardCost?: boolean
  showCost?: boolean
  strikeStandardCost?: boolean
  surface?: 'default' | 'tremor'
}>(), {
  showAccountCost: true,
  showStandardCost: true,
  showCost: true,
  strikeStandardCost: false,
  surface: 'default',
})

const { t } = useI18n()

const totalAccountCost = computed(() => {
  const stats = props.stats as (AdminUsageStatsResponse & { total_account_cost?: number }) | null
  return stats?.total_account_cost ?? null
})
const showAccountCost = computed(() => props.showAccountCost)
const showStandardCost = computed(() => props.showStandardCost)
const showCost = computed(() => props.showCost)
const strikeStandardCost = computed(() => props.strikeStandardCost)
const statCardClass = computed(() => props.surface === 'tremor'
  ? 'flex items-center gap-3 rounded-lg border border-gray-200 bg-white p-4 text-left shadow-[0_1px_2px_rgba(15,23,42,0.04)] dark:border-gray-900 dark:bg-[#090E1A]'
  : 'card p-4 flex items-center gap-3')
const valueClass = computed(() => props.surface === 'tremor'
  ? 'mt-1 text-2xl font-semibold tracking-normal text-gray-950 dark:text-gray-50'
  : 'text-xl font-bold')

const formatDuration = (ms: number) =>
  ms < 1000 ? `${ms.toFixed(0)}ms` : `${(ms / 1000).toFixed(2)}s`

const formatTokens = (value: number) => {
  if (value >= 1e9) return (value / 1e9).toFixed(2) + 'B'
  if (value >= 1e6) return (value / 1e6).toFixed(2) + 'M'
  if (value >= 1e3) return (value / 1e3).toFixed(2) + 'K'
  return value.toLocaleString()
}

// NumberTicker 动画中间值为小数,取整后再走原展示逻辑,保证动画帧格式与最终值一致
const formatCount = (value: number) => Math.round(value).toLocaleString()
const formatCost = (value: number) => value.toFixed(4)

const cacheLabel = () => t('usage.cacheTotal')
const cacheDetailLabel = () => t('usage.cacheBreakdown')
</script>
