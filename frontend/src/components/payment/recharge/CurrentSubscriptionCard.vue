<template>
  <section data-testid="current-subscription-card" class="subscription-current-card recharge-glass-card p-5 sm:p-6">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div class="min-w-0">
        <div class="flex items-center gap-2 text-sm font-semibold text-slate-600">
          <span class="subscription-current-icon">
            <Icon name="badge" size="sm" :stroke-width="2.2" />
          </span>
          {{ t('payment.activeSubscription') }}
        </div>
        <p class="mt-2 break-words text-2xl font-semibold tracking-normal text-slate-950 sm:text-3xl">
          {{ subscription.planName }}
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2 sm:justify-end">
        <span class="subscription-current-platform">
          {{ subscription.platform }}
        </span>
        <span class="subscription-current-remaining">
          {{ subscription.remainingText }}
        </span>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  subscription: {
    planName: string
    platform: string
    remainingText: string
  }
}>()

const { t } = useI18n()
</script>
