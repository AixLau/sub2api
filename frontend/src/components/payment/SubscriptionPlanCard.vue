<template>
  <div
    :class="[
      'subscription-liquid-plan-card group relative flex flex-col transition-all',
      'hover:-translate-y-0.5',
    ]"
  >
    <div class="flex min-h-[190px] flex-1 flex-col p-4 sm:p-5">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <h3 class="break-words text-lg font-extrabold leading-tight text-slate-950">{{ plan.name }}</h3>
          <p
            v-if="plan.description"
            data-testid="subscription-plan-description"
            class="mt-2 line-clamp-2 text-sm leading-relaxed text-slate-500"
          >
            {{ plan.description }}
          </p>
        </div>
        <span :class="['shrink-0 rounded-full px-2 py-1 text-xs font-bold', badgeLightClass]">
          {{ pLabel }}
        </span>
      </div>

      <!-- Features list (compact) -->
      <div v-if="plan.features.length > 0" data-testid="subscription-plan-features" class="mb-4 space-y-2">
        <div v-for="feature in plan.features" :key="feature" class="flex items-start gap-2">
          <svg class="mt-0.5 h-4 w-4 flex-shrink-0 text-blue-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
          </svg>
          <span class="break-words text-xs leading-relaxed text-slate-600">{{ feature }}</span>
        </div>
      </div>

      <div class="flex-1" />

      <div class="mt-5 flex flex-wrap items-end justify-between gap-3">
        <div class="min-w-0">
          <div data-testid="subscription-plan-price" class="flex items-baseline gap-1">
            <span class="text-3xl font-extrabold tracking-normal text-blue-700">{{ formattedPrice }}</span>
            <span class="text-sm font-semibold text-slate-500">/ {{ validitySuffix }}</span>
          </div>
          <div v-if="plan.original_price" class="mt-1 flex items-center gap-1.5">
            <span class="text-sm text-slate-400 line-through">{{ formattedOriginalPrice }}</span>
            <span :class="['rounded-full px-1.5 py-0.5 text-[10px] font-semibold', discountClass]">{{ discountText }}</span>
          </div>
        </div>
        <p data-testid="subscription-plan-quota" class="text-sm font-bold text-slate-500">
          {{ quotaSummary }}
        </p>
      </div>

      <!-- Subscribe Button -->
      <button
        type="button"
        class="recharge-primary-button mt-5 w-full"
        @click="emit('select', plan)"
      >
        {{ isRenewal ? t('payment.renewNow') : t('payment.subscribeNow') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/types/payment'
import type { UserSubscription } from '@/types'
import { formatPaymentAmount } from '@/components/payment/currency'
import {
  platformBadgeLightClass,
  platformDiscountClass,
  platformLabel,
} from '@/utils/platformColors'

const props = defineProps<{ plan: SubscriptionPlan; activeSubscriptions?: UserSubscription[] }>()
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()

const platform = computed(() => props.plan.group_platform || '')
const isRenewal = computed(() =>
  props.activeSubscriptions?.some(s => s.group_id === props.plan.group_id && s.status === 'active') ?? false
)

// Derived color classes from central config
const badgeLightClass = computed(() => platformBadgeLightClass(platform.value))
const discountClass = computed(() => platformDiscountClass(platform.value))
const pLabel = computed(() => platformLabel(platform.value))

const formattedPrice = computed(() => formatPaymentAmount(props.plan.price, 'CNY'))
const formattedOriginalPrice = computed(() =>
  props.plan.original_price ? formatPaymentAmount(props.plan.original_price, 'CNY') : ''
)

const discountText = computed(() => {
  if (!props.plan.original_price || props.plan.original_price <= 0) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const quotaSummary = computed(() => {
  const monthly = props.plan.monthly_limit_usd
  if (monthly != null && monthly > 0) {
    return `${t('payment.planCard.monthlyQuota')} ${formatPaymentAmount(monthly, 'USD')}`
  }
  return t('payment.planCard.unlimited')
})

const validitySuffix = computed(() => {
  const u = props.plan.validity_unit || 'day'
  if (u === 'month') return t('payment.perMonth')
  if (u === 'year') return t('payment.perYear')
  return `${props.plan.validity_days}${t('payment.days')}`
})
</script>
