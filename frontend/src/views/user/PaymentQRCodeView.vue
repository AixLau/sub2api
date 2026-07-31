<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-md py-8">
      <div class="overflow-hidden rounded-2xl bg-surface-panel ring-1 ring-line-default">
        <!-- Brand header bar -->
        <div :class="['flex items-center gap-3 px-6 py-4', brandHeaderClass]">
          <span class="flex h-10 w-10 items-center justify-center rounded-full bg-white shadow-sm">
            <img :src="brandIcon" alt="" class="h-6 w-6" />
          </span>
          <div class="flex flex-col leading-tight text-white">
            <span class="text-base font-semibold">{{ qrUrl ? scanTitle : t('payment.qr.payInNewWindow') }}</span>
            <span v-if="brandSubtitle" class="text-xs text-white/80">{{ brandSubtitle }}</span>
          </div>
        </div>

        <div class="flex flex-col items-center px-6 py-7">
          <!-- Expired state -->
          <template v-if="expired">
            <div class="flex h-16 w-16 items-center justify-center rounded-full bg-status-warning-soft">
              <svg class="h-8 w-8 text-status-warning" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
            <p class="mt-5 text-xl font-semibold tracking-tight text-gray-900 dark:text-white">{{ t('payment.qr.expired') }}</p>
            <button class="mt-6 w-full rounded-full bg-primary-500 py-3 text-base font-medium text-white transition-colors hover:bg-primary-600 active:bg-primary-700" @click="router.push('/purchase')">
              {{ t('payment.result.backToRecharge') }}
            </button>
          </template>

          <!-- QR code mode -->
          <template v-else-if="qrUrl">
            <div class="relative mt-1">
              <div :class="['absolute -left-1.5 -top-1.5 h-5 w-5 rounded-tl-md border-l-2 border-t-2', brandCornerClass]"></div>
              <div :class="['absolute -right-1.5 -top-1.5 h-5 w-5 rounded-tr-md border-r-2 border-t-2', brandCornerClass]"></div>
              <div :class="['absolute -bottom-1.5 -left-1.5 h-5 w-5 rounded-bl-md border-b-2 border-l-2', brandCornerClass]"></div>
              <div :class="['absolute -bottom-1.5 -right-1.5 h-5 w-5 rounded-br-md border-b-2 border-r-2', brandCornerClass]"></div>
              <div class="rounded-xl bg-white p-3">
                <canvas ref="qrCanvas" class="block"></canvas>
              </div>
            </div>
            <p v-if="scanHint" class="mt-5 max-w-[16rem] text-center text-sm leading-relaxed text-gray-500 dark:text-gray-400">{{ scanHint }}</p>
            <div class="mt-5 flex items-center gap-2 rounded-full bg-surface-subtle px-4 py-2">
              <svg class="h-4 w-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.expiresIn') }}</span>
              <span class="text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</span>
            </div>
            <div class="mt-4 flex items-center gap-2 text-sm text-gray-400 dark:text-gray-500">
              <span class="flex gap-1">
                <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-current [animation-delay:-0.3s]"></span>
                <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-current [animation-delay:-0.15s]"></span>
                <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-current"></span>
              </span>
              {{ t('payment.qr.waitingPayment') }}
            </div>
          </template>

          <!-- Pay in new window -->
          <template v-else>
            <div class="h-12 w-12 animate-spin rounded-full border-4 border-primary-100 border-t-primary-500"></div>
            <p class="mt-5 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.qr.payInNewWindowHint') }}</p>
            <p class="mt-4 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</p>
            <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('payment.qr.waitingPayment') }}</p>
            <a v-if="payUrl" :href="payUrl" target="_blank" rel="noopener noreferrer"
              class="mt-6 w-full rounded-full bg-primary-500 py-3 text-center text-base font-medium text-white transition-colors hover:bg-primary-600 active:bg-primary-700">
              {{ t('payment.qr.openPayWindow') }}
            </a>
          </template>
        </div>

        <!-- Cancel -->
        <div v-if="!expired && orderId" class="border-t border-gray-100 px-6 py-4 dark:border-dark-700">
          <button class="w-full rounded-full py-2.5 text-sm font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-200" :disabled="cancelling" @click="handleCancel">
            {{ cancelling ? t('common.processing') : t('payment.qr.cancelOrder') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { usePaymentStore } from '@/stores/payment'
import { paymentAPI } from '@/api/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores'
import { isBuiltInAlipayMethod, isBuiltInWxpayMethod } from '@/components/payment/providerConfig'
import QRCode from 'qrcode'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const paymentStore = usePaymentStore()
const appStore = useAppStore()

const qrCanvas = ref<HTMLCanvasElement | null>(null)
const qrUrl = ref('')
const payUrl = ref('')
const orderId = ref(0)
const remainingSeconds = ref(0)
const expired = ref(false)
const cancelling = ref(false)
const paymentType = ref('')

let pollTimer: ReturnType<typeof setInterval> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null

const countdownDisplay = computed(() => {
  const m = Math.floor(remainingSeconds.value / 60)
  const s = remainingSeconds.value % 60
  return m.toString().padStart(2, '0') + ':' + s.toString().padStart(2, '0')
})

const isAlipay = computed(() => isBuiltInAlipayMethod(paymentType.value) || paymentType.value === 'nineplus')
const isWxpay = computed(() => isBuiltInWxpayMethod(paymentType.value))

const brandIcon = computed(() => (isWxpay.value ? wxpayIcon : alipayIcon))

const brandHeaderClass = computed(() => {
  if (isAlipay.value) return 'bg-provider-alipay'
  if (isWxpay.value) return 'bg-provider-wechat'
  return 'bg-primary-500'
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

async function renderQR() {
  await nextTick()
  if (!qrCanvas.value || !qrUrl.value) return
  await QRCode.toCanvas(qrCanvas.value, qrUrl.value, {
    width: 220,
    margin: 1,
    errorCorrectionLevel: 'M',
  })
}

let pollInFlight = false
async function pollStatus() {
  if (!orderId.value) return
  // 防重入：接口响应慢于 3 秒轮询间隔时避免并发重叠请求与重复跳转。
  if (pollInFlight) return
  pollInFlight = true
  try {
    const order = await paymentStore.pollOrderStatus(orderId.value)
    if (!order) return
    // 定时器已被 cleanup 清除时不再执行终态跳转（响应可能在 cleanup 后才回来）。
    if (!pollTimer) return
    if (order.status === 'COMPLETED' || order.status === 'PAID') {
      cleanup()
      router.push({ path: '/payment/result', query: { order_id: String(orderId.value), status: 'success' } })
    } else if (order.status === 'EXPIRED' || order.status === 'CANCELLED' || order.status === 'FAILED') {
      cleanup()
      expired.value = true
    }
  } finally {
    pollInFlight = false
  }
}

function startCountdown(seconds: number) {
  remainingSeconds.value = Math.max(0, seconds)
  if (remainingSeconds.value <= 0) {
    expired.value = true
    return
  }
  countdownTimer = setInterval(() => {
    remainingSeconds.value--
    if (remainingSeconds.value <= 0) {
      expired.value = true
      cleanup()
    }
  }, 1000)
}

async function handleCancel() {
  if (!orderId.value || cancelling.value) return
  cancelling.value = true
  try {
    await paymentAPI.cancelOrder(orderId.value)
    cleanup()
    router.push('/purchase')
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    cancelling.value = false
  }
}

function cleanup() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

watch(qrUrl, () => renderQR())

onMounted(() => {
  orderId.value = Number(route.query.order_id) || 0
  qrUrl.value = String(route.query.qr || '')
  payUrl.value = String(route.query.pay_url || '')
  paymentType.value = String(route.query.payment_type || '')

  // Calculate countdown from expiresAt
  const expiresAtStr = String(route.query.expires_at || '')
  let seconds = 30 * 60 // fallback: 30 minutes
  if (expiresAtStr) {
    const expiresAt = new Date(expiresAtStr)
    const now = new Date()
    seconds = Math.floor((expiresAt.getTime() - now.getTime()) / 1000)
  }
  startCountdown(seconds)
  pollTimer = setInterval(pollStatus, 3000)
  renderQR()
})

onUnmounted(() => cleanup())
</script>
