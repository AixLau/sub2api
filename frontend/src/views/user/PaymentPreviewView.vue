<template>
  <main class="min-h-screen bg-surface-canvas px-2 py-3 sm:px-3 sm:py-4 lg:px-8 lg:py-5">
    <div data-testid="recharge-liquid-page" class="recharge-page-canvas">
      <div
        data-testid="purchase-preview-shell"
        class="purchase-select-shell recharge-page-content mx-auto w-full max-w-none"
      >
        <PurchaseModeTabs v-model="activeMode" :tabs="tabs" />

        <PurchasePageStage :formatted-balance="formatUsd(previewBalance)">
          <section
            v-if="activeMode === 'recharge'"
            id="purchase-panel-recharge"
            data-testid="recharge-preview-panel"
            role="tabpanel"
            aria-labelledby="purchase-tab-recharge"
            tabindex="0"
            class="purchase-business-panel focus:outline-none"
          >
            <RechargeCheckoutPanel
              v-model="amount"
              :amounts="quickRechargeAmounts"
              :min="minimumRechargeAmount"
              :max="maximumRechargeAmount"
              currency="CNY"
              :locale="locale"
              :amount-error="amountError"
              :format-amount="formatCny"
              :format-credited-amount="formatUsdCompact"
              :methods="methodOptions"
              :selected-method="selectedMethod"
              :formatted-amount="formatCny(validAmount)"
              :formatted-fee="formatCny(rechargeFeeAmount)"
              :formatted-total="formatCny(rechargeTotalAmount)"
              :formatted-estimated-credited-amount="formatUsd(rechargeCreditedAmount)"
              :disabled="!canSubmitRecharge"
              :submitting="false"
              :one-to-one-configured="true"
              @select-method="selectedMethod = $event"
              @submit="showPreviewNotice = true"
            />
          </section>

          <section
            v-else
            id="purchase-panel-subscription"
            data-testid="subscription-preview-panel"
            role="tabpanel"
            aria-labelledby="purchase-tab-subscription"
            tabindex="0"
            class="purchase-business-panel focus:outline-none"
          >
            <SubscriptionCheckoutPanel
              :plans="previewPlans"
              :active-subscriptions="activeSubscriptions"
              :current-subscription="currentSubscriptionSummary"
              :methods="methodOptions"
              :selected-method="selectedMethod"
              :selected-option-key="selectedSubscriptionOptionKey"
              :renewal-group-id="currentSubscriptionGroupId"
              :payment-amount="subscriptionPaymentAmount"
              :fee-amount="subscriptionFeeAmount"
              :total-amount="subscriptionTotalAmount"
              :payment-currency="selectedSubscriptionCurrency"
              :can-submit="canSubmitSubscription"
              :submitting="false"
              @select-option="selectSubscriptionOption"
              @select-method="selectedMethod = $event"
              @submit="showPreviewNotice = true"
            />
          </section>

          <template #trust>
            <RechargeTrustBar />
          </template>
        </PurchasePageStage>

        <p
          v-if="showPreviewNotice"
          data-testid="purchase-preview-notice"
          class="rounded-2xl border border-blue-200 bg-blue-50/80 px-4 py-3 text-sm text-blue-700 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-200"
          role="status"
        >
          {{ t('payment.rechargeUi.previewNotice') }}
        </p>

      </div>
    </div>
  </main>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import '@/components/payment/recharge/recharge-liquid.css'
import PurchasePageStage from '@/components/payment/purchase/PurchasePageStage.vue'
import RechargeCheckoutPanel from '@/components/payment/purchase/RechargeCheckoutPanel.vue'
import SubscriptionCheckoutPanel from '@/components/payment/purchase/SubscriptionCheckoutPanel.vue'
import type { PurchaseSubscriptionOption } from '@/components/payment/purchase/purchaseViewModels'
import PurchaseModeTabs from '@/components/payment/recharge/PurchaseModeTabs.vue'
import RechargeTrustBar from '@/components/payment/recharge/RechargeTrustBar.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import type { UserSubscription } from '@/types'
import type { SubscriptionPlan } from '@/types/payment'

