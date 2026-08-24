<template>
  <section
    data-testid="recharge-layout"
    class="recharge-checkout-panel purchase-checkout-grid grid min-w-0 gap-5 lg:items-start"
    :aria-label="t('payment.tabTopUp')"
  >
    <img
      data-testid="purchase-energy-orb"
      class="purchase-energy-orb"
      src="/assets/purchase/energy-orb.webp"
      width="1234"
      height="1274"
      alt=""
      aria-hidden="true"
      draggable="false"
      decoding="async"
      loading="lazy"
    />

    <div data-testid="recharge-controls" class="purchase-checkout-controls min-w-0 space-y-5">
      <RechargeAmountSelector
        :model-value="modelValue"
        :amounts="amounts"
        :min="min"
        :max="max"
        :currency="currency"
        :locale="locale"
        :max-fraction-digits="maxFractionDigits"
        :error="amountError"
        :format-amount="formatAmount"
        :format-credited-amount="oneToOneConfigured ? formatCreditedAmount : undefined"
        :recommended-amount="recommendedAmount"
        :show-header="true"
        :show-preset-meta="false"
        @update:model-value="emit('update:modelValue', $event)"
      />

      <RechargeMethodSelector
        v-if="coreMethods.length > 0"
        :methods="coreMethods"
        :selected="selectedMethod"
        :recommended-method="effectiveRecommendedMethod"
        :show-header="true"
        @select="emit('select-method', $event)"
      />

      <section
        v-else
        data-testid="recharge-method-empty"
        class="recharge-glass-card p-5 text-center text-sm text-content-secondary sm:p-6"
        role="status"
      >
        {{ t('payment.notAvailable') }}
      </section>
    </div>

    <RechargeOrderSummary
      :formatted-amount="formattedAmount"
      :formatted-fee="formattedFee"
      :formatted-total="formattedTotal"
      :formatted-estimated-credited-amount="formattedEstimatedCreditedAmount"
      :disabled="disabled || !oneToOneConfigured || !selectedCoreMethodAvailable"
      :submitting="submitting"
      :has-submitted="hasSubmitted"
      :error-message="errorMessage"
      :error-hint-message="errorHintMessage"
      :configuration-warning="configurationWarning"
      @submit="emit('submit')"
    />
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import {
  isBuiltInAlipayMethod,
  isBuiltInWxpayMethod,
} from '@/components/payment/providerConfig'
import RechargeAmountSelector from '@/components/payment/recharge/RechargeAmountSelector.vue'
import RechargeMethodSelector from '@/components/payment/recharge/RechargeMethodSelector.vue'
import RechargeOrderSummary from '@/components/payment/recharge/RechargeOrderSummary.vue'

const props = withDefaults(defineProps<{
  modelValue: number | null
  amounts: number[]
  min: number
  max: number
  currency: string
  locale?: string
  maxFractionDigits?: number
  amountError?: string
  formatAmount: (value: number) => string
  formatCreditedAmount?: (value: number) => string
  methods: PaymentMethodOption[]
  selectedMethod: string
  recommendedAmount?: number | null
  recommendedMethod?: string
  formattedAmount: string
  formattedFee: string
  formattedTotal: string
  formattedEstimatedCreditedAmount: string
  disabled: boolean
  submitting: boolean
  hasSubmitted?: boolean
  errorMessage?: string
  errorHintMessage?: string
  oneToOneConfigured: boolean
  configurationWarning?: string
}>(), {
  locale: undefined,
  maxFractionDigits: 2,
  amountError: '',
  recommendedAmount: 100,
  recommendedMethod: '',
  hasSubmitted: false,
  errorMessage: '',
  errorHintMessage: '',
  configurationWarning: '',
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  'select-method': [type: string]
  submit: []
}>()

const { t } = useI18n()

const coreMethods = computed(() => {
  const firstAvailableOrConfigured = (matches: (type: string) => boolean) =>
    props.methods.find(method => matches(method.type) && method.available)
    ?? props.methods.find(method => matches(method.type))

  return [
    firstAvailableOrConfigured(isBuiltInAlipayMethod),
    firstAvailableOrConfigured(isBuiltInWxpayMethod),
  ].filter((method): method is PaymentMethodOption => method !== undefined)
})

const effectiveRecommendedMethod = computed(() => {
  const configured = props.recommendedMethod.trim()
  if (configured && coreMethods.value.some(method => method.type === configured)) {
    return configured
  }
  return coreMethods.value.find(method =>
    method.available && isBuiltInAlipayMethod(method.type),
  )?.type ?? ''
})

const selectedCoreMethodAvailable = computed(() => coreMethods.value.some(method =>
  method.type === props.selectedMethod && method.available,
))
</script>

<style scoped>
.recharge-checkout-panel {
  position: relative;
}

.purchase-checkout-controls,
.recharge-checkout-panel :deep(.recharge-summary-card) {
  z-index: 1;
}

.purchase-energy-orb {
  position: absolute;
  top: -4.6rem;
  left: -1.75rem;
  z-index: 0;
  width: clamp(6.5rem, 9vw, 8.25rem);
  height: auto;
  filter: drop-shadow(0 0.85rem 1rem rgb(67 56 202 / 24%));
  pointer-events: none;
  user-select: none;
  transform: rotate(-9deg);
  -webkit-user-drag: none;
}

@media (max-width: 1023px) {
  .purchase-energy-orb {
    display: none;
  }

  .recharge-checkout-panel :deep(.recharge-summary-card) {
    position: static;
  }
}
</style>
