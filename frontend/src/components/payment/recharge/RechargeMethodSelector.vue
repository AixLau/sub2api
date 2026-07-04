<template>
  <section
    class="recharge-glass-card p-5 sm:p-6"
    :aria-labelledby="showHeader ? 'recharge-method-title' : undefined"
    :aria-label="showHeader ? undefined : t('payment.rechargeUi.selectPaymentMethod')"
  >
    <div v-if="showHeader" class="mb-4">
      <p id="recharge-method-title" class="recharge-section-title">2. {{ t('payment.rechargeUi.selectPaymentMethod') }}</p>
      <p class="mt-1 text-sm text-slate-500">{{ t('payment.rechargeUi.paymentMethodHint') }}</p>
    </div>

    <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3" role="radiogroup" :aria-label="t('payment.paymentMethod')">
      <button
        v-for="method in sortedMethods"
        :key="method.type"
        type="button"
        :data-testid="`payment-method-${method.type}`"
        class="recharge-method-card"
        :class="{
          'recharge-method-card-selected': selected === method.type,
          'recharge-method-card-disabled': !method.available,
        }"
        role="radio"
        :aria-checked="selected === method.type"
        :disabled="!method.available"
        @click="method.available && emit('select', method.type)"
      >
        <span class="recharge-method-icon" :class="methodToneClass(method.type)">
          <img v-if="methodIcon(method.type)" :src="methodIcon(method.type)" alt="" class="h-6 w-6 object-contain" />
          <Icon v-else :name="fallbackIcon(method.type)" size="md" />
        </span>
        <span class="recharge-method-content">
          <span class="recharge-method-title-row">
            <span class="recharge-method-name">{{ methodLabel(method.type) }}</span>
            <span
              v-if="isRecommended(method.type)"
              class="rounded-full bg-blue-100 px-2 py-0.5 text-[10px] font-semibold text-blue-700"
            >
              {{ t('payment.rechargeUi.recommended') }}
            </span>
          </span>
          <span v-if="!method.available || method.fee_rate > 0" class="recharge-method-meta">
            <template v-if="!method.available">{{ t('payment.rechargeUi.methodUnavailable') }}</template>
            <template v-else>{{ t('payment.fee') }} {{ method.fee_rate }}%</template>
          </span>
        </span>
        <span
          v-if="selected === method.type"
          class="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-blue-600 text-white"
          aria-hidden="true"
        >
          <Icon name="check" size="xs" :stroke-width="2.4" />
        </span>
        <Icon v-else name="chevronRight" size="sm" class="shrink-0 text-slate-400" aria-hidden="true" />
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import { METHOD_ORDER } from '@/components/payment/providerConfig'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import stripeIcon from '@/assets/icons/stripe.svg'
import airwallexIcon from '@/assets/icons/airwallex.svg'

const props = withDefaults(defineProps<{
  methods: PaymentMethodOption[]
  selected: string
  showHeader?: boolean
}>(), {
  showHeader: true,
})

const emit = defineEmits<{
  select: [type: string]
}>()

const { t } = useI18n()

const METHOD_ICONS: Record<string, string> = {
  alipay: alipayIcon,
  nineplus: alipayIcon,
  haozpay: alipayIcon,
  wxpay: wxpayIcon,
  stripe: stripeIcon,
  airwallex: airwallexIcon,
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
  if (type.includes('alipay')) return METHOD_ICONS.alipay
  if (type.includes('wxpay')) return METHOD_ICONS.wxpay
  return METHOD_ICONS[type] || ''
}

function fallbackIcon(type: string): 'creditCard' | 'server' | 'shield' {
  if (type === 'stripe' || type === 'airwallex') return 'creditCard'
  if (type === 'haozpay') return 'server'
  return 'shield'
}

function methodLabel(type: string): string {
  if (type === 'stripe') return t('payment.rechargeUi.enterpriseCard')
  if (type === 'airwallex') return t('payment.methods.airwallex')
  return t(`payment.methods.${type}`, type)
}

function methodToneClass(type: string): string {
  if (type.includes('wxpay')) return 'recharge-method-icon-wxpay'
  if (type === 'stripe') return 'recharge-method-icon-stripe'
  if (type === 'airwallex') return 'recharge-method-icon-airwallex'
  return 'recharge-method-icon-alipay'
}

function isRecommended(type: string): boolean {
  return type === props.methods[0]?.type || type === 'alipay' || type === 'nineplus'
}
</script>
