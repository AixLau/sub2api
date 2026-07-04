<template>
  <div
    v-if="tabs.length > 1"
    class="recharge-mode-tabs"
    role="tablist"
    :aria-label="t('payment.rechargeUi.activeMode')"
  >
    <button
      v-for="tab in tabs"
      :key="tab.key"
      type="button"
      class="recharge-mode-tab"
      :class="{ 'recharge-mode-tab-active': modelValue === tab.key }"
      role="tab"
      :aria-selected="modelValue === tab.key"
      :aria-pressed="modelValue === tab.key"
      :data-testid="`purchase-mode-${tab.key}`"
      @click="emit('update:modelValue', tab.key)"
    >
      {{ tab.label }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

type PurchaseMode = 'recharge' | 'subscription'

defineProps<{
  modelValue: PurchaseMode
  tabs: Array<{
    key: PurchaseMode
    label: string
  }>
}>()

const emit = defineEmits<{
  'update:modelValue': [value: PurchaseMode]
}>()

const { t } = useI18n()
</script>
