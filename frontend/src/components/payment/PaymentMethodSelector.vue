<template>
  <div>
    <label class="mb-2 block text-xs font-semibold uppercase tracking-[1px] text-gray-400 dark:text-gray-500">
      {{ t('payment.paymentMethod') }}
    </label>
    <div class="grid grid-cols-2 gap-3 sm:flex">
      <button
        v-for="method in sortedMethods"
        :key="method.type"
        type="button"
        :disabled="!method.available"
        :class="[
          'relative flex h-[64px] flex-col items-center justify-center rounded-xl px-3 transition-all sm:flex-1',
          !method.available
            ? 'cursor-not-allowed bg-gray-50 opacity-50 shadow-[0_0_0_1px_#E5EAFF] dark:bg-dark-800/50 dark:shadow-[0_0_0_1px_rgba(255,255,255,0.06)]'
            : selected === method.type
              ? methodSelectedClass(method.type)
              : 'bg-white text-gray-700 shadow-[0_0_0_1px_#E5EAFF] hover:shadow-[0_0_0_1px_#0033FF] dark:bg-dark-800 dark:text-gray-200 dark:shadow-[0_0_0_1px_rgba(255,255,255,0.1)]',
        ]"
        @click="method.available && emit('select', method.type)"
      >
        <span class="flex items-center gap-2">
          <img :src="methodIcon(method.type)" :alt="methodLabel(method)" class="h-7 w-7 object-contain" />
          <span class="flex flex-col items-start leading-none">
            <span class="text-base font-semibold">{{ methodLabel(method) }}</span>
            <span
              v-if="method.fee_rate > 0"
              class="mt-0.5 text-[10px] tracking-wide text-gray-500 dark:text-dark-400"
            >
              {{ t('payment.fee') }} {{ method.fee_rate }}%
            </span>
          </span>
        </span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { METHOD_ORDER, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from './providerConfig'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import stripeIcon from '@/assets/icons/stripe.svg'
import airwallexIcon from '@/assets/icons/airwallex.svg'
import paymentIcon from '@/assets/icons/payment.svg'

export interface PaymentMethodOption {
  type: string
  display_name?: string
  fee_rate: number
  available: boolean
}

const props = defineProps<{
  methods: PaymentMethodOption[]
  selected: string
}>()

const emit = defineEmits<{
  select: [type: string]
}>()

const { t } = useI18n()

const METHOD_ICONS: Record<string, string> = {
  alipay: alipayIcon,
  wxpay: wxpayIcon,
  stripe: stripeIcon,
  airwallex: airwallexIcon,
  credit_card: paymentIcon,
}

const sortedMethods = computed(() => {
  const order: readonly string[] = METHOD_ORDER
  return [...props.methods].sort((a, b) => {
    const ai = order.indexOf(a.type)
    const bi = order.indexOf(b.type)
    return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
  })
})

function methodIcon(type: string): string {
  if (isBuiltInAlipayMethod(type)) return METHOD_ICONS.alipay
  if (isBuiltInWxpayMethod(type)) return METHOD_ICONS.wxpay
  if (type === 'airwallex') return METHOD_ICONS.airwallex
  return METHOD_ICONS[type] || paymentIcon
}

function methodLabel(method: PaymentMethodOption): string {
  return method.display_name || t(`payment.methods.${method.type}`, method.type)
}

function methodSelectedClass(type: string): string {
  if (isBuiltInAlipayMethod(type)) return 'bg-blue-50 text-gray-900 shadow-[0_0_0_1.5px_#02A9F1] dark:bg-blue-950 dark:text-gray-100'
  if (isBuiltInWxpayMethod(type)) return 'bg-green-50 text-gray-900 shadow-[0_0_0_1.5px_#09BB07] dark:bg-green-950 dark:text-gray-100'
  if (type === 'stripe') return 'bg-indigo-50 text-gray-900 shadow-[0_0_0_1.5px_#676BE5] dark:bg-indigo-950 dark:text-gray-100'
  if (type === 'airwallex') return 'bg-orange-50 text-gray-900 shadow-[0_0_0_1.5px_#FF6B3D] dark:bg-orange-950 dark:text-gray-100'
  return 'border border-primary-500 bg-[#E5EAFF] text-gray-900 shadow-[0_0_0_1.5px_#0033FF] dark:bg-primary-950 dark:text-gray-100'
}
</script>
