<template>
  <div data-testid="subscription-layout" class="space-y-4">
    <CurrentSubscriptionCard
      v-if="currentSubscription"
      :subscription="currentSubscription"
    />

    <div class="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
      <div data-testid="subscription-controls" class="min-w-0 space-y-3">
        <section class="recharge-glass-card p-4 sm:p-5" aria-labelledby="subscription-plans-title">
          <div class="mb-3">
            <h2 id="subscription-plans-title" class="recharge-section-title">
              {{ t('payment.selectPlan') }}
            </h2>
          </div>

          <div
            v-if="options.length"
            data-testid="subscription-plan-list"
            role="radiogroup"
            :aria-label="t('payment.selectPlan')"
            :class="planGridClass"
          >
            <template v-for="option in options" :key="option.key">
              <SubscriptionPlanCard
                v-if="option.source === 'internal'"
                :plan="option.plan"
                :selected="option.key === selectedOptionKey"
                :renewal-target="renewalGroupId != null && option.groupId === renewalGroupId"
                @select="selectOption(option)"
              />

              <div
                v-else
                :data-testid="`nineplus-subscription-product-${option.product.product_id}`"
                role="radio"
                :aria-checked="option.key === selectedOptionKey"
                :aria-label="option.title"
                tabindex="0"
                :class="[
                  'recharge-choice-card subscription-liquid-plan-card group relative flex !min-h-[11rem] cursor-pointer flex-col p-3 transition-all hover:-translate-y-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 focus-visible:ring-offset-2',
                  option.key === selectedOptionKey && 'recharge-choice-card-selected subscription-liquid-plan-card-selected',
                  option.originalPrice && option.key !== selectedOptionKey && '!border-lime-400/90',
                ]"
                @click="selectOption(option)"
                @keydown.enter.prevent="selectOption(option)"
                @keydown.space.prevent="selectOption(option)"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0 flex-1">
                    <h3 :title="option.title" class="h-12 break-words text-base font-black leading-6 text-content-primary [overflow-wrap:anywhere] line-clamp-2">
                      {{ option.title }}
                    </h3>
                    <span
                      v-if="option.product.badge"
                      class="mt-1 inline-flex max-w-full rounded-full border border-violet-200 bg-violet-50 px-2 py-0.5 text-[10px] font-bold text-violet-700 dark:border-violet-800 dark:bg-violet-950/40 dark:text-violet-200"
                    >
                      {{ option.product.badge }}
                    </span>
                  </div>
                  <span
                    v-if="option.originalPrice"
                    class="shrink-0 rounded-full border border-lime-400 bg-lime-200 px-2 py-0.5 text-[10px] font-black text-slate-950"
                  >
                    -{{ purchaseSubscriptionDiscountPercent(option) }}%
                  </span>
                </div>

                <div class="min-h-[2.5rem]">
                  <p v-if="option.description" class="mt-1.5 line-clamp-2 text-xs leading-5 text-content-secondary">
                    {{ option.description }}
                  </p>
                </div>

                <div class="mt-3">
                  <p data-testid="nineplus-subscription-price" class="tabular-nums text-2xl font-black tracking-tight text-blue-700 dark:text-blue-300">
                    {{ formatOptionAmount(option.price, option.currency) }}
                  </p>
                  <span v-if="option.originalPrice" class="mt-0.5 block tabular-nums text-xs text-content-tertiary line-through">
                    {{ formatOptionAmount(option.originalPrice, option.currency) }}
                  </span>
                </div>

                <div v-if="option.product.delivery_note" class="mt-3 flex items-start gap-2 text-xs leading-5 text-content-secondary">
                  <svg class="mt-0.5 h-3.5 w-3.5 shrink-0 text-lime-600 dark:text-lime-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
                  </svg>
                  <span>{{ option.product.delivery_note }}</span>
                </div>

                <div class="flex-1" />
                <div class="mt-3 flex items-end justify-between gap-3 border-t border-line-subtle pt-3">
                  <span v-if="formatNinePlusQuota(option.product)" class="truncate text-sm font-black text-content-primary">
                    {{ formatNinePlusQuota(option.product) }}
                  </span>
                  <span v-else class="text-xs font-semibold text-content-tertiary">{{ option.product.currency }}</span>
                  <span
                    data-testid="subscription-plan-radio"
                    :class="[
                      'flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-2 transition-colors',
                      option.key === selectedOptionKey
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
            </template>
          </div>

          <div v-else data-testid="subscription-empty-state" class="rounded-2xl border border-dashed border-line-strong bg-surface-subtle px-6 py-10 text-center">
            <p class="text-base font-bold text-content-primary">{{ t('payment.noPlans') }}</p>
          </div>
        </section>

        <RechargeMethodSelector
          v-if="methods.length"
          :methods="methods"
          :selected="selectedMethod"
          @select="emit('select-method', $event)"
        />
      </div>

      <div data-testid="subscription-confirmation" class="min-w-0">
        <SubscriptionOrderSummary
          :option="selectedOption"
          :active-subscriptions="activeSubscriptions"
          :payment-amount="paymentAmount"
          :fee-amount="feeAmount"
          :total-amount="totalAmount"
          :original-amount="originalAmount"
          :discount-amount="discountAmount"
          :payment-currency="paymentCurrency"
          :can-submit="canSubmit"
          :submitting="submitting"
          :error-message="errorMessage"
          @submit="emit('submit', $event)"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserSubscription } from '@/types'
