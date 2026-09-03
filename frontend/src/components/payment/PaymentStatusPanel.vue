<template>
  <div class="mx-auto w-full max-w-md">
    <!-- ═══ Terminal States: show result, user clicks to return ═══ -->

    <!-- Success -->
    <template v-if="outcome === 'success'">
      <div class="rounded-2xl bg-surface-panel p-8 ring-1 ring-line-default">
        <div class="flex flex-col items-center text-center">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-status-success-soft">
            <Icon name="check" size="lg" class="text-status-success" />
          </div>
          <p class="mt-5 text-xl font-semibold tracking-tight text-gray-900 dark:text-white">
            {{ props.orderType === 'subscription' ? t('payment.result.subscriptionSuccess') : t('payment.result.success') }}
          </p>
          <div v-if="paidOrder" class="mt-6 w-full space-y-2.5 rounded-xl bg-surface-subtle p-5 text-sm">
            <div class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">#{{ paidOrder.id }}</span>
            </div>
            <div v-if="paidOrder.out_trade_no" class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderNo') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ paidOrder.out_trade_no }}</span>
            </div>
            <div class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.amount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ paidOrder.order_type === 'balance' ? creditedAmountSymbol + paidOrder.amount.toFixed(2) : formatGatewayAmount(paidOrder.amount, paidOrder.currency) }}</span>
            </div>
            <div class="flex justify-between">
              <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
              <span class="font-medium text-gray-900 dark:text-white">{{ formatGatewayAmount(paidOrder.pay_amount, paidOrder.currency) }}</span>
            </div>
          </div>
          <button class="mt-6 w-full rounded-full bg-primary-500 py-3 text-base font-medium text-white transition-colors hover:bg-primary-600 active:bg-primary-700" @click="handleDone">
            {{ t('common.confirm') }}
          </button>
        </div>
      </div>
    </template>

    <!-- Cancelled -->
    <template v-else-if="outcome === 'cancelled'">
      <div class="rounded-2xl bg-surface-panel p-8 ring-1 ring-line-default">
        <div class="flex flex-col items-center text-center">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700">
            <svg class="h-8 w-8 text-gray-400 dark:text-gray-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </div>
          <p class="mt-5 text-xl font-semibold tracking-tight text-gray-900 dark:text-white">{{ t('payment.qr.cancelled') }}</p>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.cancelledDesc') }}</p>
          <button class="mt-6 w-full rounded-full bg-primary-500 py-3 text-base font-medium text-white transition-colors hover:bg-primary-600 active:bg-primary-700" @click="handleDone">
            {{ t('common.confirm') }}
          </button>
        </div>
      </div>
    </template>

    <!-- Expired / Failed -->
    <template v-else-if="outcome === 'expired'">
      <div class="rounded-2xl bg-surface-panel p-8 ring-1 ring-line-default">
        <div class="flex flex-col items-center text-center">
          <div class="flex h-16 w-16 items-center justify-center rounded-full bg-status-warning-soft">
            <svg class="h-8 w-8 text-status-warning" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <p class="mt-5 text-xl font-semibold tracking-tight text-gray-900 dark:text-white">{{ t('payment.qr.expired') }}</p>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiredDesc') }}</p>
          <button class="mt-6 w-full rounded-full bg-primary-500 py-3 text-base font-medium text-white transition-colors hover:bg-primary-600 active:bg-primary-700" @click="handleDone">
            {{ t('common.confirm') }}
          </button>
        </div>
      </div>
    </template>

    <!-- ═══ Active States: QR or Popup waiting ═══ -->

    <!-- Mobile Alipay app handoff. The QR fallback stays hidden until launch timeout. -->
    <template v-else-if="isMobileAlipayDeepLink">
      <template v-if="!deepLinkFallbackVisible">
        <div class="card p-6">
          <div class="flex flex-col items-center space-y-4 py-4 text-center">
            <div
              v-if="deepLinkState === 'launching'"
              class="h-10 w-10 animate-spin rounded-full border-4 border-provider-alipay border-t-transparent"
            ></div>
            <div
              v-else
              class="flex h-12 w-12 items-center justify-center rounded-full bg-provider-alipay/10"
            >
              <Icon name="checkCircle" size="lg" class="text-provider-alipay" />
            </div>
            <p class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ deepLinkState === 'backgrounded' ? t('payment.qr.alipayContinueInApp') : t('payment.qr.alipayOpening') }}
            </p>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.alipayWaitingHint') }}</p>
            <button
              v-if="deepLinkState === 'backgrounded'"
              data-test="reopen-alipay"
              class="btn btn-alipay inline-flex items-center gap-2 text-sm"
              @click="reopenAlipay"
            >
              <Icon name="externalLink" size="sm" />
              {{ t('payment.qr.reopenAlipay') }}
            </button>
          </div>
        </div>
        <div class="card p-4 text-center">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiresIn') }}</p>
          <p class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</p>
          <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('payment.qr.waitingPayment') }}</p>
        </div>
      </template>
      <template v-else>
        <div data-test="alipay-qr-fallback" class="card p-6">
          <div class="flex flex-col items-center space-y-4">
            <div class="text-center">
              <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.qr.alipayFallbackTitle') }}</p>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.alipayFallbackHint') }}</p>
            </div>
            <div class="w-full space-y-2 border-y border-gray-100 py-3 text-sm dark:border-dark-600">
              <div class="flex items-start justify-between gap-4">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.payAmount') }}</span>
                <span class="font-semibold text-gray-900 dark:text-white">{{ displayPaymentAmount }}</span>
              </div>
              <div class="flex items-start justify-between gap-4">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderNo') }}</span>
                <span class="max-w-[70%] break-all text-right font-mono text-xs text-gray-900 dark:text-white">
                  {{ displayOrderNumber }}
                </span>
              </div>
              <div class="flex items-start justify-between gap-4">
                <span class="text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiresIn') }}</span>
                <span class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</span>
              </div>
            </div>
            <Lens>
              <div :class="['relative rounded-lg border-2 p-4', qrBorderClass]">
                <img v-if="qrDataUrl" :src="qrDataUrl" alt="" width="220" height="220" class="mx-auto block" />
                <div v-else class="h-[220px] w-[220px]"></div>
                <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
                  <span :class="['rounded-full p-2 shadow ring-2 ring-white', qrLogoBgClass]">
                    <img :src="qrLogoIcon" alt="" class="h-5 w-5 brightness-0 invert" />
                  </span>
                </div>
              </div>
            </Lens>
            <p class="text-center text-sm leading-6 text-gray-600 dark:text-gray-300">
              {{ t('payment.qr.alipaySaveAndScanHint') }}
            </p>
            <div class="grid w-full gap-2 sm:grid-cols-2">
              <button
                data-test="reopen-alipay"
                class="btn btn-alipay inline-flex items-center justify-center gap-2"
                @click="reopenAlipay"
              >
                <Icon name="externalLink" size="sm" />
                {{ t('payment.qr.reopenAlipay') }}
              </button>
              <button
                data-test="save-alipay-qr"
                class="btn btn-secondary inline-flex items-center justify-center gap-2"
                @click="saveQRCode"
              >
                <Icon name="download" size="sm" />
                {{ t('payment.qr.saveQRCode') }}
              </button>
            </div>
            <button class="btn btn-secondary w-full" @click="handleDone">
              {{ t('payment.result.backToRecharge') }}
            </button>
          </div>
        </div>
      </template>
    </template>

    <!-- QR Code Mode -->
    <template v-else-if="qrUrl">
      <div class="overflow-hidden rounded-2xl bg-surface-panel ring-1 ring-line-default">
        <!-- Brand header bar -->
        <div :class="['flex items-center gap-3 px-6 py-4', brandHeaderClass]">
          <span class="flex h-10 w-10 items-center justify-center rounded-full bg-white shadow-sm">
            <img :src="brandIcon" alt="" class="h-6 w-6" />
          </span>
          <div class="flex flex-col leading-tight text-white">
            <span class="text-base font-semibold">{{ scanTitle }}</span>
            <span class="text-xs text-white/80">{{ brandSubtitle }}</span>
          </div>
        </div>

        <div class="flex flex-col items-center px-6 py-7">
          <!-- Payable amount -->
          <p class="text-xs font-semibold uppercase tracking-[1px] text-gray-400 dark:text-gray-500">{{ t('payment.payableAmount') }}</p>
          <p :class="['mt-1 text-3xl font-semibold tracking-tight', brandTextClass]">{{ displayAmount }}</p>

          <!-- QR code framed with brand corners -->
          <div class="relative mt-6">
            <div :class="['absolute -left-1.5 -top-1.5 h-5 w-5 rounded-tl-md border-l-2 border-t-2', brandCornerClass]"></div>
            <div :class="['absolute -right-1.5 -top-1.5 h-5 w-5 rounded-tr-md border-r-2 border-t-2', brandCornerClass]"></div>
            <div :class="['absolute -bottom-1.5 -left-1.5 h-5 w-5 rounded-bl-md border-b-2 border-l-2', brandCornerClass]"></div>
            <div :class="['absolute -bottom-1.5 -right-1.5 h-5 w-5 rounded-br-md border-b-2 border-r-2', brandCornerClass]"></div>
            <Lens>
              <div class="relative rounded-xl bg-white p-3">
                <img
                  v-if="qrDataUrl"
                  data-test="payment-qr-image"
                  :src="qrDataUrl"
                  alt=""
                  width="220"
                  height="220"
                  class="block"
                />
                <div v-else class="h-[220px] w-[220px]"></div>
                <!-- Brand logo overlay -->
                <div v-if="isAlipay || isWxpay" class="pointer-events-none absolute inset-0 flex items-center justify-center">
                  <span class="flex h-11 w-11 items-center justify-center rounded-xl bg-white shadow-md ring-1 ring-black/5">
                    <img :src="brandIcon" alt="" class="h-7 w-7" />
                  </span>
                </div>
              </div>
            </Lens>
          </div>

          <p v-if="scanHint" class="mt-5 max-w-[16rem] text-center text-sm leading-relaxed text-gray-500 dark:text-gray-400">{{ scanHint }}</p>

          <!-- Countdown -->
          <div class="mt-5 flex items-center gap-2 rounded-full bg-surface-subtle px-4 py-2">
            <svg class="h-4 w-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiresIn') }}</span>
            <span class="text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</span>
          </div>

          <!-- Waiting indicator -->
          <div class="mt-4 flex items-center gap-2 text-sm text-gray-400 dark:text-gray-500">
            <span class="flex gap-1">
              <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-current [animation-delay:-0.3s]"></span>
              <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-current [animation-delay:-0.15s]"></span>
              <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-current"></span>
            </span>
            {{ t('payment.qr.waitingPayment') }}
          </div>
        </div>

        <!-- Actions -->
        <div class="flex flex-col gap-2 border-t border-gray-100 px-6 py-4 dark:border-dark-700">
          <button v-if="payUrl" class="rounded-full border-2 border-primary-500 py-2.5 text-sm font-medium text-content-brand transition-colors hover:bg-status-info-soft dark:border-primary-400" @click="reopenPopup">
            {{ t('payment.qr.openPayWindow') }}
          </button>
          <button class="rounded-full py-2.5 text-sm font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-200" :disabled="cancelling" @click="handleCancel">
            {{ cancelling ? t('common.processing') : t('payment.qr.cancelOrder') }}
          </button>
        </div>
      </div>
    </template>

    <!-- Waiting for Popup/Redirect Mode -->
    <template v-else>
      <div class="rounded-2xl bg-surface-panel p-8 ring-1 ring-line-default">
        <div class="flex flex-col items-center text-center">
          <div class="h-12 w-12 animate-spin rounded-full border-4 border-primary-100 border-t-primary-500"></div>
          <p class="mt-5 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.payInNewWindowHint') }}</p>
          <p class="mt-4 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</p>
          <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('payment.qr.waitingPayment') }}</p>
          <button v-if="payUrl" class="mt-6 w-full rounded-full border-2 border-primary-500 py-2.5 text-sm font-medium text-content-brand transition-colors hover:bg-status-info-soft dark:border-primary-400" @click="reopenPopup">
            {{ t('payment.qr.openPayWindow') }}
          </button>
          <button class="mt-2 w-full rounded-full py-2.5 text-sm font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-200" :disabled="cancelling" @click="handleCancel">
            {{ cancelling ? t('common.processing') : t('payment.qr.cancelOrder') }}
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePaymentStore } from '@/stores/payment'
import { useAppStore } from '@/stores'
import { paymentAPI } from '@/api/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { getPaymentPopupFeatures, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from '@/components/payment/providerConfig'
import { currencySymbol, formatPaymentAmount, normalizePaymentCurrency } from '@/components/payment/currency'
import type { PaymentOrder } from '@/types/payment'
import Icon from '@/components/icons/Icon.vue'
import Lens from '@/components/inspira/Lens.vue'
import QRCode from 'qrcode'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import paymentIcon from '@/assets/icons/payment.svg'
import {
  createAlipayDeepLinkLauncher,
  type AlipayDeepLinkLauncher,
  type AlipayDeepLinkState,
} from './alipayDeepLink'

