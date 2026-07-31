<template>
  <section data-testid="account-balance-hero" class="recharge-hero-card overflow-hidden p-5 sm:p-7">
    <div
      class="relative z-10 grid gap-6 md:items-center"
      :class="showBalance ? 'md:grid-cols-[minmax(0,1fr)_minmax(240px,0.65fr)]' : 'md:grid-cols-1'"
    >
      <div class="flex min-w-0 items-center gap-4">
        <div class="flex h-16 w-16 shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br from-primary-500 to-accent-400 text-xl font-semibold text-white shadow-glow">
          {{ initials }}
        </div>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="truncate text-lg font-semibold text-content-primary">{{ accountName }}</h2>
            <span class="inline-flex items-center gap-1 rounded-full bg-status-success-soft px-2.5 py-1 text-xs font-semibold text-status-success ring-1 ring-status-success-border">
              <Icon name="shield" size="xs" :stroke-width="2.2" />
              {{ t('payment.rechargeUi.verified') }}
            </span>
          </div>
        </div>
      </div>

      <div v-if="showBalance" class="md:border-l md:border-line-subtle md:pl-8">
        <div class="flex items-center gap-2 text-sm font-medium text-content-secondary">
          {{ t('payment.rechargeUi.availableBalance') }}
          <Icon name="eye" size="sm" class="text-content-tertiary" />
        </div>
        <p class="mt-2 text-3xl font-semibold tracking-normal text-content-primary sm:text-4xl">
          {{ formattedBalance }}
        </p>
      </div>
      <div v-else-if="subscriptionSummary" class="md:border-l md:border-line-subtle md:pl-8">
        <div class="flex items-center gap-2 text-sm font-medium text-content-secondary">
          {{ t('payment.activeSubscription') }}
          <Icon name="badge" size="sm" class="text-content-brand" />
        </div>
        <p class="mt-2 text-2xl font-semibold tracking-normal text-content-primary sm:text-3xl">
          {{ subscriptionSummary.planName }}
        </p>
        <div class="mt-3 flex flex-wrap items-center gap-2 text-sm">
          <span class="rounded-full bg-status-info-soft px-2.5 py-1 font-semibold text-content-brand">
            {{ subscriptionSummary.platform }}
          </span>
          <span class="text-content-tertiary">{{ subscriptionSummary.remainingText }}</span>
        </div>
      </div>
    </div>

    <div class="recharge-wallet-art" aria-hidden="true">
      <Icon name="creditCard" size="xl" />
      <span class="recharge-wallet-plus">
        <Icon name="plus" size="md" :stroke-width="2.2" />
      </span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  accountName: string
  formattedBalance: string
  showBalance?: boolean
  subscriptionSummary?: {
    planName: string
    platform: string
    remainingText: string
  } | null
}>(), {
  showBalance: true,
  subscriptionSummary: null,
})

const { t } = useI18n()
const initials = computed(() => {
  const source = props.accountName.trim() || 'U'
  return source.slice(0, 2).toUpperCase()
})
</script>
