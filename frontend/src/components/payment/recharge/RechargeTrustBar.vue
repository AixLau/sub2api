<template>
  <section class="recharge-glass-card p-5 sm:p-6" :aria-label="t('payment.rechargeUi.trustGuarantees')">
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <div v-for="item in items" :key="item.title" class="flex items-center gap-3">
        <span class="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-status-info-soft text-content-brand ring-1 ring-line-subtle">
          <Icon :name="item.icon" size="md" />
        </span>
        <span class="min-w-0">
          <span class="block text-sm font-semibold text-content-primary">{{ item.title }}</span>
          <span class="mt-0.5 block text-xs leading-5 text-content-secondary">{{ item.description }}</span>
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
    title: t('payment.rechargeUi.securePayment'),
    description: t('payment.rechargeUi.securePaymentDesc'),
  },
  {
    icon: 'bolt' as const,
    title: t('payment.rechargeUi.realTimeArrival'),
    description: t('payment.rechargeUi.realTimeArrivalDesc'),
  },
  {
    icon: 'lock' as const,
    title: t('payment.rechargeUi.enterpriseEncryption'),
    description: t('payment.rechargeUi.enterpriseEncryptionDesc'),
  },
  {
    icon: 'chat' as const,
    title: t('payment.rechargeUi.support247'),
    description: t('payment.rechargeUi.support247Desc'),
  },
])
</script>
