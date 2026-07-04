<template>
  <header class="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
    <div>
      <div class="flex items-center gap-2">
        <h1 class="text-2xl font-semibold tracking-normal text-slate-950 sm:text-3xl">
          {{ t('payment.rechargeUi.title') }}
        </h1>
        <span class="flex h-6 w-6 items-center justify-center rounded-full bg-blue-100 text-blue-700">
          <Icon name="shield" size="sm" :stroke-width="2" />
        </span>
      </div>
      <p class="mt-2 text-sm leading-6 text-slate-600">
        {{ t('payment.rechargeUi.subtitle') }}
      </p>
    </div>
    <div
      v-if="showAccountPill"
      data-testid="recharge-header-account-pill"
      class="recharge-top-pill"
    >
      <span class="flex h-7 w-7 items-center justify-center rounded-lg bg-blue-600/15 text-xs font-bold text-blue-700">
        {{ initials }}
      </span>
      <span class="max-w-[180px] truncate text-sm font-semibold text-slate-800">{{ accountName }}</span>
      <Icon name="chevronDown" size="sm" class="text-slate-500" />
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  accountName: string
  showAccountPill?: boolean
}>(), {
  showAccountPill: true,
})

const { t } = useI18n()
const initials = computed(() => {
  const source = props.accountName.trim() || 'U'
  return source.slice(0, 2).toUpperCase()
})
</script>
