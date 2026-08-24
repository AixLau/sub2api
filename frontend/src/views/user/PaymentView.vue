<template>
  <AppLayout>
    <div data-testid="recharge-liquid-page" class="recharge-page-canvas">
      <div class="recharge-page-content mx-auto w-full max-w-none">
        <div v-if="loading" class="flex min-h-[28rem] items-center justify-center">
          <div
            class="h-9 w-9 animate-spin rounded-full border-4 border-primary-500 border-t-transparent motion-reduce:animate-none"
            role="status"
            :aria-label="t('common.loading')"
          ></div>
        </div>

        <div v-else-if="paymentPhase === 'select'" class="purchase-select-shell">
          <PurchaseModeTabs v-model="activeTab" :tabs="tabs" />

          <PurchasePageStage :formatted-balance="formattedCurrentBalance">
            <section
              v-if="activeTab === 'recharge'"
              id="purchase-panel-recharge"
              role="tabpanel"
              aria-labelledby="purchase-tab-recharge"
              tabindex="0"
              class="purchase-business-panel focus:outline-none"
            >
              <div
                v-if="enabledMethods.length === 0"
                class="recharge-glass-card py-16 text-center"
                role="status"
              >
                <p class="text-content-secondary">{{ t('payment.notAvailable') }}</p>
              </div>

              <NinePlusRechargeCheckoutPanel
                v-else-if="isNinePlusSelected"
                :products="availableNinePlusProducts"
                :selected-product-id="selectedNinePlusProductId"
                :methods="methodOptions"
                :selected-method="selectedMethod"
                :format-amount="formatSelectedPaymentAmount"
                :disabled="!canSubmit"
                :submitting="submitting"
                :error-message="errorMessage"
                :error-hint-message="errorHintMessage"
                @select-product="selectedNinePlusProductId = $event"
                @select-method="selectRechargeMethod"
                @submit="handleSubmitRecharge"
              />

              <RechargeCheckoutPanel
                v-else
                v-model="amount"
                :amounts="quickRechargeAmounts"
                :min="effectiveMinAmount"
                :max="effectiveMaxAmount"
                :currency="selectedCurrency"
                :locale="localeCode"
                :max-fraction-digits="rechargeFractionDigits"
                :amount-error="amountError"
                :format-amount="formatSelectedPaymentAmount"
                :format-credited-amount="formatCreditedPresetAmount"
                :methods="methodOptions"
                :selected-method="selectedMethod"
                :formatted-amount="formatSelectedPaymentAmount(validAmount)"
                :formatted-fee="formatSelectedPaymentAmount(feeAmount)"
                :formatted-total="formatSelectedPaymentAmount(totalAmount)"
                :formatted-estimated-credited-amount="formattedEstimatedCreditedAmount"
                :disabled="!canSubmit"
                :submitting="submitting"
                :has-submitted="submitAttempted"
                :error-message="errorMessage"
                :error-hint-message="errorHintMessage"
                :one-to-one-configured="oneToOneConfigured"
                :configuration-warning="oneToOneConfigurationWarning"
                @select-method="selectRechargeMethod"
                @submit="handleSubmitRecharge"
              />
            </section>

            <section
              v-else
              id="purchase-panel-subscription"
              role="tabpanel"
              aria-labelledby="purchase-tab-subscription"
              tabindex="0"
              class="purchase-business-panel focus:outline-none"
            >
              <SubscriptionCheckoutPanel
                :plans="checkout.plans"
                :nine-plus-products="ninePlusSubscriptionProducts"
                :active-subscriptions="activeSubscriptions"
                :current-subscription="currentSubscriptionSummary"
                :methods="subscriptionMethodOptions"
                :selected-method="selectedMethod"
                :selected-option-key="selectedSubscriptionOptionKey"
                :renewal-group-id="renewGroupId"
                :payment-amount="subPaymentAmount"
                :fee-amount="subFeeAmount"
                :total-amount="subTotalAmount"
                :original-amount="subOriginalAmount"
                :discount-amount="subDiscountAmount"
                :payment-currency="selectedCurrency"
                :can-submit="canSubmitSubscription"
                :submitting="submitting"
                :error-message="errorMessage"
                @select-option="selectSubscriptionOption"
                @select-method="selectSubscriptionMethod"
                @submit="confirmSubscribeOption"
              />
            </section>

            <template #trust>
              <RechargeTrustBar />
            </template>
          </PurchasePageStage>

          <section
            v-if="checkout.help_text || checkout.help_image_url"
            class="recharge-glass-card p-4"
            :aria-label="t('payment.rechargeUi.support247')"
          >
            <div class="flex flex-col items-center gap-3">
              <button
                v-if="checkout.help_image_url"
                type="button"
                class="rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2"
                :aria-label="t('payment.rechargeUi.support247')"
                @click="previewImage = checkout.help_image_url"
              >
                <img
                  :src="checkout.help_image_url"
                  alt=""
                  class="h-40 max-w-full rounded-lg object-contain transition-opacity hover:opacity-80"
                />
              </button>
              <p v-if="checkout.help_text" class="text-center text-sm text-content-secondary">
                {{ checkout.help_text }}
              </p>
            </div>
          </section>
        </div>

        <div v-else class="mx-auto w-full max-w-3xl py-6">
          <PaymentStatusPanel
            :order-id="paymentState.orderId"
            :amount="paymentState.amount"
            :qr-code="paymentState.qrCode"
            :expires-at="paymentState.expiresAt"
            :payment-type="paymentState.paymentType"
            :provider-key="paymentState.providerKey"
            :pay-url="paymentState.payUrl"
            :order-type="paymentState.orderType"
            :currency="paymentState.currency || selectedCurrency"
            :pay-amount="paymentState.payAmount || paymentState.amount"
            :out-trade-no="paymentState.outTradeNo"
            :mobile-alipay-deep-link="paymentState.alipayMobilePrecreateDeepLink"
            @done="onPaymentDone"
            @success="onPaymentSuccess"
            @settled="onPaymentSettled"
          />
        </div>
      </div>
    </div>

    <Teleport to="body">
      <Transition name="modal">
        <div
          v-if="previewImage"
          class="fixed inset-0 z-[60] flex items-center justify-center bg-surface-scrim/75 p-4 backdrop-blur-sm"
          @click="previewImage = ''"
        >
          <img
            :src="previewImage"
            alt=""
            class="max-h-[85vh] max-w-[90vw] rounded-xl object-contain shadow-2xl"
          />
        </div>
      </Transition>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { usePaymentStore } from '@/stores/payment'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { useAppStore } from '@/stores'
