<template>
  <section
    data-testid="recharge-layout"
    class="grid min-w-0 items-start gap-5 lg:grid-cols-[minmax(0,1fr)_320px] xl:grid-cols-[minmax(0,1fr)_340px]"
    :aria-label="t('payment.tabTopUp')"
  >
    <div data-testid="recharge-controls" class="min-w-0 space-y-5">
      <section class="recharge-glass-card p-5 sm:p-6" aria-labelledby="nineplus-recharge-products-title">
        <div class="mb-5">
          <h2 id="nineplus-recharge-products-title" class="recharge-section-title">
            {{ t('payment.nineplus.selectProduct') }}
          </h2>
        </div>

        <div
          v-if="products.length"
          class="grid grid-cols-1 gap-3 sm:grid-cols-2"
          role="radiogroup"
          :aria-label="t('payment.nineplus.selectProduct')"
        >
          <button
            v-for="product in products"
            :key="product.product_id"
            type="button"
            role="radio"
            :aria-checked="product.product_id === selectedProductId"
            :data-testid="`nineplus-product-${product.product_id}`"
            :class="[
              'recharge-choice-card min-w-0 px-4 py-4 text-left',
              product.product_id === selectedProductId && 'recharge-choice-card-selected',
            ]"
            @click="emit('select-product', product.product_id)"
          >
            <span class="flex min-w-0 items-start justify-between gap-3">
              <span class="min-w-0">
                <span class="block break-words text-sm font-bold text-content-primary">
                  {{ product.display_name }}
                </span>
                <span v-if="product.description" class="mt-1 block line-clamp-2 text-xs leading-relaxed text-content-secondary">
                  {{ product.description }}
                </span>
              </span>
              <span
                v-if="product.badge"
                class="shrink-0 rounded-full border border-blue-200 bg-blue-50 px-2 py-0.5 text-[10px] font-bold text-blue-700 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-200"
              >
                {{ product.badge }}
              </span>
            </span>
            <span class="mt-4 flex flex-wrap items-end justify-between gap-2">
              <span class="text-2xl font-black tracking-tight text-content-primary">
                {{ formatNinePlusQuota(product) || product.display_name }}
              </span>
              <span class="text-sm font-bold text-content-brand">
                {{ formatAmount(ninePlusPaymentAmounts(product).price) }}
              </span>
            </span>
          </button>
        </div>

        <p v-else class="py-10 text-center text-sm text-content-secondary" role="status">
          {{ t('payment.nineplus.noProducts') }}
        </p>
      </section>

      <RechargeMethodSelector
        v-if="methods.length"
        :methods="methods"
        :selected="selectedMethod"
        @select="emit('select-method', $event)"
      />
    </div>

    <aside
      data-testid="order-summary"
      class="recharge-glass-card recharge-summary-card p-5 sm:p-6"
      :aria-label="t('payment.rechargeUi.orderSummary')"
    >
      <h2 class="recharge-section-title">{{ t('payment.rechargeUi.orderSummary') }}</h2>

      <div class="mt-5 space-y-3 text-sm">
        <div class="flex items-start justify-between gap-4">
          <span class="text-content-secondary">{{ t('payment.amountLabel') }}</span>
          <span class="tabular-nums font-semibold text-content-primary">{{ formattedPrice }}</span>
        </div>
        <div class="flex items-start justify-between gap-4">
          <span class="text-content-secondary">{{ t('payment.fee') }}</span>
          <span class="tabular-nums font-semibold text-content-primary">{{ formattedFee }}</span>
        </div>
        <div class="border-t border-dashed border-line-strong pt-4">
          <div class="flex items-end justify-between gap-4">
            <span class="font-bold text-content-primary">{{ t('payment.actualPay') }}</span>
            <span class="tabular-nums text-3xl font-black tracking-tight text-content-brand">{{ formattedTotal }}</span>
          </div>
        </div>
      </div>

      <div data-testid="estimated-credited-highlight" class="recharge-summary-highlight mt-5">
        <span class="text-xs font-bold uppercase tracking-wide text-content-secondary">
          {{ t('payment.rechargeUi.estimatedCreditedAmount') }}
        </span>
        <span class="recharge-summary-highlight-value">{{ creditedLabel || '—' }}</span>
      </div>

      <div
        v-if="errorMessage"
        class="mt-4 rounded-xl border border-status-danger-border bg-status-danger-soft px-3 py-2 text-sm text-status-danger"
        role="alert"
      >
        <p>{{ errorMessage }}</p>
        <p v-if="errorHintMessage" class="mt-1 opacity-80">{{ errorHintMessage }}</p>
      </div>

      <button
        type="button"
        data-testid="submit-recharge"
        class="recharge-primary-button mt-5 w-full"
        :disabled="disabled || submitting || !selectedProduct"
        :aria-busy="submitting"
        @click="emit('submit')"
      >
        {{ t('payment.rechargeUi.rechargeNow') }}
      </button>
    </aside>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NinePlusProduct } from '@/types/payment'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import RechargeMethodSelector from '@/components/payment/recharge/RechargeMethodSelector.vue'
import { formatNinePlusQuota, ninePlusPaymentAmounts } from './purchaseViewModels'

const props = withDefaults(defineProps<{
  products: NinePlusProduct[]
  selectedProductId?: string
  methods: PaymentMethodOption[]
  selectedMethod: string
  formatAmount: (value: number) => string
  disabled: boolean
  submitting: boolean
  errorMessage?: string
  errorHintMessage?: string
}>(), {
  selectedProductId: '',
  errorMessage: '',
  errorHintMessage: '',
})

const emit = defineEmits<{
  'select-product': [productId: string]
  'select-method': [method: string]
  submit: []
}>()

const { t } = useI18n()
const selectedProduct = computed(() =>
  props.products.find(product => product.product_id === props.selectedProductId) ?? null
)
const amounts = computed(() =>
  selectedProduct.value ? ninePlusPaymentAmounts(selectedProduct.value) : { price: 0, fee: 0, total: 0 }
)
const formattedPrice = computed(() => props.formatAmount(amounts.value.price))
const formattedFee = computed(() => props.formatAmount(amounts.value.fee))
const formattedTotal = computed(() => props.formatAmount(amounts.value.total))
const creditedLabel = computed(() => selectedProduct.value ? formatNinePlusQuota(selectedProduct.value) : '')
</script>
