<template>
  <div
    :class="[
      'subscription-liquid-plan-card group relative flex flex-col transition-all',
      'hover:-translate-y-0.5',
    ]"
  >
    <BorderBeam v-if="showHighlightBeam" :size="150" :duration="12" :border-width="1.5" />
    <div class="flex min-h-[190px] flex-1 flex-col p-4 sm:p-5">
      <div>
        <div class="min-w-0">
          <h3 class="break-words text-lg font-extrabold leading-tight text-slate-950">{{ plan.name }}</h3>
          <p
            v-if="planDisplay.description"
            data-testid="subscription-plan-description"
            class="mt-2 line-clamp-2 text-sm leading-relaxed text-slate-500"
          >
            {{ planDisplay.description }}
          </p>
        </div>
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
        <div v-if="planDisplay.quotaSummary || planDisplay.validitySummary" class="min-w-0">
          <p
            v-if="planDisplay.quotaSummary"
            data-testid="subscription-plan-quota-summary"
            class="text-2xl font-extrabold tracking-normal text-slate-950"
          >
            {{ planDisplay.quotaSummary }}
          </p>
          <p
            v-if="validitySummary"
            data-testid="subscription-plan-validity-summary"
            class="mt-1 text-sm font-semibold text-slate-500"
          >
            {{ validitySummary }}
          </p>
        </div>
        <div class="min-w-0">
          <div data-testid="subscription-plan-price">
            <span class="text-3xl font-extrabold tracking-normal text-blue-700">{{ formattedPrice }}</span>
          </div>
          <div v-if="plan.original_price" class="mt-1 flex items-center gap-1.5">
            <span class="text-sm text-slate-400 line-through">{{ formattedOriginalPrice }}</span>
            <span :class="['rounded-full px-1.5 py-0.5 text-[10px] font-semibold', discountClass]">{{ discountText }}</span>
          </div>
        </div>
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
import { planValiditySuffix } from './validity'
import {
  platformDiscountClass,
} from '@/utils/platformColors'
import { buildSubscriptionPlanDisplay, buildSubscriptionPlanDisplayLabels } from '@/components/payment/subscriptionPlanDisplay'
import BorderBeam from '@/components/inspira/BorderBeam.vue'

const props = defineProps<{
  plan: SubscriptionPlan
  activeSubscriptions?: UserSubscription[]
  /** Force the animated border on/off; defaults to on for discounted plans */
  highlight?: boolean
}>()
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()
const platform = computed(() => props.plan.group_platform || '')

const showHighlightBeam = computed(() => {
  if (props.highlight !== undefined) return props.highlight
  return !!(props.plan.original_price && props.plan.original_price > props.plan.price)
})

const isRenewal = computed(() =>
  props.activeSubscriptions?.some(s => s.group_id === props.plan.group_id && s.status === 'active') ?? false
)

// Derived color classes from central config
const discountClass = computed(() => platformDiscountClass(platform.value))
const planDisplay = computed(() => buildSubscriptionPlanDisplay(props.plan, buildSubscriptionPlanDisplayLabels(t)))

const formattedPrice = computed(() => formatPaymentAmount(props.plan.price, props.plan.currency))
const formattedOriginalPrice = computed(() =>
  props.plan.original_price ? formatPaymentAmount(props.plan.original_price, props.plan.currency) : ''
)

const discountText = computed(() => {
  if (!props.plan.original_price || props.plan.original_price <= 0) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const validitySummary = computed(() => {
  const unit = String(props.plan.validity_unit || 'day').trim().toLowerCase()
  if (unit === 'day') return planDisplay.value.validitySummary
  return `/ ${planValiditySuffix(props.plan, t)}`
})
</script>