import { paymentAPI } from '@/api/payment'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import { isMobileDevice } from '@/utils/device'
import type {
  SubscriptionPlan,
  CheckoutInfoResponse,
  CreateOrderResult,
  NinePlusProduct,
  OrderType,
} from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import RechargeTrustBar from '@/components/payment/recharge/RechargeTrustBar.vue'
import PurchaseModeTabs from '@/components/payment/recharge/PurchaseModeTabs.vue'
import PurchasePageStage from '@/components/payment/purchase/PurchasePageStage.vue'
import RechargeCheckoutPanel from '@/components/payment/purchase/RechargeCheckoutPanel.vue'
import NinePlusRechargeCheckoutPanel from '@/components/payment/purchase/NinePlusRechargeCheckoutPanel.vue'
import SubscriptionCheckoutPanel from '@/components/payment/purchase/SubscriptionCheckoutPanel.vue'
import {
  buildPurchaseSubscriptionOptions,
  findPurchaseSubscriptionOption,
  isNinePlusProductInStock,
  isNinePlusSubscriptionProduct,
  ninePlusPaymentAmounts,
  type CurrentSubscriptionSummary,
  type PurchaseSubscriptionOption,
} from '@/components/payment/purchase/purchaseViewModels'
import { METHOD_ORDER, getPaymentPopupFeatures } from '@/components/payment/providerConfig'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  buildCreateOrderPayload,
  clearPaymentRecoverySnapshot,
  decidePaymentLaunch,
  getVisibleMethods,
  normalizeVisibleMethod,
  readPaymentRecoverySnapshot,
  type PaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import { platformLabel } from '@/utils/platformColors'
import PaymentStatusPanel from '@/components/payment/PaymentStatusPanel.vue'
import { DEFAULT_PAYMENT_CURRENCY, formatPaymentAmount, normalizePaymentCurrency } from '@/components/payment/currency'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import { buildPaymentErrorToastMessage, describePaymentScenarioError } from './paymentUx'
import { hasWechatResumeQuery, parseWechatResumeRoute, stripWechatResumeQuery } from './paymentWechatResume'
import '@/components/payment/recharge/recharge-liquid.css'

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const paymentStore = usePaymentStore()
const subscriptionStore = useSubscriptionStore()
const appStore = useAppStore()

const user = computed(() => authStore.user)
const activeSubscriptions = computed(() => subscriptionStore.activeSubscriptions)

function getDaysRemaining(expiresAt: string): number {
  const diff = new Date(expiresAt).getTime() - Date.now()
  return Math.max(0, Math.ceil(diff / (1000 * 60 * 60 * 24)))
}

const loading = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const errorHintMessage = ref('')
const submitAttempted = ref(false)
type PurchaseMode = 'recharge' | 'subscription'
const activeTab = ref<PurchaseMode>(route.query.tab === 'subscription' ? 'subscription' : 'recharge')
const checkoutReady = ref(false)
const amount = ref<number | null>(null)
const selectedMethod = ref('')
const selectedNinePlusProductId = ref('')
const selectedSubscriptionOptionKey = ref('')
const renewGroupId = ref<number | null>(null)
const previewImage = ref('')

const paymentPhase = ref<'select' | 'paying'>('select')

interface CreateOrderOptions {
  openid?: string
  wechatResumeToken?: string
  paymentType?: string
  externalProduct?: NinePlusProduct | null
  isResume?: boolean
  mobileQrFallbackAttempted?: boolean
}

interface WeixinJSBridgeLike {
  invoke(
    action: string,
    payload: Record<string, unknown>,
    callback: (result: Record<string, unknown>) => void,
  ): void
}

function emptyPaymentState(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    providerKey: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    resumeToken: '',
    alipayMobilePrecreateDeepLink: false,
    createdAt: 0,
  }
}

function getWeixinJSBridge(): WeixinJSBridgeLike | undefined {
  return (window as Window & { WeixinJSBridge?: WeixinJSBridgeLike }).WeixinJSBridge
}

function waitForWeixinJSBridge(timeoutMs = 4000): Promise<WeixinJSBridgeLike | null> {
  const existing = getWeixinJSBridge()
  if (existing) return Promise.resolve(existing)

  return new Promise((resolve) => {
    let settled = false
    const finish = (bridge: WeixinJSBridgeLike | null) => {
      if (settled) return
      settled = true
      document.removeEventListener('WeixinJSBridgeReady', handleReady)
      document.removeEventListener('onWeixinJSBridgeReady', handleReady)
      window.clearTimeout(timer)
      resolve(bridge)
    }
    const handleReady = () => finish(getWeixinJSBridge() ?? null)
    const timer = window.setTimeout(() => finish(getWeixinJSBridge() ?? null), timeoutMs)
    document.addEventListener('WeixinJSBridgeReady', handleReady, false)
    document.addEventListener('onWeixinJSBridgeReady', handleReady, false)
  })
}

async function invokeWechatJsapiPayment(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  const bridge = await waitForWeixinJSBridge()
  if (!bridge) {
    throw new Error('WECHAT_JSAPI_UNAVAILABLE')
  }
  return new Promise((resolve) => {
    bridge.invoke('getBrandWCPayRequest', payload, (result) => resolve(result || {}))
  })
}

const paymentState = ref<PaymentRecoverySnapshot>(emptyPaymentState())

function persistRecoverySnapshot(snapshot: PaymentRecoverySnapshot) {
  if (typeof window === 'undefined' || !snapshot.orderId) return
  writePaymentRecoverySnapshot(window.localStorage, snapshot, PAYMENT_RECOVERY_STORAGE_KEY)
}

function removeRecoverySnapshot() {
  if (typeof window === 'undefined') return
  clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY)
}

function resetPayment() {
  paymentPhase.value = 'select'
  paymentState.value = emptyPaymentState()
  removeRecoverySnapshot()
}

