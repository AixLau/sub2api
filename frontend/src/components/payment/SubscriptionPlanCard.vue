<template>
  <div
    :class="[
      'subscription-liquid-plan-card group relative flex flex-col overflow-hidden transition-all',
      'hover:-translate-y-0.5',
      borderClass,
    ]"
  >
    <!-- Colored top accent bar -->
    <div :class="['subscription-liquid-accent', accentClass]" />

    <div class="flex flex-1 flex-col p-4 sm:p-5">
      <!-- Header: name + badge + price -->
      <div class="mb-4 flex items-start justify-between gap-3">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="break-words text-base font-bold text-slate-950">{{ plan.name }}</h3>
            <span :class="['shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold', badgeLightClass]">
              {{ pLabel }}
            </span>
          </div>
          <p v-if="plan.description" class="mt-1 line-clamp-2 text-xs leading-relaxed text-slate-500">
            {{ plan.description }}
          </p>
        </div>
        <div class="shrink-0 text-right">
          <div data-testid="subscription-plan-price" class="flex items-baseline justify-end gap-1">
            <span class="text-xs font-semibold text-blue-500">$</span>
            <span class="text-2xl font-extrabold tracking-normal text-blue-700">{{ plan.price }}</span>
          </div>
          <span class="text-[11px] text-slate-500">/ {{ validitySuffix }}</span>
          <div v-if="plan.original_price" class="mt-0.5 flex items-center justify-end gap-1.5">
            <span class="text-xs text-slate-400 line-through">${{ plan.original_price }}</span>
            <span :class="['rounded-full px-1.5 py-0.5 text-[10px] font-semibold', discountClass]">{{ discountText }}</span>
          </div>
        </div>
      </div>

      <!-- Group quota info (compact) -->
      <div class="subscription-detail-grid mb-4">
        <div class="subscription-detail-item">
          <span>{{ t('payment.planCard.rate') }}</span>
          <strong>{{ rateDisplay }}</strong>
        </div>
        <div v-if="hasPeakRate" class="subscription-detail-item sm:col-span-2">
          <span>{{ t('payment.planCard.peakRate') }}</span>
          <strong class="text-right text-amber-700">{{ peakRateDisplay }}</strong>
        </div>
        <div v-if="plan.daily_limit_usd != null" class="subscription-detail-item">
          <span>{{ t('payment.planCard.dailyLimit') }}</span>
          <strong>${{ plan.daily_limit_usd }}</strong>
        </div>
        <div v-if="plan.weekly_limit_usd != null" class="subscription-detail-item">
          <span>{{ t('payment.planCard.weeklyLimit') }}</span>
          <strong>${{ plan.weekly_limit_usd }}</strong>
        </div>
        <div v-if="plan.monthly_limit_usd != null" class="subscription-detail-item">
          <span>{{ t('payment.planCard.monthlyLimit') }}</span>
          <strong>${{ plan.monthly_limit_usd }}</strong>
        </div>
        <div v-if="plan.daily_limit_usd == null && plan.weekly_limit_usd == null && plan.monthly_limit_usd == null" class="subscription-detail-item">
          <span>{{ t('payment.planCard.quota') }}</span>
          <strong>{{ t('payment.planCard.unlimited') }}</strong>
        </div>
        <div v-if="modelScopeLabels.length > 0" class="subscription-detail-item sm:col-span-2">
          <span>{{ t('payment.planCard.models') }}</span>
          <div class="flex flex-wrap justify-end gap-1">
            <span v-for="scope in modelScopeLabels" :key="scope"
              class="rounded-full bg-blue-50 px-1.5 py-0.5 text-[10px] font-semibold text-blue-700">
              {{ scope }}
            </span>
          </div>
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

      <!-- Subscribe Button -->
      <button
        type="button"
        class="recharge-primary-button w-full"
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
import { useAppStore } from '@/stores/app'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import {
  platformAccentBarClass,
  platformBadgeLightClass,
  platformBorderClass,
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
const accentClass = computed(() => platformAccentBarClass(platform.value))
const borderClass = computed(() => platformBorderClass(platform.value))
const badgeLightClass = computed(() => platformBadgeLightClass(platform.value))
const discountClass = computed(() => platformDiscountClass(platform.value))
const pLabel = computed(() => platformLabel(platform.value))

const discountText = computed(() => {
  if (!props.plan.original_price || props.plan.original_price <= 0) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const rateDisplay = computed(() => {
  const rate = props.plan.rate_multiplier ?? 1
  return `×${Number(rate.toPrecision(10))}`
})

const appStore = useAppStore()

const hasPeakRate = computed(() => groupHasPeakRate(props.plan))

const peakRateDisplay = computed(() => {
  return formatPeakRateWindow(props.plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
})

const MODEL_SCOPE_LABELS: Record<string, string> = {
  claude: 'Claude',
  gemini_text: 'Gemini',
  gemini_image: 'Imagen',
}

const modelScopeLabels = computed(() => {
  if (platform.value !== 'antigravity') return []
  const scopes = props.plan.supported_model_scopes
  if (!scopes || scopes.length === 0) return []
  return scopes.map(s => MODEL_SCOPE_LABELS[s] || s)
})

const validitySuffix = computed(() => {
  const u = props.plan.validity_unit || 'day'
  if (u === 'month') return t('payment.perMonth')
  if (u === 'year') return t('payment.perYear')
  return `${props.plan.validity_days}${t('payment.days')}`
})
</script>
