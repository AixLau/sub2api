<template>
  <section
    data-testid="recharge-amount-selector"
    class="recharge-selector recharge-amount-card"
    :class="{ 'is-disabled': disabled }"
    :aria-labelledby="showHeader ? 'recharge-amount-title' : undefined"
    :aria-label="showHeader ? undefined : t('payment.rechargeUi.selectAmount')"
  >
    <header v-if="showHeader" class="recharge-selector__header">
      <h2 id="recharge-amount-title" class="recharge-selector__title">
        {{ t('payment.rechargeUi.selectAmount') }}
      </h2>
      <Icon name="bolt" size="sm" class="recharge-selector__header-icon" aria-hidden="true" />
    </header>

    <div
      class="recharge-selector__grid"
      role="group"
      :aria-label="t('payment.rechargeUi.selectAmount')"
    >
      <button
        v-for="preset in filteredAmounts"
        :key="preset"
        type="button"
        :data-testid="`quick-amount-${preset}`"
        class="recharge-amount-option"
        :class="{
          'is-selected': selectedPreset === preset,
          'is-recommended': recommendedAmount === preset,
        }"
        :aria-pressed="selectedPreset === preset"
        :aria-label="formatAmount(preset)"
        :disabled="disabled"
        @click="selectPreset(preset)"
      >
        <svg
          v-if="selectedPreset === preset"
          data-testid="selected-amount-soap"
          class="recharge-amount-option__soap-bg"
          viewBox="0 0 300 150"
          preserveAspectRatio="none"
          aria-hidden="true"
          focusable="false"
        >
          <defs>
            <linearGradient :id="`soap-gradient-${preset}`" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0%" stop-color="#d45bf5" />
              <stop offset="30%" stop-color="#923cf8" />
              <stop offset="65%" stop-color="#6238fa" />
              <stop offset="100%" stop-color="#4654ed" />
            </linearGradient>
            <filter :id="`soap-shadow-${preset}`" x="-15%" y="-20%" width="130%" height="150%">
              <feDropShadow dx="0" dy="6" stdDeviation="6" flood-color="#5748e5" flood-opacity=".35" />
            </filter>
          </defs>
          <path
            class="recharge-amount-option__soap-shape"
            d="M 34 8 C 92 15, 208 15, 266 8 C 282 6, 292 17, 292 34 C 285 60, 285 90, 292 116 C 292 133, 282 144, 266 142 C 208 135, 92 135, 34 142 C 18 144, 8 133, 8 116 C 15 90, 15 60, 8 34 C 8 17, 18 6, 34 8 Z"
            :fill="`url(#soap-gradient-${preset})`"
            :filter="`url(#soap-shadow-${preset})`"
          />
          <path
            class="recharge-amount-option__soap-highlight"
            d="M 35 11 C 94 18, 206 18, 265 11 C 278 9, 288 18, 288 35 C 282 61, 282 89, 288 115 C 288 129, 279 139, 264 138 C 206 131, 94 131, 36 138 C 21 139, 12 129, 12 115 C 18 89, 18 61, 12 35 C 12 19, 21 9, 35 11 Z"
            fill="none"
            stroke="rgba(255,255,255,.28)"
            stroke-width="2"
          />
          <path
            class="recharge-amount-option__soap-lightning"
            d="M 244 42 H 294 L 274 72 H 296 L 232 142 L 247 96 H 219 Z"
          />
        </svg>
        <span v-if="recommendedAmount === preset" class="recharge-amount-option__badge">
          {{ t('payment.rechargeUi.recommended') }}
        </span>
        <span class="recharge-amount-option__amount">
          {{ formatPresetAmount(preset) }}
        </span>
        <span
          v-if="formatCreditedAmount"
          class="recharge-amount-option__credited"
        >
          <span class="recharge-amount-option__approx">≈</span>
          <span>{{ formatCreditedAmount(preset) }}</span>
        </span>
        <span v-if="showPresetMeta" class="recharge-amount-option__meta">
          {{ t('payment.rechargeUi.noFee') }}
        </span>
      </button>

      <div
        class="recharge-custom-card"
        :class="{
          'is-active': isCustomActive,
          'has-error': isCustomActive && !!displayError,
        }"
      >
        <button
          v-if="!isCustomActive"
          data-testid="custom-recharge-trigger"
          type="button"
          class="recharge-custom-card__trigger"
          :disabled="disabled"
          @click="activateCustomAmount"
        >
          <span class="recharge-custom-card__content">
            <strong>{{ t('payment.customAmount') }}</strong>
            <span>{{ t('payment.rechargeUi.customAmountPlaceholder') }}</span>
          </span>
          <Icon name="edit" size="md" class="recharge-custom-card__icon" aria-hidden="true" />
        </button>

        <div v-else class="recharge-custom-card__input-wrap">
          <span class="recharge-custom-card__currency">{{ currencyPrefix }}</span>
          <input
            ref="customInputRef"
            :id="inputId"
            data-testid="custom-recharge-amount"
            type="text"
            inputmode="decimal"
            autocomplete="off"
            :value="customText"
            :placeholder="t('payment.rechargeUi.customAmountPlaceholder')"
            :aria-label="t('payment.customAmount')"
            :aria-invalid="!!displayError"
            :aria-describedby="displayError ? errorId : undefined"
            :disabled="disabled"
            @input="handleInput"
            @keydown.enter.prevent="confirmCustomAmount"
            @blur="confirmCustomAmount"
          />
        </div>

        <span
          data-testid="custom-amount-effects"
          class="recharge-custom-card__effects"
          aria-hidden="true"
        >
          <span class="recharge-custom-card__dot-field" />
          <span class="recharge-custom-card__glow" />
          <span class="recharge-custom-card__star recharge-custom-card__star--blue" />
          <span class="recharge-custom-card__star recharge-custom-card__star--pink" />
        </span>
      </div>
    </div>

    <p
      v-if="displayError"
      :id="errorId"
      data-testid="amount-error"
      class="recharge-selector__error"
    >
      {{ displayError }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { currencySymbol } from '@/components/payment/currency'
import Icon from '@/components/icons/Icon.vue'

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
  recommendedAmount?: number | null
  disabled?: boolean
  maxFractionDigits?: number
  formatAmount: (value: number) => string
  formatCreditedAmount?: (value: number) => string
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
  recommendedAmount: 100,
  disabled: false,
  maxFractionDigits: 2,
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  change: [value: number | null]
}>()

