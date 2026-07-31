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
            ? 'cursor-not-allowed bg-surface-subtle text-content-disabled opacity-50 ring-1 ring-line-subtle'
            : selected === method.type
              ? methodSelectedClass(method.type)
              : 'bg-surface-panel text-content-secondary ring-1 ring-line-default hover:ring-line-focus',
          { 'pm-selected-glow': method.available && selected === method.type },
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

// 选中态叠加发光边框(.pm-selected-glow,scoped CSS 伪元素实现),
// 与下方品牌色描边共存:描边表明"是哪家",光晕表明"当前选中"。
function methodSelectedClass(type: string): string {
  if (isBuiltInAlipayMethod(type)) return 'bg-provider-alipay/10 text-content-primary ring-2 ring-inset ring-provider-alipay-selection'
  if (isBuiltInWxpayMethod(type)) return 'bg-provider-wechat/10 text-content-primary ring-2 ring-inset ring-provider-wechat-selection'
  if (type === 'stripe') return 'bg-provider-stripe/10 text-content-primary ring-2 ring-inset ring-provider-stripe-selection'
  if (type === 'airwallex') return 'bg-provider-airwallex-selection/10 text-content-primary ring-2 ring-inset ring-provider-airwallex-selection'
  return 'border border-primary-500 bg-status-info-soft text-content-primary ring-1 ring-line-focus'
}
</script>

<style scoped>
/* 选中态发光边框:brand 光晕缓慢呼吸,叠加在既有品牌色描边之上(不替换)。
   用 ::after 伪元素承载 box-shadow,避免覆盖按钮本体的 shadow-[0_0_0_*] 描边。 */
.pm-selected-glow::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  pointer-events: none;
  box-shadow:
    0 0 8px 1px rgb(var(--color-brand-500) / 0.28),
    0 0 20px 5px rgb(var(--color-brand-500) / 0.14);
  animation: pm-glow-breathe 3s ease-in-out infinite;
}

/* 暗色下使用更亮的 brand-400 并略提强度 */
.dark .pm-selected-glow::after {
  box-shadow:
    0 0 8px 1px rgb(var(--color-brand-400) / 0.36),
    0 0 22px 6px rgb(var(--color-brand-400) / 0.18);
}

@keyframes pm-glow-breathe {
  0%,
  100% {
    opacity: 0.55;
  }
  50% {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .pm-selected-glow::after {
    animation: none;
    opacity: 0.8;
  }
}
</style>
