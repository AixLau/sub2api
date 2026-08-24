<template>
  <aside
    data-testid="order-summary"
    class="recharge-glass-card recharge-summary-card p-4"
    aria-labelledby="subscription-order-summary-title"
  >
    <div class="flex items-center justify-between gap-3">
      <h2 id="subscription-order-summary-title" class="recharge-section-title">
        {{ t('payment.rechargeUi.orderSummary') }}
      </h2>
      <span
        v-if="option"
        data-testid="subscription-purchase-kind"
        class="rounded-full border border-blue-200 bg-blue-50 px-2 py-0.5 text-[10px] font-bold text-blue-700 dark:border-blue-700/60 dark:bg-blue-950/40 dark:text-blue-200"
      >
        {{ isRenewal ? t('payment.renewNow') : t('payment.subscriptionUi.oneTimePurchase') }}
      </span>
    </div>

    <div v-if="option" class="mt-4 space-y-2.5 text-xs">
      <div class="flex items-start justify-between gap-3">
        <span class="text-content-secondary">{{ t('payment.subscriptionUi.selectedPlan') }}</span>
        <span data-testid="subscription-summary-plan" class="max-w-[165px] break-words text-right font-bold text-content-primary">
          {{ option.title }}
        </span>
      </div>
      <div v-if="validityText" class="flex items-start justify-between gap-3">
        <span class="text-content-secondary">{{ t('payment.subscriptionUi.validity') }}</span>
        <span data-testid="subscription-summary-validity" class="text-right font-semibold text-content-primary">
          {{ validityText }}
        </span>
      </div>
      <div class="flex items-center justify-between gap-3">
        <span class="text-content-secondary">{{ t('payment.packagePrice') }}</span>
        <span data-testid="subscription-summary-price" class="tabular-nums font-semibold text-content-primary">
          {{ formattedBaseAmount }}
        </span>
      </div>
      <div v-if="hasDiscount" class="flex items-center justify-between gap-3">
        <span class="text-content-secondary">{{ t('payment.subscriptionUi.originalPrice') }}</span>
        <span class="tabular-nums text-content-tertiary line-through">{{ formattedOriginalAmount }}</span>
      </div>
      <div class="flex items-center justify-between gap-3">
        <span class="text-content-secondary">{{ t('payment.fee') }}</span>
        <span data-testid="subscription-summary-fee" class="tabular-nums font-semibold text-content-primary">
          {{ formattedFeeAmount }}
        </span>
      </div>
    </div>

    <div v-else class="mt-4 rounded-xl border border-dashed border-line-strong bg-surface-subtle px-4 py-6 text-center">
      <p data-testid="subscription-select-plan-first" class="text-sm font-semibold text-content-secondary">
        {{ t('payment.subscriptionUi.selectPlanFirst') }}
      </p>
    </div>

    <div class="my-4 border-t border-dashed border-line-strong" />

    <div class="flex items-end justify-between gap-3">
      <span class="text-sm font-black text-content-primary">{{ t('payment.actualPay') }}</span>
      <span data-testid="subscription-summary-total" class="tabular-nums text-2xl font-black tracking-tight text-blue-700 dark:text-blue-300">
        {{ formattedTotalAmount }}
      </span>
    </div>

    <div
      v-if="errorMessage"
      data-testid="subscription-order-error"
      class="mt-3 rounded-xl border border-status-danger-border bg-status-danger-soft px-3 py-2 text-xs text-status-danger"
      role="alert"
    >
      {{ errorMessage }}
    </div>

    <button
      type="button"
      data-testid="submit-subscription"
      class="recharge-primary-button mt-4 w-full"
      :disabled="!option || !canSubmit || submitting"
      :aria-busy="submitting"
      @click="submit"
    >
      {{ isRenewal ? t('payment.renewNow') : t('payment.subscribeNow') }}
    </button>
  </aside>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserSubscription } from '@/types'
import { formatPaymentAmount } from '@/components/payment/currency'
import { planValiditySuffix } from '@/components/payment/validity'
import {
  isPurchaseSubscriptionRenewal,
  ninePlusPaymentAmounts,
  purchaseSubscriptionDiscountAmount,
  type PurchaseSubscriptionOption,
} from './purchaseViewModels'

const props = withDefaults(defineProps<{
  option?: PurchaseSubscriptionOption | null
  activeSubscriptions?: UserSubscription[]
  paymentAmount?: number
  feeAmount?: number
  totalAmount?: number
  originalAmount?: number
  discountAmount?: number
  paymentCurrency?: string
  canSubmit?: boolean
  submitting?: boolean
  errorMessage?: string
}>(), {
  option: null,
  activeSubscriptions: () => [],
  paymentAmount: undefined,
  feeAmount: undefined,
  totalAmount: undefined,
  originalAmount: undefined,
  discountAmount: undefined,
  paymentCurrency: '',
  canSubmit: false,
  submitting: false,
  errorMessage: '',
})

const emit = defineEmits<{
  submit: [option: PurchaseSubscriptionOption]
}>()

const { t, locale } = useI18n()

const isRenewal = computed(() =>
  isPurchaseSubscriptionRenewal(props.option, props.activeSubscriptions)
)

const validityText = computed(() => {
  if (!props.option) return ''
  if (props.option.source === 'internal') return planValiditySuffix(props.option.plan, t)
  // NinePlus currently has no dedicated validity field. Do not infer a billing
  // period from product copy or present its delivery note as a validity date.
  return ''
})

const currency = computed(() => props.paymentCurrency || props.option?.currency || 'CNY')
const externalAmounts = computed(() =>
  props.option?.source === 'nineplus' ? ninePlusPaymentAmounts(props.option.product) : null
)
const baseAmount = computed(() =>
  props.paymentAmount ?? externalAmounts.value?.price ?? props.option?.price ?? 0
)
const fee = computed(() => props.feeAmount ?? externalAmounts.value?.fee ?? 0)
const total = computed(() =>
  props.totalAmount ?? externalAmounts.value?.total ?? baseAmount.value + fee.value
)
const original = computed(() =>
  props.originalAmount ?? props.option?.originalPrice ?? 0
)
const discount = computed(() => {
  if (props.discountAmount !== undefined) return Math.max(0, props.discountAmount)
  if (!props.option) return 0
  return purchaseSubscriptionDiscountAmount(props.option)
})
const hasDiscount = computed(() => original.value > baseAmount.value && discount.value > 0)

const formattedFeeAmount = computed(() => formatPaymentAmount(fee.value, currency.value, locale.value))
const formattedBaseAmount = computed(() => formatPaymentAmount(baseAmount.value, currency.value, locale.value))
const formattedTotalAmount = computed(() => formatPaymentAmount(total.value, currency.value, locale.value))
const formattedOriginalAmount = computed(() => formatPaymentAmount(original.value, currency.value, locale.value))

function submit() {
  if (!props.option || !props.canSubmit || props.submitting) return
  emit('submit', props.option)
}
</script>
