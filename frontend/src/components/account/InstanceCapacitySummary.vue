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
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CredentialPrincipal } from '@/api/admin/credentialPrincipals'
const props = defineProps<{ principal: CredentialPrincipal }>()
const { t } = useI18n()
const instances = computed(() => props.principal.instances.filter(i => !i.archived_at))
</script>