import type { NinePlusProduct, SubscriptionPlan } from '@/types/payment'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import SubscriptionPlanCard from '@/components/payment/SubscriptionPlanCard.vue'
import CurrentSubscriptionCard from '@/components/payment/recharge/CurrentSubscriptionCard.vue'
import RechargeMethodSelector from '@/components/payment/recharge/RechargeMethodSelector.vue'
import SubscriptionOrderSummary from './SubscriptionOrderSummary.vue'
import {
  buildPurchaseSubscriptionOptions,
  findPurchaseSubscriptionOption,
  formatNinePlusQuota,
  purchaseSubscriptionDiscountPercent,
  type CurrentSubscriptionSummary,
  type PurchaseSubscriptionOption,
} from './purchaseViewModels'

const props = withDefaults(defineProps<{
  plans: SubscriptionPlan[]
  ninePlusProducts?: NinePlusProduct[]
  activeSubscriptions?: UserSubscription[]
  currentSubscription?: CurrentSubscriptionSummary | null
  methods?: PaymentMethodOption[]
  selectedMethod?: string
  selectedOptionKey?: string
  renewalGroupId?: number | null
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
  ninePlusProducts: () => [],
  activeSubscriptions: () => [],
  currentSubscription: null,
  methods: () => [],
  selectedMethod: '',
  selectedOptionKey: '',
  renewalGroupId: null,
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
  'select-option': [option: PurchaseSubscriptionOption]
  'select-method': [type: string]
  submit: [option: PurchaseSubscriptionOption]
}>()

const { t, locale } = useI18n()
const options = computed(() => buildPurchaseSubscriptionOptions(props.plans, props.ninePlusProducts))
const selectedOption = computed(() =>
  findPurchaseSubscriptionOption(options.value, props.selectedOptionKey)
)

const planGridClass = computed(() => {
  if (options.value.length === 1) return 'mx-auto grid max-w-xl grid-cols-1 gap-3'
  if (options.value.length === 2) return 'grid grid-cols-1 gap-3 sm:grid-cols-2'
  return 'grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4'
})

function formatOptionAmount(amount: number, currency: string): string {
  return formatPaymentAmount(amount, currency, locale.value)
}

function selectOption(option: PurchaseSubscriptionOption) {
  emit('select-option', option)
}
</script>