type PurchaseMode = 'recharge' | 'subscription'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()

const previewBalance = 7365.87
const previewBalanceRechargeMultiplier = 1
const quickRechargeAmounts = [10, 20, 50, 100, 200, 500, 1000]
const minimumRechargeAmount = 10
const maximumRechargeAmount = 500000
const currentSubscriptionGroupId = 101

const activeMode = ref<PurchaseMode>(
  route.query.tab === 'subscription' ? 'subscription' : 'recharge',
)
const amount = ref<number | null>(100)
const selectedMethod = ref('alipay')
const selectedSubscriptionOptionKey = ref('internal:101')
const showPreviewNotice = ref(false)

const tabs = computed<Array<{
  key: PurchaseMode
  label: string
  panelId: string
}>>(() => [
  {
    key: 'recharge',
    label: t('payment.tabTopUp'),
    panelId: 'purchase-panel-recharge',
  },
  {
    key: 'subscription',
    label: t('payment.tabSubscribe'),
    panelId: 'purchase-panel-subscription',
  },
])

const previewPlans: SubscriptionPlan[] = [
  {
    id: 101,
    group_id: currentSubscriptionGroupId,
    group_platform: 'openai',
    group_name: 'OpenAI 创作服务',
    rate_multiplier: 1,
    daily_limit_usd: 10,
    weekly_limit_usd: 50,
    monthly_limit_usd: 180,
    supported_model_scopes: ['gpt-5', 'gpt-4.1'],
    name: '创作月度套餐',
    description: '适合日常创作与稳定 API 调用',
    price: 39,
    currency: 'CNY',
    validity_days: 1,
    validity_unit: 'month',
    features: ['每月 API 额度', '支持主流 OpenAI 模型', '到期后手动续费'],
    for_sale: true,
    sort_order: 1,
  },
  {
    id: 102,
    group_id: 102,
    group_platform: 'anthropic',
    group_name: 'Claude 专业服务',
    rate_multiplier: 1,
    daily_limit_usd: 20,
    weekly_limit_usd: 100,
    monthly_limit_usd: 360,
    supported_model_scopes: ['claude-sonnet-4', 'claude-opus-4'],
    name: '创作季度套餐',
    description: '适合持续开发、团队协作与高频调用',
    price: 99,
    original_price: 117,
    currency: 'CNY',
    validity_days: 3,
    validity_unit: 'month',
    features: ['季度 API 额度', '支持 Claude 专业模型', '一次性购买'],
    for_sale: true,
    sort_order: 2,
  },
  {
    id: 103,
    group_id: 103,
    group_platform: 'gemini',
    group_name: 'Gemini 年度服务',
    rate_multiplier: 1,
    daily_limit_usd: 35,
    weekly_limit_usd: 180,
    monthly_limit_usd: 720,
    supported_model_scopes: ['gemini-2.5-pro', 'gemini-2.5-flash'],
    name: '创作年度套餐',
    description: '适合长期项目与更高额度需求',
    price: 299,
    original_price: 468,
    currency: 'CNY',
    validity_days: 365,
    validity_unit: 'day',
    features: ['年度 API 额度', '支持 Gemini 系列模型', '人工续费更可控'],
    for_sale: true,
    sort_order: 3,
  },
]

const activeSubscriptions: UserSubscription[] = [
  {
    id: 1,
    user_id: 1,
    group_id: currentSubscriptionGroupId,
    status: 'active',
    starts_at: '2026-08-01T00:00:00Z',
    expires_at: '2026-09-01T00:00:00Z',
    daily_usage_usd: 1.25,
    weekly_usage_usd: 8.5,
    monthly_usage_usd: 42,
    monthly_bonus_usd: 0,
    pending_renewal_count: 1,
    pending_renewals: [],
    daily_window_start: '2026-08-23T00:00:00Z',
    weekly_window_start: '2026-08-18T00:00:00Z',
    monthly_window_start: '2026-08-01T00:00:00Z',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-23T00:00:00Z',
  },
]

