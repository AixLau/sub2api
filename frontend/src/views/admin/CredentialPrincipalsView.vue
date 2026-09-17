<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl space-y-6 p-4 sm:p-6">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h1 class="text-xl font-semibold">{{ t('admin.accounts.credentials.title') }}</h1>
        <button class="btn btn-secondary" :disabled="loading" @click="load">{{ t('common.refresh') }}</button>
      </div>
      <p class="rounded-xl bg-amber-50 p-4 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">{{ t('admin.accounts.credentials.notice') }}</p>
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <form class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700" @submit.prevent="submitImport">
        <h2 class="font-semibold">{{ t('admin.accounts.credentials.import') }}</h2>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="text-sm">Access token<input v-model="accessToken" class="input mt-1 w-full" type="password" autocomplete="off" required /></label>
          <label class="text-sm">Refresh token<input v-model="refreshToken" class="input mt-1 w-full" type="password" autocomplete="off" /></label>
        </div>
        <button class="btn btn-primary" :disabled="loading || !accessToken">{{ t('admin.accounts.credentials.verify') }}</button>
        <p v-if="importResult" class="break-all text-sm">{{ importResult }}</p>
      </form>
      <p v-if="!loading && !items.length" class="text-gray-500">{{ t('admin.accounts.credentials.empty') }}</p>
      <div class="grid gap-4 lg:grid-cols-2">
        <button v-for="principal in items" :key="principal.id" class="rounded-xl border border-gray-200 p-4 text-left dark:border-dark-700" @click="select(principal.id)">
          <div class="flex items-center justify-between gap-2"><strong>{{ principal.name }}</strong><span class="text-xs">{{ principal.admission_state }}</span></div>
          <p class="mt-3 text-sm">{{ t('admin.accounts.credentials.capacity') }}: {{ principal.occupied }} / {{ principal.requested_limit }}</p>
          <p class="mt-1 text-xs text-gray-500">{{ principal.verification_state }} · v{{ principal.config_version }}</p>
          <p v-if="principal.overhang" class="mt-2 text-sm text-amber-700">{{ t('admin.accounts.credentials.overhang', { count: principal.overhang }) }}</p>
        </button>
      </div>
      <section v-if="selected" class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <h2 class="text-lg font-semibold">{{ selected.name }}</h2>
        <div v-if="runtime" class="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <div>{{ t('admin.accounts.credentials.reserved') }}: {{ runtime.reserved }}</div>
          <div>{{ t('admin.accounts.credentials.running') }}: {{ runtime.running + runtime.dispatching }}</div>
          <div>{{ t('admin.accounts.credentials.orphaned') }}: {{ runtime.orphaned }}</div>
          <div>{{ t('admin.accounts.credentials.queued') }}: {{ runtime.queued }}</div>
        </div>
        <p v-if="runtime?.counter_mismatch" role="alert" class="text-red-600">{{ t('admin.accounts.credentials.mismatch') }}</p>
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="saveLimit">
          <label class="text-sm">{{ t('admin.accounts.credentials.limit') }}<input v-model.number="limit" class="input mt-1 block" type="number" min="0" required /></label>
          <button class="btn btn-secondary" :disabled="loading">{{ t('common.save') }}</button>
        </form>
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm"><thead><tr><th class="p-2">{{ t('admin.accounts.credentials.instance') }}</th><th class="p-2">{{ t('admin.accounts.credentials.weight') }}</th><th class="p-2">{{ t('admin.accounts.credentials.capacity') }}</th><th class="p-2">{{ t('admin.accounts.credentials.identity') }}</th></tr></thead>
            <tbody><tr v-for="instance in selected.instances" :key="instance.id" class="border-t border-gray-100 dark:border-dark-700"><td class="p-2">{{ instance.name }}<div class="text-xs text-gray-500">{{ instance.credential_state }} · v{{ instance.credential_version }}</div></td><td class="p-2">{{ instance.weight }}</td><td class="p-2">{{ instance.occupied }} / {{ instance.effective_hard_max }}</td><td class="p-2">{{ instance.identity_source }}</td></tr></tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getCredentialPrincipal, getCredentialRuntime, importCredential, listCredentialPrincipals, updateCredentialLimit, type CredentialPrincipal, type CredentialRuntime } from '@/api/admin/credentialPrincipals'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const items = ref<CredentialPrincipal[]>([])
const selected = ref<CredentialPrincipal | null>(null)
const runtime = ref<CredentialRuntime | null>(null)
const accessToken = ref('')
const refreshToken = ref('')
const importResult = ref('')
const limit = ref(0)
async function load() {
  loading.value = true
  error.value = ''
  try { items.value = (await listCredentialPrincipals()).items } catch { error.value = t('admin.accounts.credentials.loadError') } finally { loading.value = false }
}
async function select(id: number) {
  try { const [principal, state] = await Promise.all([getCredentialPrincipal(id), getCredentialRuntime(id)]); selected.value = principal; runtime.value = state; limit.value = principal.requested_limit } catch { error.value = t('admin.accounts.credentials.loadError') }
}
async function submitImport() {
  loading.value = true
  error.value = ''
  try { const result = await importCredential(accessToken.value, refreshToken.value, crypto.randomUUID()); importResult.value = `${result.verification_state} · ${result.import_id}` } catch { error.value = t('admin.accounts.credentials.importError') } finally { accessToken.value = ''; refreshToken.value = ''; loading.value = false }
}
async function saveLimit() {
  if (!selected.value || !Number.isInteger(limit.value) || limit.value < 0) return
  if (!window.confirm(t('admin.accounts.credentials.confirmLimit', { limit: limit.value, occupied: selected.value.occupied }))) return
  loading.value = true
  try { selected.value = await updateCredentialLimit(selected.value, limit.value); await load() } catch { error.value = t('admin.accounts.credentials.updateError') } finally { loading.value = false }
}
onMounted(load)
</script>
