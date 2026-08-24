<template>
  <section class="recharge-glass-card recharge-trust-bar" :aria-label="t('payment.rechargeUi.trustGuarantees')">
    <div class="recharge-trust-grid grid sm:grid-cols-2 lg:grid-cols-4">
      <div v-for="item in items" :key="item.title" class="recharge-trust-item flex items-center">
        <span class="recharge-trust-icon" :class="`recharge-trust-icon--${item.tone}`">
          <Icon :name="item.icon" size="lg" :stroke-width="1.8" />
        </span>
        <span class="recharge-trust-copy min-w-0">
          <span class="recharge-trust-title">{{ item.title }}</span>
          <span class="recharge-trust-description">{{ item.description }}</span>
        </span>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

const items = computed(() => [
  {
    icon: 'shield' as const,
    tone: 'violet' as const,
    title: t('payment.rechargeUi.securePayment'),
    description: t('payment.rechargeUi.securePaymentDesc'),
  },
  {
    icon: 'bolt' as const,
    tone: 'violet' as const,
    title: t('payment.rechargeUi.realTimeArrival'),
    description: t('payment.rechargeUi.realTimeArrivalDesc'),
  },
  {
    icon: 'lock' as const,
    tone: 'green' as const,
    title: t('payment.rechargeUi.enterpriseEncryption'),
    description: t('payment.rechargeUi.enterpriseEncryptionDesc'),
  },
  {
    icon: 'chat' as const,
    tone: 'violet' as const,
    title: t('payment.rechargeUi.support247'),
    description: t('payment.rechargeUi.support247Desc'),
  },
])
</script>
