<template>
  <span v-if="loading" class="text-sm text-gray-400" :title="t('admin.accounts.rpm.loading')">…</span>
  <span v-else-if="current == null" class="text-sm text-gray-400" :title="t('admin.accounts.rpm.unavailable')">—</span>
  <CapacityBadge v-else-if="showRpmLimit" :color-class="rpmClass" :tooltip="rpmTooltip" :current="current" :max="account.base_rpm!" :suffix="rpmStrategyTag" />
  <span v-else class="font-mono text-sm tabular-nums text-gray-700 dark:text-gray-300" :title="t('admin.accounts.rpm.hint')">{{ current }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountListItem } from '@/types'
import CapacityBadge from './CapacityBadge.vue'
const props = defineProps<{ account: AccountListItem; current: number | null; loading: boolean }>()
const { t } = useI18n()
const isAnthropicOAuthOrSetupToken = computed(() =>
  props.account.platform === 'anthropic' && ['oauth', 'setup-token'].includes(props.account.type)
)
// ====== RPM ======
const showRpmLimit = computed(() =>
  isAnthropicOAuthOrSetupToken.value &&
  props.account.base_rpm != null &&
  props.account.base_rpm > 0
)

const currentRPM = computed(() => props.current ?? 0)
const rpmStrategy = computed(() => props.account.rpm_strategy || 'tiered')
const rpmStrategyTag = computed(() => rpmStrategy.value === 'sticky_exempt' ? '[S]' : '[T]')

const rpmBuffer = computed(() => {
  const base = props.account.base_rpm || 0
  return props.account.rpm_sticky_buffer ?? (base > 0 ? Math.max(1, Math.floor(base / 5)) : 0)
})

const rpmClass = computed(() => {
  if (!showRpmLimit.value) return ''
  const current = currentRPM.value
  const base = props.account.base_rpm ?? 0
  const buffer = rpmBuffer.value
  if (rpmStrategy.value === 'tiered') {
    if (current >= base + buffer) return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
    if (current >= base) return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
  } else {
    if (current >= base) return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
  }
  if (current >= base * 0.8) return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
})

const rpmTooltip = computed(() => t('admin.accounts.rpm.limitHint'))

</script>