const currentSubscriptionSummary = {
  planName: '创作月度套餐',
  platform: 'OpenAI',
  remainingText: t('userSubscriptions.daysRemaining', { days: 9 }),
  pendingCount: 1,
  pendingDays: 30,
}

const methodOptions: PaymentMethodOption[] = [
  { type: 'alipay', fee_rate: 0, available: true },
  { type: 'wxpay', fee_rate: 0, available: true },
  { type: 'stripe', fee_rate: 1.2, available: true },
  { type: 'airwallex', fee_rate: 0.6, available: true },
]

const validAmount = computed(() => amount.value ?? 0)
const rechargeCreditedAmount = computed(() =>
  roundCurrency(validAmount.value * previewBalanceRechargeMultiplier),
)
const selectedPaymentMethod = computed(() =>
  methodOptions.find(method => method.type === selectedMethod.value),
)
const selectedPaymentFeeRate = computed(() => selectedPaymentMethod.value?.fee_rate ?? 0)
const rechargeFeeAmount = computed(() =>
  roundCurrency(validAmount.value * selectedPaymentFeeRate.value / 100),
)
const rechargeTotalAmount = computed(() =>
  roundCurrency(validAmount.value + rechargeFeeAmount.value),
)
const amountError = computed(() => {
  if (validAmount.value <= 0) return ''
  if (validAmount.value < minimumRechargeAmount) {
    return t('payment.amountTooLow', { min: formatCny(minimumRechargeAmount) })
  }
  if (validAmount.value > maximumRechargeAmount) {
    return t('payment.amountTooHigh', { max: formatCny(maximumRechargeAmount) })
  }
  return ''
})
const canSubmitRecharge = computed(() =>
  validAmount.value > 0
  && !amountError.value
  && selectedPaymentMethod.value?.available === true,
)

const selectedPreviewPlan = computed(() => {
  const id = Number(selectedSubscriptionOptionKey.value.replace('internal:', ''))
  return previewPlans.find(plan => plan.id === id) ?? null
})
const subscriptionPaymentAmount = computed(() => selectedPreviewPlan.value?.price ?? 0)
const subscriptionFeeAmount = computed(() =>
  roundCurrency(subscriptionPaymentAmount.value * selectedPaymentFeeRate.value / 100),
)
const subscriptionTotalAmount = computed(() =>
  roundCurrency(subscriptionPaymentAmount.value + subscriptionFeeAmount.value),
)
const selectedSubscriptionCurrency = computed(() =>
  selectedPreviewPlan.value?.currency || 'CNY',
)
const canSubmitSubscription = computed(() =>
  selectedPreviewPlan.value !== null && selectedPaymentMethod.value?.available === true,
)

function roundCurrency(value: number): number {
  return Math.round(value * 100) / 100
}

function formatCny(value: number): string {
  return formatPaymentAmount(value, 'CNY', locale.value)
}

function formatUsd(value: number): string {
  return formatPaymentAmount(value, 'USD', locale.value)
}

function formatUsdCompact(value: number): string {
  try {
    return new Intl.NumberFormat(locale.value, {
      style: 'currency',
      currency: 'USD',
      currencyDisplay: 'narrowSymbol',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(value)
  } catch {
    return `$${value.toFixed(2)}`
  }
}

function selectSubscriptionOption(option: PurchaseSubscriptionOption): void {
  selectedSubscriptionOptionKey.value = option.key
}

watch(activeMode, (mode) => {
  showPreviewNotice.value = false
  const query: LocationQueryRaw = { ...route.query, tab: mode }
  if (mode === 'recharge') delete query.group
  void router.replace({ query })
})

watch(
  () => route.query.tab,
  (tab) => {
    const nextMode: PurchaseMode = tab === 'subscription' ? 'subscription' : 'recharge'
    if (nextMode !== activeMode.value) activeMode.value = nextMode
  },
)
</script>