const props = defineProps<{
  orderId: number
  amount?: number
  payAmount?: number
  qrCode: string
  expiresAt: string
  paymentType: string
  providerKey?: string
  payUrl?: string
  orderType?: string
  currency?: string
  outTradeNo?: string
  mobileAlipayDeepLink?: boolean
}>()

type PaymentOutcome = 'success' | 'cancelled' | 'expired'

const emit = defineEmits<{ done: []; success: []; settled: [outcome: PaymentOutcome] }>()

const i18n = useI18n()
const { t } = i18n
const paymentStore = usePaymentStore()
const appStore = useAppStore()

const qrUrl = ref('')
const qrDataUrl = ref('')
const remainingSeconds = ref(0)
const cancelling = ref(false)
const paidOrder = ref<PaymentOrder | null>(null)
const deepLinkState = ref<AlipayDeepLinkState>('idle')
const deepLinkFallbackVisible = ref(false)
const paymentCurrency = computed(() => normalizePaymentCurrency(props.currency))
const creditedAmountSymbol = currencySymbol('USD')
const localeCode = computed(() => {
  const raw = i18n.locale as unknown
  if (typeof raw === 'string') return raw
  if (raw && typeof raw === 'object' && 'value' in raw) {
    return String((raw as { value?: string }).value || '')
  }
  return undefined
})

