<template>
  <div
    :data-testid="`subscription-plan-${plan.id}`"
    role="radio"
    :aria-checked="selected"
    :aria-label="plan.name"
    tabindex="0"
    :data-highlighted="showStaticHighlight ? 'true' : undefined"
    :class="[
      'subscription-liquid-plan-card group relative flex !min-h-[11rem] cursor-pointer flex-col transition-all',
      'hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 focus-visible:ring-offset-2',
      selected && 'subscription-liquid-plan-card-selected',
      showStaticHighlight && !selected && '!border-lime-400/90',
      renewalTarget && !selected && '!border-amber-400/90',
    ]"
    @click="selectPlan"
    @keydown.enter.prevent="selectPlan"
    @keydown.space.prevent="selectPlan"
  >
    <div class="flex flex-1 flex-col p-3">
      <span
        v-if="renewalTarget"
        data-testid="subscription-renewal-target"
        class="mb-1 w-fit rounded-full border border-amber-300 bg-amber-50 px-2 py-0.5 text-[10px] font-bold text-amber-800 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-200"
      >
        {{ t('payment.subscriptionUi.renewalTarget') }}
      </span>
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0 flex-1">
          <h3
            :title="plan.name"
            class="h-10 min-w-0 break-words [overflow-wrap:anywhere] text-base font-black leading-5 text-content-primary line-clamp-2"
          >{{ plan.name }}</h3>
        </div>
        <span
          v-if="hasDiscount"
          data-testid="subscription-plan-discount"
          class="shrink-0 rounded-full border border-lime-400 bg-lime-200 px-2 py-0.5 text-[10px] font-black text-slate-950"
        >
          {{ discountText }}
        </span>
      </div>

      <div class="min-h-[2rem]">
        <p
          v-if="planDisplay.description"
          data-testid="subscription-plan-description"
          class="mt-1 line-clamp-2 text-xs leading-4 text-content-secondary"
        >
          {{ planDisplay.description }}
        </p>
      </div>

      <div class="mt-2">
        <div class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <span data-testid="subscription-plan-price" class="tabular-nums text-2xl font-black tracking-tight text-blue-700 dark:text-blue-300">
            {{ formattedPrice }}
          </span>
          <template v-if="validitySummary">
            <span class="text-xs font-black text-blue-600 dark:text-blue-300" aria-hidden="true">/</span>
            <span
              data-testid="subscription-plan-validity-summary"
              class="text-xs font-bold text-content-tertiary"
            >
              {{ validitySummary }}
            </span>
          </template>
        </div>
        <span v-if="hasDiscount" class="mt-0.5 block tabular-nums text-xs text-content-tertiary line-through">
          {{ formattedOriginalPrice }}
        </span>
      </div>

      <div class="flex-1" />

      <div class="mt-2 flex items-end justify-between gap-3 border-t border-line-subtle pt-2">
        <div class="min-w-0">
          <p
            v-if="planDisplay.quotaSummary"
            data-testid="subscription-plan-quota-summary"
            class="truncate text-sm font-black text-content-primary"
          >
            {{ planDisplay.quotaSummary }}
          </p>
          <p v-else class="text-xs font-semibold text-content-tertiary">{{ t('payment.planFeatures') }}</p>
        </div>
        <span
          data-testid="subscription-plan-radio"
          :class="[
            'flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-2 transition-colors',
            selected
              ? 'border-blue-600 bg-blue-600 text-white'
              : 'border-slate-300 bg-white text-transparent dark:border-slate-500 dark:bg-slate-800',
          ]"
          aria-hidden="true"
        >
          <svg class="h-3 w-3" viewBox="0 0 12 12" fill="none">
            <path d="m2.25 6.25 2.25 2.25 5.25-5.25" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/types/payment'
import { formatPaymentAmount } from '@/components/payment/currency'
import { planValiditySuffix } from './validity'
import { buildSubscriptionPlanDisplay, buildSubscriptionPlanDisplayLabels } from '@/components/payment/subscriptionPlanDisplay'

const props = withDefaults(defineProps<{
  plan: SubscriptionPlan
  /** Force the static highlighted outline on/off; defaults to on for discounted plans. */
  highlight?: boolean
  selected?: boolean
  renewalTarget?: boolean
}>(), {
  highlight: undefined,
  selected: false,
  renewalTarget: false,
})
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()
const hasDiscount = computed(() =>
  Number.isFinite(Number(props.plan.original_price))
  && (props.plan.original_price ?? 0) > props.plan.price
)

const showStaticHighlight = computed(() => {
  if (props.highlight !== undefined) return props.highlight
  return hasDiscount.value
})

const planDisplay = computed(() => buildSubscriptionPlanDisplay(props.plan, buildSubscriptionPlanDisplayLabels(t)))

const formattedPrice = computed(() => formatPaymentAmount(props.plan.price, props.plan.currency))
const formattedOriginalPrice = computed(() =>
  hasDiscount.value ? formatPaymentAmount(props.plan.original_price ?? 0, props.plan.currency) : ''
)

const discountText = computed(() => {
  if (!hasDiscount.value || !props.plan.original_price) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const validitySummary = computed(() => {
  const unit = String(props.plan.validity_unit || 'day').trim().toLowerCase()
  if (unit === 'day') return planDisplay.value.validitySummary
  return planValiditySuffix(props.plan, t)
})

function selectPlan() {
  emit('select', props.plan)
}
</script>
