<template>
  <div data-testid="recharge-liquid-page" class="mx-auto max-w-6xl">
    <div class="recharge-page-shell space-y-5">
      <PurchaseModeTabs v-model="activeMode" :tabs="tabs" />
      <RechargeHeader
        account-name="Acme Corporation"
        :show-account-pill="activeMode === 'recharge'"
      />
      <AccountBalanceHero
        account-name="Acme Corporation"
        account-id="1000 8888 6666"
        :formatted-balance="formatCny(currentBalance)"
        :show-balance="activeMode === 'recharge'"
        :subscription-summary="activeMode === 'subscription' ? currentSubscriptionSummary : null"
      />

      <div
        v-if="activeMode === 'recharge'"
        data-testid="recharge-preview-layout"
        class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] xl:items-start"
      >
        <div class="space-y-5">
          <RechargeAmountSelector
            v-model="amount"
            :amounts="quickRechargeAmounts"
            :min="10"
            :max="500000"
            currency="CNY"
            :error="amountError"
            :format-amount="formatCny"
          />
          <RechargeMethodSelector
            :methods="methodOptions"
            :selected="selectedMethod"
            @select="selectedMethod = $event"
          />
        </div>

        <RechargeOrderSummary
          :formatted-amount="formatCny(validAmount)"
          :formatted-fee="formatCny(feeAmount)"
          :formatted-total="formatCny(totalAmount)"
          :formatted-estimated-credited-amount="formatCny(validAmount)"
          :disabled="!!amountError || validAmount <= 0"
          :submitting="false"
          @submit="showPreviewNotice = true"
        />
      </div>

      <div
        v-else
        data-testid="subscription-preview-layout"
        class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] xl:items-start"
      >
        <section class="recharge-glass-card p-5 sm:p-6" aria-labelledby="subscription-preview-title">
          <div class="mb-4">
            <p id="subscription-preview-title" class="recharge-section-title">1. {{ t('payment.tabSubscribe') }}</p>
            <p class="mt-1 text-sm text-slate-500">{{ t('payment.rechargeUi.subscriptionHint') }}</p>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <button
              v-for="plan in previewPlans"
              :key="plan.id"
              type="button"
              class="recharge-choice-card px-4 py-4 text-left"
              :class="{ 'recharge-choice-card-selected': selectedPlanId === plan.id }"
              :data-testid="`preview-subscription-plan-${plan.id}`"
              @click="selectedPlanId = plan.id"
            >
              <div class="flex items-start justify-between gap-3">
                <span>
                  <span class="block text-base font-semibold text-slate-950">{{ plan.name }}</span>
                  <span class="mt-1 block text-xs text-slate-500">{{ plan.description }}</span>
                </span>
                <span class="rounded-full bg-blue-100 px-2 py-0.5 text-[10px] font-semibold text-blue-700">
                  {{ plan.badge }}
                </span>
              </div>
              <div class="mt-4 flex items-end justify-between gap-3">
                <span class="text-2xl font-semibold text-blue-700">{{ formatCny(plan.price) }}</span>
                <span class="text-xs font-medium text-slate-500">{{ plan.quota }}</span>
              </div>
            </button>
          </div>
          <RechargeMethodSelector
            class="mt-5"
            :methods="methodOptions"
            :selected="selectedMethod"
            @select="selectedMethod = $event"
          />
        </section>

        <aside data-testid="subscription-preview-summary" class="recharge-glass-card recharge-summary-card p-5 sm:p-6">
          <p class="recharge-section-title">2. {{ t('payment.rechargeUi.orderSummary') }}</p>
          <div class="mt-5 space-y-4 text-sm">
            <div class="flex items-center justify-between gap-4">
              <span class="text-slate-500">{{ t('payment.rechargeUi.selectedPlan') }}</span>
              <span class="font-semibold text-slate-950">{{ selectedPlan.name }}</span>
            </div>
            <div class="flex items-center justify-between gap-4">
              <span class="text-slate-500">{{ t('payment.planCard.quota') }}</span>
              <span class="font-semibold text-slate-950">{{ selectedPlan.quota }}</span>
            </div>
            <div class="flex items-center justify-between gap-4">
              <span class="text-slate-500">{{ t('payment.packagePrice') }}</span>
              <span class="font-semibold text-slate-950">{{ formatCny(selectedPlan.price) }}</span>
            </div>
            <div class="border-t border-slate-200/70 pt-4">
              <div class="flex items-center justify-between gap-4">
                <span class="font-semibold text-slate-950">{{ t('payment.payableAmount') }}</span>
                <span class="text-2xl font-semibold text-blue-700">{{ formatCny(selectedPlan.price) }}</span>
              </div>
            </div>
          </div>
          <button
            data-testid="preview-submit-subscription"
            type="button"
            class="recharge-primary-button mt-6 w-full"
            @click="showPreviewNotice = true"
          >
            {{ t('payment.subscribeNow') }}
          </button>
        </aside>
      </div>

      <p
        v-if="showPreviewNotice"
        class="rounded-2xl border border-blue-200 bg-blue-50/80 px-4 py-3 text-sm text-blue-700"
        role="status"
      >
        {{ t('payment.rechargeUi.previewNotice') }}
      </p>

      <RechargeTrustBar />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import '@/components/payment/recharge/recharge-liquid.css'
