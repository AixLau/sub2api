<template>
  <section class="card p-5" aria-labelledby="paid-user-retention-title">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div>
        <h3 id="paid-user-retention-title" class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('payment.admin.paidUserRetention') }}
        </h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('payment.admin.paidUserRetentionHint') }}
        </p>
      </div>
      <p class="rounded-full bg-gray-100 px-3 py-1.5 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
        {{ t('payment.admin.totalChurned', { count: totalChurned, rate: formatRate(totalChurned) }) }}
      </p>
    </div>

    <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <div class="min-w-0 rounded-xl border border-primary-100 bg-primary-50/60 px-4 py-4 dark:border-primary-900/40 dark:bg-primary-950/20">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
          {{ t('payment.admin.totalPaidUsers') }}
        </p>
        <p class="mt-1 text-2xl font-bold text-gray-900 dark:text-white" data-testid="total-paid-users">
          {{ stats.total_paid_users }}
        </p>
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">100%</p>
      </div>

      <button
        v-for="item in churnItems"
        :key="item.bucket"
        type="button"
        class="group min-w-0 rounded-xl border border-gray-200 bg-white px-4 py-4 text-left transition-colors hover:border-primary-200 hover:bg-primary-50/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:bg-dark-800 dark:hover:border-primary-800 dark:hover:bg-primary-950/20"
        :data-testid="`paid-churn-${item.bucket}`"
        @click="openUsers(item.bucket)"
      >
        <span class="block text-xs font-medium text-gray-500 dark:text-gray-400">{{ item.label }}</span>
        <span class="mt-1 flex items-center gap-2">
          <span class="text-2xl font-bold text-gray-900 dark:text-white">{{ item.count }}</span>
          <Icon name="chevronRight" size="sm" class="text-gray-400 transition-transform group-hover:translate-x-0.5 group-hover:text-primary-500" />
        </span>
        <span class="mt-1 block text-xs text-gray-400 dark:text-gray-500">{{ formatRate(item.count) }}</span>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import type { PaidChurnStats } from '@/types/payment'

const props = defineProps<{ stats: PaidChurnStats }>()
const { t } = useI18n()
const router = useRouter()

const totalChurned = computed(() =>
  props.stats.days_7_to_14 + props.stats.days_15_to_29 + props.stats.days_30_plus
)

const churnItems = computed(() => [
  { bucket: '7_14', label: t('payment.admin.churnDays7To14'), count: props.stats.days_7_to_14 },
  { bucket: '15_29', label: t('payment.admin.churnDays15To29'), count: props.stats.days_15_to_29 },
  { bucket: '30_plus', label: t('payment.admin.churnDays30Plus'), count: props.stats.days_30_plus },
])

function formatRate(count: number): string {
  if (props.stats.total_paid_users <= 0) return '0%'
  return `${((count / props.stats.total_paid_users) * 100).toFixed(1)}%`
}

function openUsers(bucket: string) {
  void router.push({ name: 'AdminUsers', query: { paid_churn: bucket } })
}
</script>
