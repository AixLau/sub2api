<template>
  <div class="min-w-40 max-w-md space-y-1 text-xs" data-testid="instance-capacity">
    <div class="font-medium">{{ principal.occupied }}/{{ principal.account_max_concurrency }} · {{ t(`admin.accounts.instances.states.${principal.health_state}`) }}</div>
    <div v-if="principal.overhang" class="text-amber-700 dark:text-amber-400">{{ t('admin.accounts.instances.shrinking', { count: principal.overhang }) }}</div>
    <div class="flex flex-wrap gap-1">
      <span v-for="instance in visible" :key="instance.id" class="rounded border border-gray-200 px-1.5 py-1 dark:border-gray-700" :title="t(`admin.accounts.instances.states.${instance.state}`)">
        {{ instance.name }} {{ instance.occupied }}/{{ instance.max_concurrency }}
        <span class="text-gray-500">· {{ t(`admin.accounts.instances.states.${instance.state}`) }}</span>
      </span>
    </div>
    <details class="text-gray-600 dark:text-gray-400">
      <summary class="cursor-pointer">{{ t('admin.accounts.instances.count', { count: instances.length }) }}</summary>
      <div class="mt-1 flex flex-wrap gap-1">
        <span v-for="instance in instances.slice(3)" :key="instance.id" class="rounded border px-1.5 py-1">{{ instance.name }} {{ instance.occupied }}/{{ instance.max_concurrency }} · {{ t(`admin.accounts.instances.states.${instance.state}`) }}</span>
      </div>
      <p>{{ t('admin.accounts.instances.configured') }}: {{ principal.configured_capacity }}</p>
      <p>{{ t('admin.accounts.instances.effective') }}: {{ principal.effective_configured_capacity }}</p>
      <p>{{ t('admin.accounts.instances.available') }}: {{ principal.available_capacity }}</p>
      <p v-if="principal.unknown_occupied">{{ t('admin.accounts.instances.unknown', { count: principal.unknown_occupied }) }}</p>
      <p>{{ t('admin.accounts.instances.observed') }}: {{ new Date(principal.observed_at).toLocaleString() }}</p>
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
const visible = computed(() => instances.value.slice(0, 3))
</script>