import RechargeHeader from '@/components/payment/recharge/RechargeHeader.vue'
import AccountBalanceHero from '@/components/payment/recharge/AccountBalanceHero.vue'
import RechargeAmountSelector from '@/components/payment/recharge/RechargeAmountSelector.vue'
import RechargeMethodSelector from '@/components/payment/recharge/RechargeMethodSelector.vue'
import RechargeOrderSummary from '@/components/payment/recharge/RechargeOrderSummary.vue'
import RechargeTrustBar from '@/components/payment/recharge/RechargeTrustBar.vue'
import PurchaseModeTabs from '@/components/payment/recharge/PurchaseModeTabs.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'

type PurchaseMode = 'recharge' | 'subscription'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const quickRechargeAmounts = [10, 20, 50, 100, 200, 500, 1000]
const currentBalance = 12345.67
const activeMode = ref<PurchaseMode>(route.query.tab === 'subscription' ? 'subscription' : 'recharge')
const amount = ref<number | null>(50)
const selectedMethod = ref('alipay')
const selectedPlanId = ref('team-pro')
const showPreviewNotice = ref(false)
const tabs = computed<Array<{ key: PurchaseMode; label: string }>>(() => [
  { key: 'recharge', label: t('payment.tabTopUp') },
  { key: 'subscription', label: t('payment.tabSubscribe') },
])

const previewPlans = [
  {
    id: 'team-pro',
    name: '团队专业版',
    description: '适合稳定调用与团队协作',
    badge: '推荐',
    quota: '月度额度 $220',
    price: 299,
  },
  {
    id: 'enterprise',
    name: '企业旗舰版',
    description: '更高额度与企业支持',
    badge: '企业',
    quota: '月度额度 $830',
    price: 999,
  },
]

const validAmount = computed(() => amount.value ?? 0)
const feeAmount = computed(() => 0)
const totalAmount = computed(() => validAmount.value + feeAmount.value)
const selectedPlan = computed(() =>
  previewPlans.find((plan) => plan.id === selectedPlanId.value) ?? previewPlans[0]
)
const currentSubscriptionSummary = computed(() => ({
  planName: 'Pro 套餐',
  platform: 'OpenAI',
  remainingText: t('userSubscriptions.daysRemaining', { days: 21 }),
}))
const amountError = computed(() => {
  if (validAmount.value <= 0) return ''
  if (validAmount.value < 10) return t('payment.amountTooLow', { min: formatCny(10) })
  if (validAmount.value > 500000) return t('payment.amountTooHigh', { max: formatCny(500000) })
  return ''
})
const methodOptions = computed<PaymentMethodOption[]>(() => [
  { type: 'alipay', fee_rate: 0, available: true },
  { type: 'wxpay', fee_rate: 0, available: true },
  { type: 'stripe', fee_rate: 0, available: true },
  { type: 'airwallex', fee_rate: 0, available: true },
])

function formatCny(value: number): string {
  return formatPaymentAmount(value, 'CNY', 'zh')
}

watch(activeMode, (mode) => {
  showPreviewNotice.value = false
  router.replace({ query: { ...route.query, tab: mode } })
})
</script>
