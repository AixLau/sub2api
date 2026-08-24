<template>
  <aside
    data-testid="order-summary"
    class="recharge-glass-card recharge-summary-card min-w-0 p-5 tabular-nums sm:p-6"
    aria-labelledby="order-summary-title"
  >
    <p id="order-summary-title" class="recharge-section-title">{{ t('payment.rechargeUi.orderSummary') }}</p>

    <div class="mt-5 space-y-4 text-sm">
      <div class="flex items-center justify-between gap-4">
        <span class="text-content-secondary">{{ t('payment.paymentAmount') }}</span>
        <span class="font-semibold text-content-primary">{{ formattedAmount }}</span>
      </div>
      <div class="flex items-center justify-between gap-4">
        <span class="inline-flex items-center gap-1 text-content-secondary">
          {{ t('payment.fee') }}
          <Icon name="infoCircle" size="xs" class="text-content-disabled" aria-hidden="true" />
        </span>
        <span class="font-semibold text-content-primary">{{ formattedFee }}</span>
      </div>
      <div class="border-t border-line-subtle/70 pt-4">
        <div class="flex items-center justify-between gap-4">
          <span class="font-semibold text-content-primary">{{ t('payment.actualPay') }}</span>
          <span class="text-2xl font-semibold tracking-normal text-content-brand">{{ formattedTotal }}</span>
        </div>
      </div>
      <div class="border-t border-line-subtle/70 pt-4">
        <div
          data-testid="estimated-credited-highlight"
          class="recharge-summary-highlight"
        >
          <span class="text-sm font-semibold text-content-secondary">{{ t('payment.rechargeUi.estimatedCreditedAmount') }}</span>
          <span class="recharge-summary-highlight-value">{{ formattedEstimatedCreditedAmount }}</span>
        </div>
        <div class="mt-4 flex items-center justify-between gap-4">
          <span class="text-content-secondary">{{ t('payment.rechargeUi.arrivalTime') }}</span>
          <span class="font-semibold text-status-success">{{ t('payment.rechargeUi.instantArrivalShort') }}</span>
        </div>
      </div>
    </div>

    <div
      v-if="configurationWarning"
      data-testid="recharge-configuration-warning"
      class="mt-5 rounded-2xl border border-status-warning-border bg-status-warning-soft px-4 py-3 text-sm text-status-warning"
      role="alert"
    >
      {{ configurationWarning }}
    </div>

    <div v-if="errorMessage" class="mt-5 rounded-2xl border border-status-danger-border bg-status-danger-soft px-4 py-3 text-sm text-status-danger" role="alert" aria-live="polite">
      <p>{{ errorMessage }}</p>
      <p v-if="errorHintMessage" class="mt-1 text-status-danger/80">{{ errorHintMessage }}</p>
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
        <span class="recharge-submit-spinner h-4 w-4 animate-spin rounded-full border-2 border-white/80 border-t-transparent"></span>
        {{ t('common.processing') }}
      </span>
      <span v-else class="inline-flex items-center justify-center gap-2">
        {{ ctaText }}
        <Icon name="externalLink" size="sm" aria-hidden="true" />
      </span>
    </button>

    <p class="mt-4 flex items-start justify-center gap-2 text-xs text-content-secondary">
      <span
        class="mt-0.5 inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded border border-line-default text-[9px] font-bold text-content-brand"
        aria-hidden="true"
      >
        ✓
      </span>
      <span>
        {{ t('payment.rechargeUi.agreementPrefix') }}
        <span class="font-medium text-content-brand">{{ t('payment.rechargeUi.agreementName') }}</span>
      </span>
    </p>
  </aside>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  formattedAmount: string
  formattedFee: string
  formattedTotal: string
  formattedEstimatedCreditedAmount: string
  disabled: boolean
  submitting: boolean
  hasSubmitted?: boolean
  errorMessage?: string
  errorHintMessage?: string
  configurationWarning?: string
}>(), {
  hasSubmitted: false,
  errorMessage: '',
  errorHintMessage: '',
  configurationWarning: '',
})

const emit = defineEmits<{
  submit: []
}>()

const { t } = useI18n()
const ctaText = computed(() => props.hasSubmitted && props.errorMessage
  ? t('payment.rechargeUi.retryRecharge')
  : t('payment.rechargeUi.rechargeNow')
)
</script>

<style scoped>
@media (prefers-reduced-motion: reduce) {
  .recharge-submit-spinner {
    animation: none;
  }
}
</style>