const { t } = useI18n()
const customText = ref('')
const customActive = ref(false)
const customInputRef = ref<HTMLInputElement | null>(null)
const localError = ref('')

const amountSet = computed(() => new Set(props.amounts))
const filteredAmounts = computed(() =>
  props.amounts.filter(amount => amount >= props.min && amount <= props.max)
)
const currencyPrefix = computed(() => currencySymbol(props.currency))
const isCustomActive = computed(() => customActive.value)
const customAmountBelowMinimum = computed(() => {
  if (!customActive.value || !customText.value) return false
  const amount = Number(customText.value)
  return Number.isFinite(amount) && amount < props.min
})
const displayError = computed(() =>
  customAmountBelowMinimum.value ? '' : props.error || localError.value
)
const allowedFractionDigits = computed(() =>
  Math.max(0, Math.min(2, Math.trunc(props.maxFractionDigits)))
)
const selectedPreset = computed(() =>
  !customActive.value && props.modelValue !== null && amountSet.value.has(props.modelValue)
    ? props.modelValue
    : null
)

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

function emitAmount(value: number | null): void {
  emit('update:modelValue', value)
  emit('change', value)
}

function selectPreset(amount: number): void {
  if (props.disabled) return
  customActive.value = false
  customText.value = ''
  localError.value = ''
  emitAmount(amount)
}

async function activateCustomAmount(): Promise<void> {
  if (props.disabled) return
  customActive.value = true
  customText.value = ''
  localError.value = ''
  emitAmount(null)
  await nextTick()
  customInputRef.value?.focus()
}

function sanitizeAmountInput(value: string): string {
  if (/[eE+-]/.test(value)) return customText.value

  const normalized = value
    .replace(/[^\d.]/g, '')
    .replace(/(\..*)\./g, '$1')
  const [rawInteger = '', rawDecimal] = normalized.split('.')
  const integer = rawInteger.replace(/^0+(?=\d)/, '')

  if (rawDecimal === undefined || allowedFractionDigits.value === 0) {
    return integer
  }
  return `${integer || '0'}.${rawDecimal.slice(0, allowedFractionDigits.value)}`
}

