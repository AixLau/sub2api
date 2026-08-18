<template>
  <section
    class="recharge-glass-card p-5 sm:p-6"
    :aria-labelledby="showHeader ? 'recharge-amount-title' : undefined"
    :aria-label="showHeader ? undefined : t('payment.rechargeUi.selectAmount')"
  >
    <div v-if="showHeader" class="mb-4 flex items-start justify-between gap-4">
      <div>
        <p id="recharge-amount-title" class="recharge-section-title">1. {{ t('payment.rechargeUi.selectAmount') }}</p>
        <p class="mt-1 text-sm text-content-secondary">{{ t('payment.rechargeUi.amountHint') }}</p>
      </div>
    </div>

    <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <button
        v-for="preset in filteredAmounts"
        :key="preset"
        type="button"
        :data-testid="`quick-amount-${preset}`"
        class="recharge-choice-card relative min-h-[74px] px-4 py-3 text-left"
        :class="{ 'recharge-choice-card-selected': selectedPreset === preset }"
        :aria-pressed="selectedPreset === preset"
        @click="selectPreset(preset)"
      >
        <span class="block text-lg font-semibold text-content-primary">{{ formatPresetAmount(preset) }}</span>
        <span v-if="showPresetMeta" class="mt-1 block text-xs text-content-tertiary">{{ t('payment.rechargeUi.noFee') }}</span>
      </button>

      <label
        class="recharge-choice-card col-span-2 flex min-h-[74px] items-center gap-3 px-4 py-3 sm:col-span-1"
        :class="{
          'recharge-choice-card-selected': isCustomActive,
          'recharge-choice-card-error': isCustomActive && !!error,
        }"
      >
        <span class="shrink-0 text-sm font-semibold text-content-tertiary">{{ currencyPrefix }}</span>
        <span class="min-w-0 flex-1">
          <span class="mb-1 block text-xs font-medium text-content-tertiary">{{ t('payment.customAmount') }}</span>
          <input
            :id="inputId"
            data-testid="custom-recharge-amount"
            type="text"
            inputmode="numeric"
            :value="customText"
            :placeholder="t('payment.rechargeUi.customAmountPlaceholder')"
            :aria-invalid="!!error"
            :aria-describedby="error ? errorId : undefined"
            class="w-full min-w-0 bg-transparent text-sm font-semibold text-content-primary outline-none placeholder:text-content-disabled"
            @input="handleInput"
            @focus="markCustomActive"
          />
        </span>
      </label>
    </div>

    <p
      v-if="error"
      :id="errorId"
      data-testid="amount-error"
      class="mt-3 rounded-xl border border-status-danger-border bg-status-danger-soft px-3 py-2 text-sm text-status-danger"
    >
      {{ error }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { currencySymbol } from '@/components/payment/currency'

const props = withDefaults(defineProps<{
  modelValue: number | null
  amounts?: number[]
  min?: number
  max?: number
  currency?: string
  locale?: string
  error?: string
  inputId?: string
  errorId?: string
  showHeader?: boolean
  showPresetMeta?: boolean
  formatAmount: (value: number) => string
}>(), {
  amounts: () => [10, 20, 50, 100, 200, 500, 1000],
  min: 10,
  max: 500000,
  currency: 'CNY',
  locale: undefined,
  error: '',
  inputId: 'recharge-custom-amount',
  errorId: 'recharge-amount-error',
  showHeader: true,
  showPresetMeta: true,
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
}>()

const { t } = useI18n()
const customText = ref('')
const customFocused = ref(false)

const amountSet = computed(() => new Set(props.amounts))
const filteredAmounts = computed(() =>
  props.amounts.filter(amount => amount >= props.min && amount <= props.max)
)
const currencyPrefix = computed(() => currencySymbol(props.currency))
const isCustomActive = computed(() =>
  customFocused.value || (props.modelValue !== null && !amountSet.value.has(props.modelValue))
)
const selectedPreset = computed(() =>
  !isCustomActive.value && props.modelValue !== null && amountSet.value.has(props.modelValue)
    ? props.modelValue
    : null
)

const AMOUNT_PATTERN = /^\d*$/

function formatPresetAmount(amount: number): string {
  const safeAmount = Number.isFinite(amount) ? Math.trunc(amount) : 0
  try {
    return new Intl.NumberFormat(props.locale || undefined, {
      style: 'currency',
      currency: props.currency,
      currencyDisplay: 'narrowSymbol',
      minimumFractionDigits: 0,
      maximumFractionDigits: 0,
    }).format(safeAmount)
  } catch {
    return `${currencyPrefix.value}${safeAmount.toLocaleString('en-US')}`
  }
}

function selectPreset(amount: number) {
  customFocused.value = false
  customText.value = ''
  emit('update:modelValue', amount)
}

function markCustomActive() {
  customFocused.value = true
}

function handleInput(event: Event) {
  const nextValue = (event.target as HTMLInputElement).value.trim()
  if (!AMOUNT_PATTERN.test(nextValue)) {
    const target = event.target as HTMLInputElement
    target.value = customText.value
    return
  }

  customFocused.value = true
  customText.value = nextValue
  if (!nextValue) {
    emit('update:modelValue', null)
    return
  }

  const amount = Number(nextValue)
  emit('update:modelValue', Number.isFinite(amount) && amount > 0 ? amount : null)
}

watch(() => props.modelValue, (value) => {
  if (value === null) {
    if (!customFocused.value) customText.value = ''
    return
  }
  if (amountSet.value.has(value) && !customFocused.value) {
    customText.value = ''
    return
  }
  if (!amountSet.value.has(value)) {
    customText.value = String(value)
  }
}, { immediate: true })
</script>
