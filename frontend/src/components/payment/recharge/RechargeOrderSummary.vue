<template>
  <aside data-testid="order-summary" class="recharge-glass-card recharge-summary-card p-5 sm:p-6" aria-labelledby="order-summary-title">
    <p id="order-summary-title" class="recharge-section-title">{{ t('payment.rechargeUi.orderSummary') }}</p>

    <div class="mt-5 space-y-4 text-sm">
      <div class="flex items-center justify-between gap-4">
        <span class="text-slate-500">{{ t('payment.paymentAmount') }}</span>
        <span class="font-semibold text-slate-950">{{ formattedAmount }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="inline-flex items-center gap-1 text-slate-500">
          {{ t('payment.fee') }}
          <Icon name="infoCircle" size="xs" class="text-slate-400" />
        </span>
        <span class="font-semibold text-slate-950">{{ formattedFee }}</span>
      </div>
      <div class="border-t border-slate-200/70 pt-4">
        <div class="flex items-center justify-between gap-4">
          <span class="font-semibold text-slate-950">{{ t('payment.actualPay') }}</span>
          <span class="text-2xl font-semibold tracking-normal text-blue-700">{{ formattedTotal }}</span>
        </div>
      </div>
      <div class="border-t border-slate-200/70 pt-4">
        <div
          data-testid="estimated-credited-highlight"
          class="recharge-summary-highlight"
        >
          <span class="text-sm font-semibold text-slate-600">{{ t('payment.rechargeUi.estimatedCreditedAmount') }}</span>
          <span class="recharge-summary-highlight-value">{{ formattedEstimatedCreditedAmount }}</span>
        </div>
        <div class="mt-4 flex items-center justify-between gap-4">
          <span class="text-slate-500">{{ t('payment.rechargeUi.arrivalTime') }}</span>
          <span class="font-semibold text-emerald-600">{{ t('payment.rechargeUi.instantArrivalShort') }}</span>
        </div>
      </div>
    </div>

    <div v-if="errorMessage" class="mt-5 rounded-2xl border border-red-200 bg-red-50/80 px-4 py-3 text-sm text-red-700" role="alert" aria-live="polite">
      <p>{{ errorMessage }}</p>
      <p v-if="errorHintMessage" class="mt-1 text-red-600/80">{{ errorHintMessage }}</p>
    </div>

    <button
      data-testid="submit-recharge"
      type="button"
      class="recharge-primary-button mt-6 w-full"
      :disabled="disabled || submitting"
      :aria-busy="submitting"
      @click="emit('submit')"
    >
      <span v-if="submitting" class="flex items-center justify-center gap-2">
        <span class="h-4 w-4 animate-spin rounded-full border-2 border-white/80 border-t-transparent"></span>
        {{ t('common.processing') }}
      </span>
      <span v-else>{{ ctaText }}</span>
    </button>

    <label class="mt-4 flex items-start justify-center gap-2 text-xs text-slate-500">
      <input
        type="checkbox"
        checked
        disabled
        class="mt-0.5 h-3.5 w-3.5 rounded border-slate-300 text-blue-600"
      />
      <span>
        {{ t('payment.rechargeUi.agreementPrefix') }}
        <span class="font-medium text-blue-700">{{ t('payment.rechargeUi.agreementName') }}</span>
      </span>
    </label>
  </aside>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  formattedAmount: string
  formattedFee: string
  formattedTotal: string
  formattedEstimatedCreditedAmount: string
  disabled: boolean
  submitting: boolean
  hasSubmitted?: boolean
  errorMessage?: string
  errorHintMessage?: string
}>()

const emit = defineEmits<{
  submit: []
}>()

const { t } = useI18n()
const ctaText = computed(() => props.hasSubmitted && props.errorMessage
  ? t('payment.rechargeUi.retryRecharge')
  : t('payment.rechargeUi.rechargeNow')
)
</script>