async function redirectToPaymentResult(state: PaymentRecoverySnapshot): Promise<void> {
  const query: Record<string, string | undefined> = {}
  if (state.orderId > 0) {
    query.order_id = String(state.orderId)
  }
  if (state.outTradeNo) {
    query.out_trade_no = state.outTradeNo
  }
  if (state.resumeToken) {
    query.resume_token = state.resumeToken
  }
  await router.push({
    path: '/payment/result',
    query,
  })
}

function buildWechatOAuthAuthorizeUrl(
  authorizeUrl: string,
  context: { paymentType: string; orderType: OrderType; planId?: number; orderAmount: number },
): string {
  const normalizedUrl = authorizeUrl.trim()
  if (!normalizedUrl || typeof window === 'undefined') {
    return normalizedUrl
  }

  try {
    const targetUrl = new URL(normalizedUrl, window.location.origin)
    const redirectPath = targetUrl.searchParams.get('redirect') || '/purchase'
    const redirectUrl = new URL(redirectPath, window.location.origin)
    const paymentType = normalizeVisibleMethod(context.paymentType) || context.paymentType.trim() || 'wxpay'

    redirectUrl.searchParams.set('payment_type', paymentType)
    redirectUrl.searchParams.set('order_type', context.orderType)

    if (context.planId) {
      redirectUrl.searchParams.set('plan_id', String(context.planId))
    } else {
      redirectUrl.searchParams.delete('plan_id')
    }

    if (context.orderAmount > 0) {
      redirectUrl.searchParams.set('amount', String(context.orderAmount))
    } else {
      redirectUrl.searchParams.delete('amount')
    }

    targetUrl.searchParams.set('redirect', `${redirectUrl.pathname}${redirectUrl.search}`)
    return targetUrl.toString()
  } catch {
    return normalizedUrl
  }
}