// Terminal outcome: null = still active, 'success' | 'cancelled' | 'expired'
const outcome = ref<PaymentOutcome | null>(null)

let pollTimer: ReturnType<typeof setInterval> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null
let verifyAttempts = 0
let lastVerifyAt = 0
let alipayLauncher: AlipayDeepLinkLauncher | null = null

const VERIFY_RETRY_INTERVAL_MS = 15000
const NINEPLUS_VERIFY_RETRY_INTERVAL_MS = 3000
const VERIFY_RETRY_MAX_ATTEMPTS = 6

// Aggregators reuse the Alipay-style QR surface because the cashier URL should
// be scanned in the external wallet instead of opened as an in-app page.
const isAlipay = computed(() => isBuiltInAlipayMethod(props.paymentType) || props.paymentType === 'nineplus' || props.paymentType === 'haozpay')
const isWxpay = computed(() => isBuiltInWxpayMethod(props.paymentType))
const isHostedQrAggregator = computed(() => {
  const providerKey = String(props.providerKey || '').trim().toLowerCase()
  return props.paymentType === 'nineplus'
    || props.paymentType === 'haozpay'
    || providerKey === 'nineplus'
    || providerKey === 'haozpay'
})
const isMobileAlipayDeepLink = computed(() => props.mobileAlipayDeepLink === true && isBuiltInAlipayMethod(props.paymentType) && !!qrUrl.value)
const showQRCode = computed(() => !!qrUrl.value && (!isMobileAlipayDeepLink.value || deepLinkFallbackVisible.value))

