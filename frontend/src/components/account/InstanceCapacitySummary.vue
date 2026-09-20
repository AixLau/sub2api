<template>
  <div class="min-w-32 max-w-md space-y-1.5 text-xs" data-testid="instance-capacity">
    <div class="flex items-center gap-1.5">
      <span class="font-semibold text-gray-900 dark:text-white" data-testid="account-capacity-total">
        {{ principal.occupied }} / {{ principal.account_max_concurrency }}
      </span>
      <span class="text-gray-500 dark:text-gray-400">{{ t(`admin.accounts.instances.states.${principal.health_state}`) }}</span>
    </div>
    <div v-if="principal.overhang" class="text-amber-700 dark:text-amber-400">{{ t('admin.accounts.instances.shrinking', { count: principal.overhang }) }}</div>
    <div class="flex flex-wrap gap-1">
      <span v-for="instance in instances" :key="instance.id" class="rounded border border-gray-200 px-1.5 py-0.5 font-mono text-gray-700 dark:border-gray-700 dark:text-gray-300" :title="`${instance.name} · ${t(`admin.accounts.instances.states.${instance.state}`)}`">
        {{ instance.occupied }}/{{ instance.max_concurrency }}
      </span>
    </div>
    <details class="text-gray-600 dark:text-gray-400">
      <summary class="cursor-pointer">{{ t('admin.accounts.instances.count', { count: instances.length }) }}</summary>
      <div class="mt-1 space-y-1">
        <p>{{ t('admin.accounts.instances.accountLimit') }}: {{ principal.account_max_concurrency }}</p>
        <p>{{ t('admin.accounts.instances.configured') }}: {{ instanceLimitsFormula }}</p>
        <p>{{ t('admin.accounts.instances.effective') }}: {{ principal.effective_configured_capacity }}</p>
        <p>{{ t('admin.accounts.instances.available') }}: {{ principal.available_capacity }}</p>
        <p v-if="principal.unknown_occupied">{{ t('admin.accounts.instances.unknown', { count: principal.unknown_occupied }) }}</p>
        <p>{{ t('admin.accounts.instances.observed') }}: {{ new Date(principal.observed_at).toLocaleString() }}</p>
      </div>
    </details>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CredentialPrincipal } from '@/api/admin/credentialPrincipals'
const props = defineProps<{ principal: CredentialPrincipal }>()
const { t } = useI18n()
const instances = computed(() => props.principal.instances.filter(i => !i.archived_at))
const instanceLimitsFormula = computed(() => {
  const limits = instances.value.map(instance => instance.max_concurrency)
  if (limits.length === 0) return `0 = ${props.principal.configured_capacity}`
  return `${limits.join(' + ')} = ${props.principal.configured_capacity}`
})
</script>
