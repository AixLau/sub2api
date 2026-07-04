<template>
  <section data-testid="account-balance-hero" class="recharge-hero-card overflow-hidden p-5 sm:p-7">
    <div
      class="relative z-10 grid gap-6 md:items-center"
      :class="showBalance ? 'md:grid-cols-[minmax(0,1fr)_minmax(240px,0.65fr)]' : 'md:grid-cols-1'"
    >
      <div class="flex min-w-0 items-center gap-4">
        <div class="flex h-16 w-16 shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br from-blue-500 to-violet-400 text-xl font-semibold text-white shadow-[0_16px_32px_rgba(37,99,235,0.22)]">
          {{ initials }}
        </div>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="truncate text-lg font-semibold text-slate-950">{{ accountName }}</h2>
            <span class="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-semibold text-emerald-700 ring-1 ring-emerald-100">
              <Icon name="shield" size="xs" :stroke-width="2.2" />
              {{ t('payment.rechargeUi.verified') }}
            </span>
          </div>
          <p class="mt-2 text-sm text-slate-500">
            {{ t('payment.rechargeUi.accountId') }} {{ accountId }}
          </p>
        </div>
      </div>

      <div v-if="showBalance" class="md:border-l md:border-white/70 md:pl-8">
        <div class="flex items-center gap-2 text-sm font-medium text-slate-600">
          {{ t('payment.rechargeUi.availableBalance') }}
          <Icon name="eye" size="sm" class="text-slate-500" />
        </div>
        <p class="mt-2 text-3xl font-semibold tracking-normal text-slate-950 sm:text-4xl">
          {{ formattedBalance }}
        </p>
      </div>
      <div v-else-if="subscriptionSummary" class="md:border-l md:border-white/70 md:pl-8">
        <div class="flex items-center gap-2 text-sm font-medium text-slate-600">
          {{ t('payment.activeSubscription') }}
          <Icon name="badge" size="sm" class="text-blue-500" />
        </div>
        <p class="mt-2 text-2xl font-semibold tracking-normal text-slate-950 sm:text-3xl">
          {{ subscriptionSummary.planName }}
        </p>
        <div class="mt-3 flex flex-wrap items-center gap-2 text-sm">
          <span class="rounded-full bg-blue-100 px-2.5 py-1 font-semibold text-blue-700">
            {{ subscriptionSummary.platform }}
          </span>
          <span class="text-slate-500">{{ subscriptionSummary.remainingText }}</span>
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
  accountId: string
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