const qrBorderClass = computed(() => {
  if (isAlipay.value) return 'border-provider-alipay-selection bg-provider-alipay/10 dark:border-provider-alipay-selection/70'
  if (isWxpay.value) return 'border-provider-wechat-selection bg-provider-wechat/10 dark:border-provider-wechat-selection/70'
  return 'border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800'
})

const qrLogoBgClass = computed(() => {
  if (isAlipay.value) return 'bg-provider-alipay'
  if (isWxpay.value) return 'bg-provider-wechat'
  return 'bg-gray-400'
})

const qrLogoIcon = computed(() => {
  if (isAlipay.value) return alipayIcon
  if (isWxpay.value) return wxpayIcon
  return paymentIcon
})

const brandIcon = computed(() => (isWxpay.value ? wxpayIcon : alipayIcon))

const brandHeaderClass = computed(() => {
  if (isAlipay.value) return 'bg-provider-alipay'
  if (isWxpay.value) return 'bg-provider-wechat'
  return 'bg-primary-500'
})

const brandTextClass = computed(() => {
  if (isAlipay.value) return 'text-provider-alipay'
  if (isWxpay.value) return 'text-provider-wechat'
  return 'text-content-brand'
})

const brandCornerClass = computed(() => {
  if (isAlipay.value) return 'border-provider-alipay'
  if (isWxpay.value) return 'border-provider-wechat'
  return 'border-primary-500'
})