function handleInput(event: Event): void {
  const target = event.target as HTMLInputElement
  const nextValue = sanitizeAmountInput(target.value.trim())
  target.value = nextValue
  customActive.value = true
  customText.value = nextValue
  localError.value = ''

  if (!nextValue) {
    emitAmount(null)
    return
  }

  const amount = Number(nextValue)
  emitAmount(Number.isFinite(amount) && amount > 0 ? amount : null)
}

function confirmCustomAmount(): void {
  if (!customText.value) return
  const amount = Number(customText.value)
  if (!Number.isFinite(amount)) {
    localError.value = t('payment.rechargeUi.customAmountPositive')
    emitAmount(null)
    return
  }
  const normalizedAmount = Math.max(props.min, amount)
  customText.value = String(normalizedAmount)
  localError.value = ''
  emitAmount(normalizedAmount)
}

watch(() => props.modelValue, (value) => {
  if (value === null) {
    if (!customActive.value) customText.value = ''
    return
  }
  if (amountSet.value.has(value) && !customActive.value) {
    customText.value = ''
    return
  }
  if (!amountSet.value.has(value)) {
    customActive.value = true
    customText.value = String(value)
  }
}, { immediate: true })
</script>

<style scoped>
.recharge-selector {
  position: relative;
  min-width: 0;
  padding: 1.5rem 1.5rem 1.625rem;
  overflow: hidden;
  border: 1px solid rgb(99 102 241 / 14%);
  border-radius: 28px;
  background: rgb(255 255 255 / 94%);
  box-shadow:
    0 24px 70px rgb(41 44 86 / 10%),
    inset 0 1px 0 rgb(255 255 255 / 85%);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
}

.recharge-selector::after {
  position: absolute;
  right: -42px;
  bottom: -40px;
  width: 240px;
  height: 240px;
  background-image: radial-gradient(circle, rgb(59 130 246 / 65%) 2px, transparent 2.5px);
  background-size: 14px 14px;
  content: '';
  mask-image: radial-gradient(circle, #000 10%, transparent 72%);
  opacity: 0.22;
  pointer-events: none;
}

.recharge-selector__header,
.recharge-selector__grid,
.recharge-selector__error {
  position: relative;
  z-index: 1;
}

.recharge-selector__header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1.25rem;
}

.recharge-selector__title {
  margin: 0;
  color: #111218;
  font-family: "PingFang SC", "Microsoft YaHei", ui-sans-serif, system-ui, sans-serif;
  font-size: clamp(1.05rem, 1.5vw, 1.35rem);
  font-weight: 850;
  letter-spacing: -0.025em;
  line-height: 1.25;
}

.recharge-selector__header-icon {
  color: #111827;
}

.recharge-selector__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
}

