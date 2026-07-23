<template>
  <div class="card overflow-hidden">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700 sm:px-6">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('usage.ranking.title') }}
        </h2>
      </div>
      <button
        type="button"
        class="btn btn-secondary px-2"
        :disabled="loading"
        :title="t('common.refresh')"
        :aria-label="t('common.refresh')"
        @click="load"
      >
        <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
      </button>
    </div>

    <div class="overflow-x-auto">
      <table class="w-full divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th class="w-16 px-3 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400 sm:w-20 sm:px-6">
              {{ t('usage.ranking.rank') }}
            </th>
            <th class="px-3 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400 sm:px-4">
              {{ t('usage.ranking.user') }}
            </th>
            <th class="px-3 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400 sm:px-4">
              {{ t('usage.ranking.totalTokens') }}
            </th>
            <th class="hidden px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400 sm:table-cell sm:px-6">
              {{ t('usage.ranking.requests') }}
            </th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
          <tr v-if="loading && items.length === 0">
            <td colspan="4" class="py-12 text-center">
              <LoadingSpinner />
            </td>
          </tr>
          <tr v-else-if="loadFailed">
            <td colspan="4" class="px-4 py-12 text-center">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('usage.ranking.failedToLoad') }}</p>
              <button type="button" class="btn btn-secondary btn-sm mt-3" @click="load">
                {{ t('common.retry') }}
              </button>
            </td>
          </tr>
          <tr v-else-if="items.length === 0">
            <td colspan="4" class="px-4 py-12 text-center text-sm text-gray-400 dark:text-gray-500">
              {{ t('usage.ranking.empty') }}
            </td>
          </tr>
          <tr
            v-for="item in items"
            v-else
            :key="item.rank"
            :aria-current="item.is_current ? 'true' : undefined"
            class="transition-colors"
            :class="item.is_current
              ? 'border-l-4 border-l-primary-500 bg-primary-50/80 dark:bg-primary-500/10'
              : 'border-l-4 border-l-transparent hover:bg-gray-50 dark:hover:bg-dark-700/40'"
          >
            <td class="px-3 py-3 sm:px-6">
              <span
                v-if="item.rank <= 3"
                class="inline-flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold"
                :class="rankBadgeClasses[item.rank - 1]"
              >{{ item.rank }}</span>
              <span v-else class="inline-block w-6 text-center text-sm tabular-nums text-gray-500 dark:text-gray-400">
                {{ item.rank }}
              </span>
            </td>
            <td class="max-w-[180px] px-3 py-3 text-sm font-medium text-gray-700 dark:text-gray-200 sm:max-w-[280px] sm:px-4">
              <span class="break-all">{{ item.display_name }}</span>
              <span
                v-if="item.is_current"
                class="ml-2 inline-flex rounded bg-primary-100 px-1.5 py-0.5 text-xs font-semibold text-primary-700 dark:bg-primary-500/20 dark:text-primary-300"
              >{{ t('usage.ranking.me') }}</span>
              <span class="mt-0.5 block text-xs font-normal text-gray-400 dark:text-gray-500 sm:hidden">
                {{ t('usage.ranking.requests') }} {{ item.requests.toLocaleString() }}
              </span>
            </td>
            <td class="whitespace-nowrap px-3 py-3 text-right text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100 sm:px-4">
              {{ formatCompactNumber(item.total_tokens) }}
            </td>
            <td class="hidden whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums text-gray-500 dark:text-gray-400 sm:table-cell sm:px-6">
              {{ item.requests.toLocaleString() }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { usageAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { formatCompactNumber } from '@/utils/format'
import type { UserUsageRankingItem } from '@/api/usage'

const props = defineProps<{
  startDate: string
  endDate: string
  startTime?: string
  endTime?: string
}>()

const { t } = useI18n()
const items = ref<UserUsageRankingItem[]>([])
const loading = ref(false)
const loadFailed = ref(false)
let requestSequence = 0

const rankBadgeClasses = [
  'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-400',
  'bg-gray-200 text-gray-600 dark:bg-gray-500/20 dark:text-gray-300',
  'bg-orange-100 text-orange-700 dark:bg-orange-500/20 dark:text-orange-400',
]

const load = async () => {
  const sequence = ++requestSequence
  loading.value = true
  loadFailed.value = false
  try {
    const response = await usageAPI.getUserRanking({
      start_date: props.startDate,
      end_date: props.endDate,
      start_time: props.startTime,
      end_time: props.endTime,
    })
    if (sequence !== requestSequence) return
    items.value = response.ranking ?? []
  } catch {
    if (sequence !== requestSequence) return
    items.value = []
    loadFailed.value = true
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

watch(
  () => [props.startDate, props.endDate, props.startTime, props.endTime],
  () => void load(),
  { immediate: true }
)
</script>