function onPaymentDone() {
  const wasSubscription = paymentState.value.orderType === 'subscription'
  resetPayment()
  selectedSubscriptionOptionKey.value = ''
  if (wasSubscription) {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
}

async function onPaymentSuccess() {
  const completedPayment = { ...paymentState.value }
  removeRecoverySnapshot()
  authStore.refreshUser()
  if (paymentState.value.orderType === 'subscription') {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
  await redirectToPaymentResult(completedPayment)
}

function onPaymentSettled() {
  removeRecoverySnapshot()
}

// All checkout data from single API call
const checkout = ref<CheckoutInfoResponse>({
  methods: {}, global_min: 0, global_max: 0,
  plans: [], nineplus_products: [], balance_disabled: false, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0, help_text: '', help_image_url: '', stripe_publishable_key: '',
})

const quickRechargeAmounts = [10, 20, 50, 100, 200, 500, 1000]
const DEFAULT_MIN_RECHARGE_AMOUNT = 10
const DEFAULT_MAX_RECHARGE_AMOUNT = 500000

const tabs = computed(() => {
  const result: Array<{
    key: PurchaseMode
    label: string
    panelId: string
  }> = []
  if (!checkout.value.balance_disabled) {
    result.push({
      key: 'recharge',
      label: t('payment.tabTopUp'),
      panelId: 'purchase-panel-recharge',
    })
  }
  result.push({
    key: 'subscription',
    label: t('payment.tabSubscribe'),
    panelId: 'purchase-panel-subscription',
  })
  return result
})

function paymentMethodSortIndex(type: string): number {
  const index = METHOD_ORDER.indexOf(type as (typeof METHOD_ORDER)[number])
  return index < 0 ? METHOD_ORDER.length : index
}

const visibleMethods = computed(() => getVisibleMethods(checkout.value.methods))
const enabledMethods = computed(() =>
  Object.keys(visibleMethods.value).sort((left, right) =>
    paymentMethodSortIndex(left) - paymentMethodSortIndex(right) || left.localeCompare(right)
  )
)
const standardRechargeMethodTypes = computed(() =>
  enabledMethods.value.filter(type => type !== 'nineplus')
)
const isNinePlusSelected = computed(() => selectedMethod.value === 'nineplus')

const activeNinePlusProducts = computed(() =>
  (checkout.value.nineplus_products || [])
    .filter(product => product.enabled && isNinePlusProductInStock(product))
    .sort((left, right) => left.sort_order - right.sort_order)
)
const availableNinePlusProducts = computed(() =>
  activeNinePlusProducts.value
    .filter(product => !isNinePlusSubscriptionProduct(product))
    .sort((left, right) =>
      ninePlusPaymentAmounts(left).price - ninePlusPaymentAmounts(right).price
      || left.sort_order - right.sort_order
    )
)
const ninePlusSubscriptionProducts = computed(() =>
  activeNinePlusProducts.value
    .filter(isNinePlusSubscriptionProduct)
    .sort((left, right) =>
      ninePlusPaymentAmounts(left).price - ninePlusPaymentAmounts(right).price
      || left.sort_order - right.sort_order
    )
)
const selectedNinePlusProduct = computed(() =>
  availableNinePlusProducts.value.find(product => product.product_id === selectedNinePlusProductId.value) ?? null
)
const effectiveNinePlusProduct = computed(() =>
  selectedNinePlusProduct.value ?? availableNinePlusProducts.value[0] ?? null
)
const selectedNinePlusRechargeAmounts = computed(() =>
  effectiveNinePlusProduct.value
    ? ninePlusPaymentAmounts(effectiveNinePlusProduct.value)
    : { price: 0, fee: 0, total: 0 }
)

const validAmount = computed(() =>
  isNinePlusSelected.value
    ? selectedNinePlusRechargeAmounts.value.total
    : amount.value ?? 0
)
const balanceRechargeMultiplier = computed(() => {
  const multiplier = checkout.value.balance_recharge_multiplier
  // Treat an invalid checkout response as a configuration failure. Falling
  // back to 1 here would make the UI promise a credit the backend did not
  // explicitly authorize.
  return Number.isFinite(multiplier) && multiplier > 0 ? multiplier : 0
})
const oneToOneConfigured = computed(() =>
  Math.abs(balanceRechargeMultiplier.value - 1) < 1e-9
)
const oneToOneConfigurationWarning = computed(() =>
  oneToOneConfigured.value
    ? ''
    : t('payment.rechargeUi.oneToOneConfigurationWarning', {
        multiplier: balanceRechargeMultiplier.value.toFixed(4),
      })
)
const creditedAmount = computed(() =>
  Math.round(validAmount.value * balanceRechargeMultiplier.value * 100) / 100
)

// 订阅 CNY 换算汇率（1 USD = X CNY）。0 = 未配置，保持套餐 price 直付。
const subscriptionUsdToCnyRate = computed(() => {
  const rate = checkout.value.subscription_usd_to_cny_rate
  return Number.isFinite(rate) && rate > 0 ? rate : 0
})

// Check if an amount fits a method's [min, max]. 0 = no limit.
function amountFitsMethod(value: number, methodType: string): boolean {
  if (value <= 0) return true
  const limit = visibleMethods.value[methodType]
  if (!limit) return false
  if (limit.single_min > 0 && value < limit.single_min) return false
  if (limit.single_max > 0 && value > limit.single_max) return false
  return true
}

const standardRechargeLimits = computed(() =>
  standardRechargeMethodTypes.value
    .map(type => visibleMethods.value[type])
    .filter((limit): limit is NonNullable<typeof limit> => Boolean(limit))
)
const globalMinAmount = computed(() => {
  const limits = standardRechargeLimits.value
  if (limits.length === 0 || limits.some(limit => limit.single_min <= 0)) return 0
  return Math.min(...limits.map(limit => limit.single_min))
})
const globalMaxAmount = computed(() => {
  const limits = standardRechargeLimits.value
  if (limits.length === 0 || limits.some(limit => limit.single_max <= 0)) return 0
  return Math.max(...limits.map(limit => limit.single_max))
})
const effectiveMinAmount = computed(() =>
  globalMinAmount.value > 0 ? globalMinAmount.value : DEFAULT_MIN_RECHARGE_AMOUNT
)
const effectiveMaxAmount = computed(() =>
  globalMaxAmount.value > 0 ? globalMaxAmount.value : DEFAULT_MAX_RECHARGE_AMOUNT
)

const selectedLimit = computed(() => visibleMethods.value[selectedMethod.value])
const selectedCurrency = computed(() => normalizePaymentCurrency(selectedLimit.value?.currency))
const rechargeFractionDigits = computed(() =>
  Math.min(2, currencyFractionDigits(selectedCurrency.value))
)
const localeCode = computed(() => {
  const raw = i18n.locale as unknown
  if (typeof raw === 'string') return raw
  if (raw && typeof raw === 'object' && 'value' in raw) {
    return String((raw as { value?: string }).value || '')
  }
  return undefined
})

const currentBalanceAmount = computed(() => {
  const value = user.value?.balance
  return Number.isFinite(value) ? Number(value) : 0
})
const formattedCurrentBalance = computed(() =>
  formatPaymentAmount(currentBalanceAmount.value, 'USD', localeCode.value)
)
const currentSubscription = computed(() =>
  activeSubscriptions.value.find(subscription => subscription.status === 'active') ?? null
)
const currentSubscriptionSummary = computed<CurrentSubscriptionSummary | null>(() => {
  const subscription = currentSubscription.value
  if (!subscription) return null

  return {
    planName: subscription.group?.name || t('payment.groupFallback', { id: subscription.group_id }),
    platform: platformLabel(subscription.group?.platform || ''),
    remainingText: subscription.expires_at
      ? t('userSubscriptions.daysRemaining', { days: getDaysRemaining(subscription.expires_at) })
      : t('userSubscriptions.noExpiration'),
    pendingCount: subscription.pending_renewal_count || 0,
    pendingDays: (subscription.pending_renewals || []).reduce(
      (total, renewal) => total + renewal.validity_days,
      0
    ),
  }
})

function currencyFractionDigits(currency: string): number {
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
    }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

function roundPaymentAmount(value: number, currency: string): number {
  if (!Number.isFinite(value)) return 0
  const factor = 10 ** currencyFractionDigits(currency)
  return Math.round(value * factor) / factor
}

function ceilPaymentAmount(value: number, currency: string): number {
  if (!Number.isFinite(value)) return 0
  const factor = 10 ** currencyFractionDigits(currency)
  return Math.ceil(value * factor) / factor
}

function subscriptionPaymentAmountForCurrency(value: number, currency: string): number {
  const rate = subscriptionUsdToCnyRate.value
  if (rate <= 0 || currency !== DEFAULT_PAYMENT_CURRENCY) {
    return roundPaymentAmount(value, currency)
  }
  return roundPaymentAmount(value * rate, currency)
}

function formatSelectedPaymentAmount(value: number): string {
  return formatPaymentAmount(value, selectedCurrency.value, localeCode.value)
}

function formatCreditedAmount(value: number): string {
  return formatPaymentAmount(value, 'USD', localeCode.value)
}

function formatCreditedPresetAmount(value: number): string {
  try {
    return new Intl.NumberFormat(localeCode.value, {
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

function hasSupportedRechargePrecision(value: number): boolean {
  const factor = 10 ** rechargeFractionDigits.value
  return Math.abs(value * factor - Math.round(value * factor)) < 1e-8
}

function standardRechargeTotalForCurrency(value: number, currency: string): number {
  const feeRate = checkout.value.recharge_fee_rate ?? 0
  if (feeRate <= 0 || value <= 0) return roundPaymentAmount(value, currency)
  const fee = ceilPaymentAmount((value * feeRate) / 100, currency)
  return roundPaymentAmount(value + fee, currency)
}

function rechargeAmountForMethod(type: string): number {
  if (type === 'nineplus') return selectedNinePlusRechargeAmounts.value.total
  const currency = normalizePaymentCurrency(visibleMethods.value[type]?.currency)
  return standardRechargeTotalForCurrency(amount.value ?? 0, currency)
}

const methodOptions = computed<PaymentMethodOption[]>(() =>
  enabledMethods.value.map((type) => {
    const limit = visibleMethods.value[type]
    const methodAmount = rechargeAmountForMethod(type)
    const hasNinePlusProduct = type !== 'nineplus' || effectiveNinePlusProduct.value !== null
    return {
      type,
      display_name: limit?.display_name,
      fee_rate: limit?.fee_rate ?? 0,
      available: hasNinePlusProduct
        && limit?.available !== false
        && amountFitsMethod(methodAmount, type),
    }
  })
)

const feeRate = computed(() =>
  isNinePlusSelected.value ? 0 : checkout.value.recharge_fee_rate ?? 0
)
const feeAmount = computed(() =>
  feeRate.value > 0 && validAmount.value > 0
    ? ceilPaymentAmount((validAmount.value * feeRate.value) / 100, selectedCurrency.value)
    : 0
)
const totalAmount = computed(() =>
  feeRate.value > 0 && validAmount.value > 0
    ? roundPaymentAmount(validAmount.value + feeAmount.value, selectedCurrency.value)
    : validAmount.value
)
const formattedEstimatedCreditedAmount = computed(() =>
  formatCreditedAmount(creditedAmount.value)
)

const amountError = computed(() => {
  if (isNinePlusSelected.value || validAmount.value <= 0) return ''
  if (!hasSupportedRechargePrecision(validAmount.value)) {
    return t('payment.amountTooManyDecimals', { digits: rechargeFractionDigits.value })
  }
  if (validAmount.value < effectiveMinAmount.value) {
    return t('payment.amountTooLow', {
      min: formatSelectedPaymentAmount(effectiveMinAmount.value),
    })
  }
  if (validAmount.value > effectiveMaxAmount.value) {
    return t('payment.amountTooHigh', {
      max: formatSelectedPaymentAmount(effectiveMaxAmount.value),
    })
  }
  if (!standardRechargeMethodTypes.value.some(method =>
    amountFitsMethod(
      standardRechargeTotalForCurrency(
        validAmount.value,
        normalizePaymentCurrency(visibleMethods.value[method]?.currency),
      ),
      method,
    )
  )) {
    return t('payment.amountNoMethod')
  }
  const limit = selectedLimit.value
  if (limit) {
    if (limit.single_min > 0 && totalAmount.value < limit.single_min) {
      return t('payment.amountTooLow', {
        min: formatSelectedPaymentAmount(limit.single_min),
      })
    }
    if (limit.single_max > 0 && totalAmount.value > limit.single_max) {
      return t('payment.amountTooHigh', {
        max: formatSelectedPaymentAmount(limit.single_max),
      })
    }
  }
  return ''
})

const canSubmit = computed(() => {
  if (isNinePlusSelected.value) {
    return effectiveNinePlusProduct.value !== null
      && validAmount.value > 0
      && amountFitsMethod(validAmount.value, 'nineplus')
      && visibleMethods.value.nineplus?.available !== false
  }
  return oneToOneConfigured.value
    && validAmount.value >= effectiveMinAmount.value
    && validAmount.value <= effectiveMaxAmount.value
    && !amountError.value
    && amountFitsMethod(totalAmount.value, selectedMethod.value)
    && selectedLimit.value?.available !== false
})

const subscriptionOptions = computed(() =>
  buildPurchaseSubscriptionOptions(checkout.value.plans, ninePlusSubscriptionProducts.value)
)
const selectedSubscriptionOption = computed(() =>
  findPurchaseSubscriptionOption(subscriptionOptions.value, selectedSubscriptionOptionKey.value)
)
const selectedInternalPlan = computed(() =>
  selectedSubscriptionOption.value?.source === 'internal'
    ? selectedSubscriptionOption.value.plan
    : null
)
const selectedNinePlusSubscriptionProduct = computed(() =>
  selectedSubscriptionOption.value?.source === 'nineplus'
    ? selectedSubscriptionOption.value.product
    : null
)

function subscriptionMethodTypesForOption(
  option: PurchaseSubscriptionOption | null,
): string[] {
  if (option?.source === 'nineplus') {
    return enabledMethods.value.includes('nineplus') ? ['nineplus'] : []
  }
  if (option?.source === 'internal') {
    return enabledMethods.value.filter(type => type !== 'nineplus')
  }
  if (checkout.value.plans.length > 0) {
    return enabledMethods.value.filter(type => type !== 'nineplus')
  }
  if (ninePlusSubscriptionProducts.value.length > 0 && enabledMethods.value.includes('nineplus')) {
    return ['nineplus']
  }
  return enabledMethods.value.filter(type => type !== 'nineplus')
}

function subscriptionAmountsForOption(
  option: PurchaseSubscriptionOption | null,
  currency: string,
): { price: number; fee: number; total: number; original: number; discount: number } {
  if (!option) {
    return { price: 0, fee: 0, total: 0, original: 0, discount: 0 }
  }
  if (option.source === 'nineplus') {
    const amounts = ninePlusPaymentAmounts(option.product)
    const original = option.originalPrice ?? 0
    return {
      ...amounts,
      original,
      discount: Math.max(0, original - amounts.price),
    }
  }

  const price = subscriptionPaymentAmountForCurrency(option.plan.price, currency)
  const fee = checkout.value.recharge_fee_rate > 0 && price > 0
    ? ceilPaymentAmount((price * checkout.value.recharge_fee_rate) / 100, currency)
    : 0
  const original = option.originalPrice
    ? subscriptionPaymentAmountForCurrency(option.originalPrice, currency)
    : 0
  return {
    price,
    fee,
    total: roundPaymentAmount(price + fee, currency),
    original,
    discount: Math.max(0, original - price),
  }
}

const selectedSubscriptionAmounts = computed(() =>
  subscriptionAmountsForOption(selectedSubscriptionOption.value, selectedCurrency.value)
)
const subPaymentAmount = computed(() => selectedSubscriptionAmounts.value.price)
const subFeeAmount = computed(() => selectedSubscriptionAmounts.value.fee)
const subTotalAmount = computed(() => selectedSubscriptionAmounts.value.total)
const subOriginalAmount = computed(() => selectedSubscriptionAmounts.value.original)
const subDiscountAmount = computed(() => selectedSubscriptionAmounts.value.discount)

const subscriptionMethodOptions = computed<PaymentMethodOption[]>(() =>
  subscriptionMethodTypesForOption(selectedSubscriptionOption.value).map((type) => {
    const limit = visibleMethods.value[type]
    const currency = normalizePaymentCurrency(limit?.currency)
    const amounts = subscriptionAmountsForOption(selectedSubscriptionOption.value, currency)
    return {
      type,
      display_name: limit?.display_name,
      fee_rate: limit?.fee_rate ?? 0,
      available: limit?.available !== false
        && amountFitsMethod(amounts.total, type),
    }
  })
)

const canSubmitSubscription = computed(() => {
  const option = selectedSubscriptionOption.value
  if (!option || subTotalAmount.value <= 0) return false
  if (option.source === 'nineplus' && selectedMethod.value !== 'nineplus') return false
  if (option.source === 'internal' && selectedMethod.value === 'nineplus') return false
  return amountFitsMethod(subTotalAmount.value, selectedMethod.value)
    && selectedLimit.value?.available !== false
})

function firstAvailableMethod(options: PaymentMethodOption[]): string {
  return options.find(option => option.available !== false)?.type || options[0]?.type || ''
}

function selectRechargeMethod(method: string): void {
  selectedMethod.value = method
  errorMessage.value = ''
  errorHintMessage.value = ''
}

function selectSubscriptionMethod(method: string): void {
  selectedMethod.value = method
  errorMessage.value = ''
  errorHintMessage.value = ''
}

function selectSubscriptionOption(option: PurchaseSubscriptionOption): void {
  selectedSubscriptionOptionKey.value = option.key
  const options = subscriptionMethodTypesForOption(option).map(type => {
    const limit = visibleMethods.value[type]
    const currency = normalizePaymentCurrency(limit?.currency)
    const amounts = subscriptionAmountsForOption(option, currency)
    return {
      type,
      available: limit?.available !== false
        && amountFitsMethod(amounts.total, type),
    } as PaymentMethodOption
  })
  if (!options.some(method => method.type === selectedMethod.value && method.available !== false)) {
    selectedMethod.value = firstAvailableMethod(options)
  }
  errorMessage.value = ''
  errorHintMessage.value = ''
}

function selectInternalPlan(plan: SubscriptionPlan): void {
  const option = subscriptionOptions.value.find(candidate =>
    candidate.source === 'internal' && candidate.id === plan.id
  )
  if (option) selectSubscriptionOption(option)
}

watch(availableNinePlusProducts, (products) => {
  if (!products.length) {
    selectedNinePlusProductId.value = ''
    return
  }
  if (!products.some(product => product.product_id === selectedNinePlusProductId.value)) {
    selectedNinePlusProductId.value = products[0].product_id
  }
}, { immediate: true })

watch(subscriptionOptions, (options) => {
  if (
    selectedSubscriptionOptionKey.value
    && !options.some(option => option.key === selectedSubscriptionOptionKey.value)
  ) {
    selectedSubscriptionOptionKey.value = ''
  }
}, { immediate: true })

watch(() => [totalAmount.value, selectedMethod.value, activeTab.value] as const, ([value, method, tab]) => {
  if (tab !== 'recharge' || isNinePlusSelected.value || value <= 0 || amountFitsMethod(value, method)) {
    return
  }
  const fallback = methodOptions.value.find(option =>
    option.type !== 'nineplus' && option.available !== false
  )
  if (fallback) selectedMethod.value = fallback.type
})

function desiredPurchaseMode(): PurchaseMode {
  if (checkout.value.balance_disabled) return 'subscription'
  return route.query.tab === 'subscription' ? 'subscription' : 'recharge'
}

async function syncActiveTabQuery(mode: PurchaseMode): Promise<void> {
  if (!checkoutReady.value || paymentPhase.value !== 'select') return

  const query = { ...route.query }
  const mustDropGroup = mode === 'recharge' && 'group' in query
  if (query.tab === mode && !mustDropGroup) return

  query.tab = mode
  if (mode === 'recharge') delete query.group
  await router.replace({ path: route.path, query })
}

function applyRenewalGroupFromQuery(): void {
  renewGroupId.value = null
  if (activeTab.value !== 'subscription' || typeof route.query.group !== 'string') return

  const groupId = Number(route.query.group)
  if (!Number.isSafeInteger(groupId) || groupId <= 0) return

  const groupPlans = checkout.value.plans.filter(plan => plan.group_id === groupId)
  if (!groupPlans.length) return

  renewGroupId.value = groupId
  if (groupPlans.length === 1) {
    selectInternalPlan(groupPlans[0])
    return
  }

  if (
    selectedInternalPlan.value
    && selectedInternalPlan.value.group_id !== groupId
  ) {
    selectedSubscriptionOptionKey.value = ''
  }
}

function ensureMethodForMode(mode: PurchaseMode): void {
  const options = mode === 'subscription'
    ? subscriptionMethodOptions.value
    : methodOptions.value
  if (!options.some(option => option.type === selectedMethod.value && option.available !== false)) {
    selectedMethod.value = firstAvailableMethod(options)
  }
}

watch(activeTab, (mode) => {
  if (!checkoutReady.value) return
  if (mode === 'recharge') renewGroupId.value = null
  ensureMethodForMode(mode)
  void syncActiveTabQuery(mode)
})

watch(
  () => [route.query.tab, route.query.group] as const,
  () => {
    if (!checkoutReady.value || paymentPhase.value !== 'select') return
    const mode = desiredPurchaseMode()
    if (activeTab.value !== mode) activeTab.value = mode
    applyRenewalGroupFromQuery()
    ensureMethodForMode(mode)
  },
)

async function handleSubmitRecharge() {
  if (!canSubmit.value || submitting.value) return
  await createOrder(validAmount.value, 'balance')
}

async function confirmSubscribeOption(option: PurchaseSubscriptionOption) {
  if (submitting.value || option.key !== selectedSubscriptionOptionKey.value) return

  if (option.source === 'nineplus') {
    const amounts = ninePlusPaymentAmounts(option.product)
    await createOrder(amounts.total, 'subscription', undefined, {
      externalProduct: option.product,
    })
    return
  }

  await createOrder(option.plan.price, 'subscription', option.plan.id)
}

async function createOrder(orderAmount: number, orderType: OrderType, planId?: number, options: CreateOrderOptions = {}) {
  submitting.value = true
  submitAttempted.value = true
  errorMessage.value = ''
  errorHintMessage.value = ''
  const requestType = normalizeVisibleMethod(options.paymentType || selectedMethod.value) || options.paymentType || selectedMethod.value
  const defaultExternalProduct = orderType === 'subscription'
    ? selectedNinePlusSubscriptionProduct.value
    : effectiveNinePlusProduct.value
  const ninePlusProduct = requestType === 'nineplus' ? options.externalProduct ?? defaultExternalProduct : null
  try {
    const payload = buildCreateOrderPayload({
      amount: orderAmount,
      paymentType: requestType,
      orderType,
      planId,
      externalProductId: ninePlusProduct?.product_id,
      externalQuantity: ninePlusProduct ? 1 : undefined,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && normalizeVisibleMethod(requestType) === 'alipay'),
      mobilePrecreateDeepLink: checkout.value.alipay_mobile_precreate_deep_link === true,
    })
    if (options.openid) {
      payload.openid = options.openid
    }
    if (options.wechatResumeToken) {
      payload.wechat_resume_token = options.wechatResumeToken
    }

    const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
    const openWindow = (url: string) => {
      const win = window.open(url, 'paymentPopup', getPaymentPopupFeatures())
      if (!win || win.closed) {
        window.location.href = url
      }
    }
    const visibleMethod = normalizeVisibleMethod(requestType) || requestType
    // When user clicks the dedicated Stripe button, leave method blank so the
    // landing page renders Stripe's full Payment Element (card/link/alipay/wxpay).
    const stripeMethod = visibleMethod === 'stripe'
      ? ''
      : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const stripeRouteUrl = result.client_secret && visibleMethod !== 'airwallex'
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          client_secret: result.client_secret,
          method: stripeMethod || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const airwallexRouteUrl = result.client_secret && result.intent_id
      ? router.resolve({
        path: '/payment/airwallex',
        query: {
          order_id: String(result.order_id),
          out_trade_no: result.out_trade_no || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType,
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && visibleMethod === 'alipay'),
      mobilePrecreateDeepLink: checkout.value.alipay_mobile_precreate_deep_link === true,
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
      airwallexRouteUrl,
    })

    if (decision.kind === 'wechat_oauth' && decision.oauth?.authorize_url) {
      window.location.href = buildWechatOAuthAuthorizeUrl(decision.oauth.authorize_url, {
        paymentType: visibleMethod,
        orderType,
        planId,
        orderAmount,
      })
      return
    }

    if (decision.kind === 'unhandled') {
      applyScenarioError({ reason: 'UNHANDLED_PAYMENT_SCENARIO' }, visibleMethod)
      return
    }

    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)

    if (decision.kind === 'stripe_popup') {
      openWindow(decision.paymentState.payUrl)
      return
    }
    if (decision.kind === 'stripe_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'airwallex_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'wechat_jsapi' && decision.jsapi) {
      try {
        const jsapiResult = await invokeWechatJsapiPayment(decision.jsapi as Record<string, unknown>)
        const errMsg = String(jsapiResult.err_msg || '').toLowerCase()
        if (errMsg.includes('cancel')) {
          appStore.showInfo(t('payment.qr.cancelled'))
          resetPayment()
        } else if (errMsg && !errMsg.includes('ok')) {
          resetPayment()
          const fallbackApplied = await attemptMobileQrFallback(
            { reason: 'WECHAT_JSAPI_FAILED', message: errMsg },
            {
              orderAmount,
              orderType,
              planId,
              paymentType: visibleMethod,
              externalProduct: ninePlusProduct,
              attempted: options.mobileQrFallbackAttempted === true,
            },
          )
          if (!fallbackApplied) {
            applyScenarioError({ reason: 'WECHAT_JSAPI_FAILED', message: errMsg }, visibleMethod)
          }
        } else {
          const resultState = { ...decision.paymentState }
          resetPayment()
          await redirectToPaymentResult(resultState)
        }
      } catch (err: unknown) {
        resetPayment()
        const fallbackApplied = await attemptMobileQrFallback(err, {
          orderAmount,
          orderType,
          planId,
          paymentType: visibleMethod,
          externalProduct: ninePlusProduct,
          attempted: options.mobileQrFallbackAttempted === true,
        })
        if (!fallbackApplied) {
          throw err
        }
      }
      return
    }
    if (decision.kind === 'redirect_waiting' && decision.paymentState.payUrl) {
      if (isMobileDevice()) {
        window.location.href = decision.paymentState.payUrl
        return
      }
      openWindow(decision.paymentState.payUrl)
    }
  } catch (err: unknown) {
    const apiErr = err as Record<string, unknown>
    if (apiErr.reason === 'TOO_MANY_PENDING') {
      const metadata = apiErr.metadata as Record<string, unknown> | undefined
      errorMessage.value = t('payment.errors.tooManyPending', { max: metadata?.max || '' })
      errorHintMessage.value = ''
    } else if (apiErr.reason === 'CANCEL_RATE_LIMITED') {
      errorMessage.value = t('payment.errors.cancelRateLimited')
      errorHintMessage.value = ''
    } else if (await attemptMobileQrFallback(err, {
      orderAmount,
      orderType,
      planId,
      paymentType: requestType,
      externalProduct: ninePlusProduct,
      attempted: options.mobileQrFallbackAttempted === true,
    })) {
      return
    } else {
      const handled = applyScenarioError(
        err,
        normalizeVisibleMethod(options.paymentType || selectedMethod.value) || selectedMethod.value,
      )
      if (!handled) {
        errorMessage.value = extractI18nErrorMessage(err, t, 'payment.errors', extractApiErrorMessage(err, t('payment.result.failed')))
        errorHintMessage.value = ''
      }
      if (handled) {
        return
      }
    }
    appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  } finally {
    submitting.value = false
  }
}

interface MobileQrFallbackContext {
  orderAmount: number
  orderType: OrderType
  planId?: number
  paymentType: string
  externalProduct?: NinePlusProduct | null
  attempted: boolean
}

function shouldFallbackToDesktopQr(err: unknown, paymentMethod: string, attempted: boolean): boolean {
  if (attempted || !isMobileDevice()) {
    return false
  }

  const normalizedMethod = normalizeVisibleMethod(paymentMethod) || paymentMethod
  const reason = typeof err === 'object' && err && 'reason' in err && typeof err.reason === 'string'
    ? err.reason
    : ''
  const message = err instanceof Error
    ? err.message
    : (typeof err === 'object' && err && 'message' in err && typeof err.message === 'string'
      ? err.message
      : '')
  const normalizedMessage = message.toLowerCase()

  if (normalizedMethod === 'wxpay') {
    return reason === 'WECHAT_H5_NOT_AUTHORIZED'
      || reason === 'WECHAT_PAYMENT_MP_NOT_CONFIGURED'
      || reason === 'WECHAT_JSAPI_FAILED'
      || reason === 'PAYMENT_GATEWAY_ERROR'
      || reason === 'UNHANDLED_PAYMENT_SCENARIO'
      || normalizedMessage.includes('weixinjsbridge is unavailable')
      || normalizedMessage.includes('wechat_jsapi_unavailable')
  }

  if (normalizedMethod === 'alipay') {
    return reason === 'PAYMENT_GATEWAY_ERROR' || reason === 'UNHANDLED_PAYMENT_SCENARIO'
  }

  return false
}

async function attemptMobileQrFallback(err: unknown, context: MobileQrFallbackContext): Promise<boolean> {
  if (!shouldFallbackToDesktopQr(err, context.paymentType, context.attempted)) {
    return false
  }

  try {
    const visibleMethod = normalizeVisibleMethod(context.paymentType) || context.paymentType
    const ninePlusProduct = visibleMethod === 'nineplus' ? context.externalProduct ?? effectiveNinePlusProduct.value : null
    const payload = buildCreateOrderPayload({
      amount: context.orderAmount,
      paymentType: visibleMethod,
      orderType: context.orderType,
      planId: context.planId,
      externalProductId: ninePlusProduct?.product_id,
      externalQuantity: ninePlusProduct ? 1 : undefined,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: false,
      isWechatBrowser: false,
    })
    const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
    const stripeMethod = visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const stripeRouteUrl = result.client_secret
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          client_secret: result.client_secret,
          method: stripeMethod,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType: context.orderType,
      isMobile: false,
      isWechatBrowser: false,
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
    })

    if (decision.kind !== 'qr_waiting' || !decision.paymentState.qrCode) {
      return false
    }

    errorMessage.value = ''
    errorHintMessage.value = ''
    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)
    appStore.showWarning(t('payment.errors.mobilePaymentFallbackToQr'))
    return true
  } catch {
    return false
  }
}

function applyScenarioError(err: unknown, paymentMethod: string): boolean {
  const descriptor = describePaymentScenarioError(err, {
    paymentMethod,
    isMobile: isMobileDevice(),
    isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
  })
  if (!descriptor) {
    errorMessage.value = ''
    errorHintMessage.value = ''
    return false
  }
  errorMessage.value = t(descriptor.messageKey)
  errorHintMessage.value = descriptor.hintKey ? t(descriptor.hintKey) : ''
  appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  return true
}

async function resumeWechatPaymentFromQuery() {
  const resume = parseWechatResumeRoute(route.query, checkout.value.plans, validAmount.value)
  if (!resume) {
    return
  }

  selectedMethod.value = resume.paymentType
  if (resume.orderType === 'balance' && resume.orderAmount > 0) {
    amount.value = resume.orderAmount
  }
  if (resume.orderType === 'subscription' && resume.planId) {
    activeTab.value = 'subscription'
    const plan = checkout.value.plans.find(candidate => candidate.id === resume.planId)
    if (plan) selectInternalPlan(plan)
  }

  await router.replace({ path: route.path, query: stripWechatResumeQuery(route.query) })

  if (resume.wechatResumeToken) {
    await createOrder(0, resume.orderType, resume.planId, {
      wechatResumeToken: resume.wechatResumeToken,
      paymentType: resume.paymentType,
      isResume: true,
    })
    return
  }

  if (resume.orderAmount > 0 && resume.openid) {
    await createOrder(resume.orderAmount, resume.orderType, resume.planId, {
      openid: resume.openid,
      paymentType: resume.paymentType,
      isResume: true,
    })
  }
}

onMounted(async () => {
  try {
    const res = await paymentAPI.getCheckoutInfo()
    checkout.value = res.data
    activeTab.value = desiredPurchaseMode()
    selectedMethod.value = firstAvailableMethod(
      activeTab.value === 'subscription' ? subscriptionMethodOptions.value : methodOptions.value,
    )
    if (typeof window !== 'undefined') {
      if (hasWechatResumeQuery(route.query)) {
        removeRecoverySnapshot()
      }
      const routeResumeToken = typeof route.query.resume_token === 'string'
        ? route.query.resume_token
        : typeof route.query.wechat_resume_token === 'string'
          ? route.query.wechat_resume_token
          : undefined
      const restored = readPaymentRecoverySnapshot(
        window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY),
        { resumeToken: routeResumeToken },
      )
      if (restored) {
        paymentState.value = restored
        paymentPhase.value = 'paying'
        const restoredMethod = normalizeVisibleMethod(restored.paymentType)
          || (visibleMethods.value[restored.paymentType] ? restored.paymentType : '')
        if (restoredMethod) {
          selectedMethod.value = restoredMethod
        }
      } else {
        removeRecoverySnapshot()
      }
    }
    await resumeWechatPaymentFromQuery()
    checkoutReady.value = true
    if (paymentPhase.value === 'select') {
      activeTab.value = desiredPurchaseMode()
      applyRenewalGroupFromQuery()
      ensureMethodForMode(activeTab.value)
      await syncActiveTabQuery(activeTab.value)
    }
  } catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally {
    checkoutReady.value = true
    loading.value = false
  }
  // Fetch active subscriptions (uses cache, non-blocking)
  subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
</script>

<style scoped>
.recharge-page-canvas {
  --color-brand-300: 165 180 252;
  --color-brand-400: 99 102 241;
  --color-brand-500: 79 70 229;
  --color-accent-400: 168 85 247;
  --color-content-brand: 67 56 202;
  --color-line-focus: 79 70 229;

  width: 100%;
  min-width: 0;
  min-height: calc(100vh - 4rem);
  overflow-x: clip;
  padding: clamp(1rem, 2.1vw, 2rem);
  background:
    radial-gradient(circle at 16% 0%, rgb(99 102 241 / 0.13), transparent 30%),
    radial-gradient(circle at 90% 8%, rgb(236 72 153 / 0.1), transparent 24%),
    linear-gradient(180deg, #f7f8ff 0%, #eef2ff 48%, #f8fafc 100%);
}

.recharge-page-content {
  min-width: 0;
}

:global(.dark) .recharge-page-canvas {
  background:
    radial-gradient(circle at 16% 0%, rgb(79 70 229 / 0.2), transparent 30%),
    radial-gradient(circle at 90% 8%, rgb(190 24 93 / 0.12), transparent 24%),
    linear-gradient(
      180deg,
      rgb(var(--color-surface-canvas)) 0%,
      rgb(var(--color-surface-subtle)) 100%
    );
}

@media (max-width: 767px) {
  .recharge-page-canvas {
    padding: 0.75rem;
  }
}
</style>