const scanTitle = computed(() => {
  if (isAlipay.value) return t('payment.qr.scanAlipay')
  if (isWxpay.value) return t('payment.qr.scanWxpay')
  return t('payment.qr.scanToPay')
})

const brandSubtitle = computed(() => {
  if (isAlipay.value) return t('payment.methods.alipay')
  if (isWxpay.value) return t('payment.methods.wxpay')
  return ''
})

const scanHint = computed(() => {
  if (isAlipay.value) return t('payment.qr.scanAlipayHint')
  if (isWxpay.value) return t('payment.qr.scanWxpayHint')
  return ''
})

const displayAmount = computed(() => {
  if (!props.payAmount || props.payAmount <= 0) return ''
  return formatGatewayAmount(props.payAmount)
})

const countdownDisplay = computed(() => {
  const m = Math.floor(remainingSeconds.value / 60)
  const s = remainingSeconds.value % 60
  return m.toString().padStart(2, '0') + ':' + s.toString().padStart(2, '0')
})

const displayPaymentAmount = computed(() => formatGatewayAmount(props.payAmount || props.amount || 0))
const displayOrderNumber = computed(() => props.outTradeNo || `#${props.orderId}`)

function formatGatewayAmount(value: number, currency?: string | null): string {
  return formatPaymentAmount(value, currency || paymentCurrency.value, localeCode.value)
}

function isSuccessStatus(status: string | null | undefined): boolean {
  return status === 'COMPLETED' || status === 'PAID' || status === 'RECHARGING'
}

function reopenPopup() {
  if (props.payUrl) {
    const win = window.open(props.payUrl, 'paymentPopup', getPaymentPopupFeatures())
    if (!win || win.closed) {
      window.location.href = props.payUrl
    }
  }
}

function setOutcome(next: PaymentOutcome) {
  if (outcome.value === next) return
  outcome.value = next
  emit('settled', next)
}

async function renderQR() {
  await nextTick()
  if (!showQRCode.value || !qrUrl.value) {
    qrDataUrl.value = ''
    return
  }
  try {
    const canvas = document.createElement('canvas')
    await QRCode.toCanvas(canvas, qrUrl.value, {
      width: 220, margin: 1,
      errorCorrectionLevel: 'M',
    })
    qrDataUrl.value = canvas.toDataURL('image/png')
  } catch {
    qrDataUrl.value = ''
  }
}

function updateDeepLinkState(state: AlipayDeepLinkState) {
  deepLinkState.value = state
  if (state === 'fallback') {
    deepLinkFallbackVisible.value = true
    renderQR()
  } else if (state === 'backgrounded') {
    deepLinkFallbackVisible.value = false
  }
}

function reopenAlipay() {
  alipayLauncher?.launch()
}

function saveQRCode() {
  if (!qrDataUrl.value) return
  const link = document.createElement('a')
  link.href = qrDataUrl.value
  link.download = `alipay-${props.outTradeNo || props.orderId}.png`
  document.body.appendChild(link)
  link.click()
  link.remove()
}

