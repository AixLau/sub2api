<template>
  <section
    data-testid="current-subscription-card"
    class="subscription-current-card recharge-glass-card px-4 py-3 sm:px-5"
    :aria-label="t('payment.activeSubscription')"
  >
    <div class="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
      <div class="flex min-w-0 items-center gap-3">
        <span class="subscription-current-icon shrink-0">
          <Icon name="badge" size="sm" :stroke-width="2.2" />
        </span>
        <div class="min-w-0 sm:flex sm:items-baseline sm:gap-2">
          <span class="text-xs font-bold uppercase tracking-wide text-content-secondary">
            {{ t('payment.activeSubscription') }}
          </span>
          <p data-testid="current-subscription-name" class="truncate text-base font-black tracking-tight text-content-primary">
            {{ subscription.planName }}
          </p>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2 sm:justify-end">
        <span data-testid="current-subscription-platform" class="subscription-current-platform">
          {{ subscription.platform }}
        </span>
        <span data-testid="current-subscription-remaining" class="subscription-current-remaining tabular-nums">
          {{ subscription.remainingText }}
        </span>
      </div>
    </div>
    <div
      v-if="subscription.pendingCount > 0"
      data-testid="current-subscription-pending-renewals"
      class="mt-2.5 flex flex-col gap-1 border-t border-status-warning-border pt-2.5 text-xs sm:flex-row sm:items-center sm:justify-between"
    >
      <span class="font-semibold text-status-warning">
        {{ t('userSubscriptions.pendingRenewals', { count: subscription.pendingCount }) }}
        · {{ t('userSubscriptions.pendingRenewalTotalDays', { days: subscription.pendingDays }) }}
      </span>
      <span class="text-xs text-content-secondary">
        {{ t('userSubscriptions.pendingRenewalRule') }}
      </span>
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
    pendingCount: number
    pendingDays: number
  }
}>()

const { t } = useI18n()
</script>