.recharge-amount-option,
.recharge-custom-card {
  position: relative;
  min-width: 0;
  min-height: 116px;
  overflow: hidden;
  border: 1px solid transparent;
  border-radius: 18px;
  background:
    linear-gradient(#fff, #fff) padding-box,
    linear-gradient(135deg, rgb(168 85 247 / 30%), rgb(59 130 246 / 20%)) border-box;
  box-shadow:
    0 12px 26px rgb(59 63 112 / 8%),
    inset 0 1px 0 rgb(255 255 255 / 90%);
}

.recharge-amount-option {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding: 1.4rem 1.125rem;
  color: #111218;
  cursor: pointer;
  transition:
    transform 180ms ease,
    box-shadow 180ms ease,
    border-color 180ms ease;
}

.recharge-amount-option > * {
  position: relative;
  z-index: 1;
}

.recharge-amount-option__amount {
  font-family: "Arial Black", Inter, "PingFang SC", ui-sans-serif, system-ui, sans-serif;
  font-size: clamp(2rem, 2.4vw, 2.75rem);
  font-weight: 800;
  letter-spacing: -0.04em;
  line-height: 1;
  white-space: nowrap;
}

.recharge-amount-option__credited {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  color: #737786;
  font-size: clamp(0.9rem, 1.2vw, 1.125rem);
  font-weight: 500;
  white-space: nowrap;
}

.recharge-amount-option__approx {
  color: #22a447;
  font-weight: 800;
}

.recharge-amount-option__meta {
  color: #8b8fa0;
  font-size: 0.72rem;
}

.recharge-amount-option__badge {
  position: absolute;
  z-index: 3;
  top: 0;
  right: 0;
  padding: 0.5rem 1.125rem 0.56rem;
  border-bottom-left-radius: 18px;
  background: rgb(124 58 237 / 11%);
  color: #6d48d7;
  font-size: 0.82rem;
  font-weight: 750;
  line-height: 1;
  backdrop-filter: blur(8px);
}

.recharge-amount-option.is-selected {
  isolation: isolate;
  overflow: visible;
  border-color: transparent;
  background: transparent;
  color: #fff;
  box-shadow: none;
}

.recharge-amount-option__soap-bg {
  position: absolute;
  z-index: 0;
  inset: 0;
  width: 100%;
  height: 100%;
  overflow: visible;
  pointer-events: none;
}

.recharge-amount-option__soap-lightning {
  fill: rgb(155 143 255 / 24%);
}

.recharge-amount-option.is-selected .recharge-amount-option__credited {
  color: rgb(255 255 255 / 86%);
  text-shadow: 0 1px 3px rgb(255 255 255 / 20%);
}

.recharge-amount-option.is-selected .recharge-amount-option__amount {
  text-shadow:
    0 1px 3px rgb(255 255 255 / 25%),
    0 2px 5px rgb(64 34 200 / 18%);
}

.recharge-amount-option.is-selected .recharge-amount-option__approx {
  color: #d8ff68;
}

.recharge-amount-option.is-selected .recharge-amount-option__badge {
  top: 5%;
  right: 2.2%;
  background: linear-gradient(135deg, rgb(202 128 255 / 92%), rgb(142 83 255 / 82%));
  color: #fff;
  font-style: italic;
  letter-spacing: 0.06em;
  text-shadow:
    0 0 3px rgb(255 255 255 / 65%),
    0 1px 3px rgb(104 55 222 / 35%);
  box-shadow: inset 0 1px 2px rgb(255 255 255 / 25%);
}

.recharge-amount-option:focus-visible,
.recharge-custom-card__trigger:focus-visible {
  outline: 3px solid rgb(99 102 241 / 28%);
  outline-offset: 3px;
}

.recharge-custom-card__input-wrap,
.recharge-custom-card input,
.recharge-custom-card input:focus,
.recharge-custom-card input:focus-visible {
  border: 0;
  outline: 0;
  background: transparent;
  box-shadow: none;
  appearance: none;
  -webkit-appearance: none;
}

.recharge-amount-option:disabled,
.recharge-custom-card__trigger:disabled,
.recharge-custom-card input:disabled {
  cursor: not-allowed;
  opacity: 0.52;
}

.recharge-custom-card.is-active {
  border-color: rgb(124 58 237 / 62%);
  box-shadow:
    0 0 0 3px rgb(124 58 237 / 10%),
    0 16px 32px rgb(91 72 190 / 12%);
}

.recharge-custom-card.has-error {
  border-color: rgb(var(--color-status-danger-border));
  box-shadow: 0 0 0 3px rgb(var(--color-status-danger) / 10%);
}

.recharge-custom-card__trigger,
.recharge-custom-card__input-wrap {
  position: relative;
  z-index: 2;
  display: flex;
  width: 100%;
  height: 100%;
  min-height: inherit;
  align-items: center;
}

.recharge-custom-card__trigger {
  justify-content: space-between;
  gap: 0.4rem;
  border: 0;
  background: transparent;
  padding: 1rem 0.8rem;
  color: #111218;
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.recharge-custom-card__content {
  display: grid;
  min-width: 0;
  gap: 0.55rem;
}

.recharge-custom-card__content strong {
  font-size: 0.88rem;
  font-weight: 750;
  white-space: nowrap;
}

.recharge-custom-card__content span {
  color: #8a8e9e;
  font-size: 0.78rem;
  white-space: nowrap;
}

.recharge-custom-card__icon {
  width: 1.25rem;
  height: 1.25rem;
  flex: 0 0 auto;
  color: #111827;
}

.recharge-custom-card__effects {
  position: absolute;
  z-index: 1;
  top: 0;
  right: 0;
  display: block;
  width: 48%;
  height: 100%;
  overflow: hidden;
  opacity: 0.9;
  pointer-events: none;
}

.recharge-custom-card__dot-field {
  position: absolute;
  top: -28%;
  right: -14%;
  width: 112%;
  height: 148%;
  background-image: radial-gradient(circle, rgb(37 99 235 / 70%) 1.35px, transparent 1.9px);
  background-size: 8px 8px;
  clip-path: polygon(43% 0, 100% 0, 100% 100%, 0 100%, 25% 62%, 8% 42%);
  mask-image: linear-gradient(105deg, transparent 3%, #000 46%, #000 100%);
  transform: rotate(-8deg);
}

.recharge-custom-card__glow {
  position: absolute;
  right: -25%;
  bottom: -48%;
  width: 130%;
  aspect-ratio: 1;
  border-radius: 50%;
  background: radial-gradient(circle, rgb(59 130 246 / 23%) 0, rgb(168 85 247 / 12%) 38%, transparent 68%);
}

.recharge-custom-card__star {
  position: absolute;
  display: block;
  width: 21px;
  height: 21px;
  clip-path: polygon(50% 0, 59% 40%, 100% 50%, 59% 60%, 50% 100%, 41% 60%, 0 50%, 41% 40%);
  filter: drop-shadow(0 0 5px currentColor);
}

.recharge-custom-card__star--blue {
  top: 12%;
  right: 12%;
  background: #3b82f6;
  color: rgb(59 130 246 / 45%);
  transform: rotate(12deg);
}

.recharge-custom-card__star--pink {
  right: 38%;
  bottom: 10%;
  width: 14px;
  height: 14px;
  background: #ec4899;
  color: rgb(236 72 153 / 42%);
  transform: rotate(27deg);
}

.recharge-custom-card.is-active .recharge-custom-card__effects {
  opacity: 0.55;
}

.recharge-custom-card.has-error .recharge-custom-card__effects {
  opacity: 0.22;
}

.recharge-custom-card__input-wrap {
  gap: 0.75rem;
  padding: 1.25rem 1.4rem;
}

.recharge-custom-card__currency {
  flex: 0 0 auto;
  color: #6f7380;
  font-size: 1.15rem;
  font-weight: 750;
}

.recharge-custom-card input {
  width: 100%;
  min-width: 0;
  border: 0;
  background: transparent;
  color: #111218;
  font-size: 1.35rem;
  font-weight: 750;
  outline: 0;
}

.recharge-custom-card input::placeholder {
  color: #a2a6b3;
}

.recharge-selector__error {
  margin: 0.9rem 0 0;
  border: 1px solid rgb(var(--color-status-danger-border));
  border-radius: 12px;
  background: rgb(var(--color-status-danger-soft));
  padding: 0.65rem 0.8rem;
  color: rgb(var(--color-status-danger));
  font-size: 0.86rem;
}

:global(.dark) .recharge-selector {
  border-color: rgb(167 139 250 / 22%);
  background: rgb(var(--color-surface-panel) / 94%);
  box-shadow:
    0 24px 70px rgb(0 0 0 / 24%),
    inset 0 1px 0 rgb(255 255 255 / 7%);
}

:global(.dark) .recharge-selector__title,
:global(.dark) .recharge-selector__header-icon,
:global(.dark) .recharge-custom-card__trigger,
:global(.dark) .recharge-custom-card__icon,
:global(.dark) .recharge-custom-card input {
  color: rgb(var(--color-content-primary));
}

:global(.dark) .recharge-amount-option,
:global(.dark) .recharge-custom-card {
  background:
    linear-gradient(rgb(var(--color-surface-raised)), rgb(var(--color-surface-raised))) padding-box,
    linear-gradient(135deg, rgb(168 85 247 / 36%), rgb(59 130 246 / 26%)) border-box;
  color: rgb(var(--color-content-primary));
}

:global(.dark) .recharge-amount-option.is-selected {
  background: transparent;
  color: #fff;
}

@media (hover: hover) and (pointer: fine) {
  .recharge-amount-option:hover:not(:disabled),
  .recharge-custom-card__trigger:hover:not(:disabled) {
    transform: translateY(-3px);
    box-shadow:
      0 18px 34px rgb(91 72 190 / 14%),
      inset 0 1px 0 rgb(255 255 255 / 90%);
  }

  .recharge-amount-option:active:not(:disabled),
  .recharge-custom-card__trigger:active:not(:disabled) {
    transform: translateY(-1px) scale(0.99);
  }
}

@media (min-width: 640px) {
  .recharge-selector__grid {
    gap: 1rem 1.125rem;
  }
}

@media (min-width: 640px) and (max-width: 1099px) {
  .recharge-selector {
    padding: 0.95rem 1rem 1.05rem;
    border-radius: 20px;
  }

  .recharge-selector__header {
    margin-bottom: 0.7rem;
  }

  .recharge-selector__grid {
    gap: 0.65rem;
  }

  .recharge-amount-option,
  .recharge-custom-card {
    min-height: 82px;
    border-radius: 16px;
  }

  .recharge-amount-option {
    gap: 0.45rem;
    padding: 0.75rem;
  }

  .recharge-amount-option__amount {
    font-size: 1.65rem;
  }

  .recharge-amount-option__credited {
    font-size: 0.82rem;
  }

  .recharge-custom-card__trigger {
    padding: 0.75rem;
  }
}

@media (min-width: 1100px) {
  .recharge-selector {
    padding: 0.675rem 0.75rem 0.75rem;
    border-radius: 16px;
  }

  .recharge-selector__header {
    gap: 0.42rem;
    margin-bottom: 0.48rem;
  }

  .recharge-selector__title {
    font-size: 0.85rem;
  }

  .recharge-selector__grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 0.47rem 0.56rem;
  }

  .recharge-amount-option,
  .recharge-custom-card {
    min-height: 0;
    height: clamp(75px, 6vw, 86px);
    aspect-ratio: auto;
    border-radius: clamp(14px, 1.05vw, 16px);
  }

  .recharge-amount-option {
    gap: 0.54rem;
    padding: 0.75rem 0.525rem;
  }

  .recharge-amount-option__amount {
    font-size: clamp(1.3rem, 1.4vw, 1.6rem);
    font-weight: 700;
    letter-spacing: 0.01em;
  }

  .recharge-amount-option__credited {
    gap: 0.32rem;
    font-size: clamp(0.68rem, 0.72vw, 0.8rem);
    font-weight: 600;
  }

  .recharge-amount-option__badge {
    display: flex;
    width: clamp(2.75rem, 26%, 3.3rem);
    height: clamp(1.45rem, 27%, 1.75rem);
    align-items: center;
    justify-content: center;
    padding: 0;
    border-radius: 12px 14px 2px 12px;
    font-size: clamp(0.66rem, 0.7vw, 0.75rem);
    font-weight: 700;
  }

  .recharge-custom-card__trigger {
    padding: 0.54rem 0.525rem;
  }

  .recharge-custom-card__content {
    gap: 0.26rem;
  }

  .recharge-custom-card__content strong {
    font-size: 0.72rem;
  }

  .recharge-custom-card__content span {
    font-size: 0.64rem;
  }

  .recharge-custom-card__input-wrap {
    gap: 0.38rem;
    padding: 0.6rem 0.56rem;
  }

  .recharge-custom-card input {
    font-size: 0.88rem;
  }
}

@media (max-width: 639px) {
  .recharge-selector {
    padding: 1.25rem 1rem 1.375rem;
    border-radius: 22px;
  }

  .recharge-selector__header {
    margin-bottom: 1rem;
  }
}

@media (max-width: 389px) {
  .recharge-selector__grid {
    grid-template-columns: 1fr;
  }
}

@media (prefers-reduced-motion: reduce) {
  .recharge-amount-option,
  .recharge-custom-card__trigger {
    transition: none;
  }
}

@media (forced-colors: active) {
  .recharge-amount-option.is-selected,
  .recharge-custom-card.is-active {
    border: 2px solid Highlight;
  }
}
</style>