async function tryRecoverPendingOrder(order: PaymentOrder): Promise<PaymentOrder> {
  const orderProviderKey = String(order.provider_key || props.providerKey || '').trim().toLowerCase()
  const orderPaymentType = String(order.payment_type || props.paymentType || '').trim().toLowerCase()
  const isOrderHostedAggregator = isHostedQrAggregator.value
    || orderProviderKey === 'nineplus'
    || orderProviderKey === 'haozpay'
    || orderPaymentType === 'nineplus'
    || orderPaymentType === 'haozpay'
  if (!isWxpay.value && !isOrderHostedAggregator && !isAlipay.value) return order
  const outTradeNo = String(order.out_trade_no || '').trim()
  if (!outTradeNo) return order
  const normalizedStatus = String(order.status || '').trim().toUpperCase()
  if (normalizedStatus !== 'PENDING') return order
  const now = Date.now()
  const reachedMaxAttempts = !isOrderHostedAggregator && verifyAttempts >= VERIFY_RETRY_MAX_ATTEMPTS
  const retryInterval = isOrderHostedAggregator ? NINEPLUS_VERIFY_RETRY_INTERVAL_MS : VERIFY_RETRY_INTERVAL_MS
  if (reachedMaxAttempts || now - lastVerifyAt < retryInterval) {
    return order
  }

  lastVerifyAt = now
  verifyAttempts += 1
  try {
    const result = await paymentAPI.verifyOrder(outTradeNo)
    return result.data ?? order
  } catch {
    return order
  }
}

let pollInFlight = false
async function pollStatus() {
  if (!props.orderId || outcome.value) return
  // 防重入：接口（含 verifyOrder 二次确认）响应慢于 3 秒轮询间隔时避免并发重叠请求。
  if (pollInFlight) return
  pollInFlight = true
  try {
    let order = await paymentStore.pollOrderStatus(props.orderId)
    if (!order) return
    // 已进入终态则不再处理迟到的响应。
    if (outcome.value) return
    order = await tryRecoverPendingOrder(order)
    if (outcome.value) return
    if (isSuccessStatus(order.status)) {
      cleanup()
      paidOrder.value = order
      setOutcome('success')
      emit('success')
    } else if (order.status === 'CANCELLED') {
      cleanup()
      setOutcome('cancelled')
    } else if (order.status === 'EXPIRED' || order.status === 'FAILED') {
      cleanup()
      setOutcome('expired')
    }
  } finally {
    pollInFlight = false
  }
}

function startCountdown(seconds: number) {
  remainingSeconds.value = Math.max(0, seconds)
  if (remainingSeconds.value <= 0) { setOutcome('expired'); return }
  countdownTimer = setInterval(() => {
    remainingSeconds.value--
    if (remainingSeconds.value <= 0) { setOutcome('expired'); cleanup() }
  }, 1000)
}

async function handleCancel() {
  if (!props.orderId || cancelling.value) return
  cancelling.value = true
  try {
    await paymentAPI.cancelOrder(props.orderId)
    cleanup()
    setOutcome('cancelled')
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    cancelling.value = false
  }
}

function handleDone() { cleanup(); emit('done') }

function cleanup() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
  alipayLauncher?.dispose()
  alipayLauncher = null
}

// Initialize on mount
qrUrl.value = props.qrCode
verifyAttempts = 0
lastVerifyAt = 0
let seconds = 30 * 60
if (props.expiresAt) {
  seconds = Math.floor((new Date(props.expiresAt).getTime() - Date.now()) / 1000)
}
startCountdown(seconds)
pollTimer = setInterval(pollStatus, 3000)
renderQR()

watch([() => qrUrl.value, showQRCode], () => renderQR())
onMounted(() => {
  if (!isMobileAlipayDeepLink.value) return
  alipayLauncher = createAlipayDeepLinkLauncher({
    qrCode: qrUrl.value,
    document,
    lifecycleTarget: window,
    userAgent: window.navigator.userAgent,
    assignLocation: (url) => window.location.assign(url),
    onStateChange: updateDeepLinkState,
  })
  alipayLauncher.launch()
})
onUnmounted(() => cleanup())
</script>
