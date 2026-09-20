<template>
  <TotpStepUpDialog :controller="stepUp" />
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.instances.deviceManagement')"
    width="extra-wide"
    :auto-focus="false"
    @close="emit('close')"
  >
    <div v-if="account" class="space-y-5" data-testid="device-management-modal">
      <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h4 class="text-base font-semibold text-gray-900 dark:text-white">{{ account.name }}</h4>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ accountStatusLabel }}</p>
          </div>
          <button class="btn btn-secondary btn-sm" :disabled="busy" :title="t('common.refresh')" @click="load">
            <Icon name="refresh" size="sm" :class="busy ? 'animate-spin' : ''" />
            <span>{{ t('common.refresh') }}</span>
          </button>
        </div>
        <div class="mt-3 grid gap-2 text-xs text-gray-500 dark:text-gray-400 sm:grid-cols-2">
          <span>{{ t('admin.accounts.instances.createdAt') }}: {{ formatDate(account.created_at) }}</span>
          <span>{{ t('admin.accounts.instances.updatedAt') }}: {{ formatDate(account.updated_at) }}</span>
        </div>
      </section>

      <section v-if="principal" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex items-center justify-between gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.accounts.instances.accountLimit') }}</span>
            <button class="rounded p-1 text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700" :title="t('common.edit')" @click="editingLimit = !editingLimit">
              <Icon name="edit" size="xs" />
            </button>
          </div>
          <div v-if="editingLimit" class="mt-2 flex gap-2">
            <input v-model.number="limitDraft" class="input h-8 min-w-0" type="number" min="0" max="2147483647" step="1" />
            <button class="btn btn-primary btn-sm" :disabled="busy" @click="saveLimit">{{ t('common.save') }}</button>
          </div>
          <div v-else class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ principal.account_max_concurrency }}</div>
        </div>
        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.instances.configuredCapacity') }}</div>
          <div class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ principal.configured_capacity }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ activeInstances.map(i => i.max_concurrency).join(' + ') || '-' }}</div>
        </div>
        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.instances.currentUsage') }}</div>
          <div class="mt-1 text-2xl font-semibold text-gray-900 dark:text-white">{{ principal.occupied }} / {{ effectiveCapacity }}</div>
          <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700"><div class="h-full rounded-full bg-primary-500" :style="{ width: `${usagePercent}%` }" /></div>
        </div>
        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.instances.availableBalance') }}</div>
          <div class="mt-1 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">{{ Math.max(0, principal.available_capacity) }}</div>
        </div>
      </section>

      <p v-if="error" role="alert" class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>

      <div class="border-b border-gray-200 dark:border-dark-600">
        <div class="flex gap-5" role="tablist">
          <button class="border-b-2 border-primary-500 px-1 pb-2 text-sm font-medium text-primary-600" role="tab" aria-selected="true">{{ t('admin.accounts.instances.deviceList') }}</button>
        </div>
      </div>

      <section v-if="activeTab === 'devices'" class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.accounts.instances.deviceList') }}</h4>
          <button class="btn btn-primary btn-sm" :disabled="busy" @click="openAdd"><Icon name="plus" size="sm" /> {{ t('admin.accounts.instances.addDevice') }}</button>
        </div>
        <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-600">
            <thead class="bg-gray-50 dark:bg-dark-800"><tr><th v-for="heading in headings" :key="heading" class="whitespace-nowrap px-3 py-2 text-left text-xs font-semibold text-gray-500 dark:text-gray-400">{{ heading }}</th></tr></thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-600">
              <tr v-for="instance in activeInstances" :key="instance.id" class="align-top">
                <td class="px-3 py-3 text-gray-500">#{{ instance.id }}</td>
                <td class="px-3 py-3"><div class="font-medium text-gray-900 dark:text-white">{{ instance.name }}</div><div class="text-xs text-gray-500">{{ instance.identity_source || '-' }}</div></td>
                <td class="px-3 py-3"><span :class="statusClass(instance.state)">{{ stateLabel(instance.state) }}</span></td>
                <td class="px-3 py-3"><div class="font-medium">{{ instance.occupied }} / {{ instance.max_concurrency }}</div><div class="mt-1 h-1 w-20 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700"><div class="h-full bg-primary-500" :style="{ width: `${Math.min(100, instance.max_concurrency ? instance.occupied / instance.max_concurrency * 100 : 0)}%` }" /></div><div v-if="instance.occupied > instance.max_concurrency" class="mt-1 text-xs text-amber-600">{{ t('admin.accounts.instances.shrinking', { count: instance.occupied - instance.max_concurrency }) }}</div></td>
                <td class="px-3 py-3 text-xs text-gray-500">{{ formatDate(instance.credential_version ? principal?.observed_at : undefined) }}</td>
                <td class="px-3 py-3"><div class="flex flex-wrap gap-1"><button class="btn btn-secondary btn-sm" :disabled="busy" @click="editInstance(instance)">{{ t('common.edit') }}</button><button class="btn btn-secondary btn-sm" :disabled="busy" @click="reauthorize(instance.id)">{{ t('admin.accounts.instances.reauthorize') }}</button><button class="btn btn-secondary btn-sm" :disabled="busy" @click="toggleDrain(instance)">{{ t(instance.admin_state === 'ACTIVE' ? 'admin.accounts.instances.drain' : 'admin.accounts.instances.resume') }}</button><button class="btn btn-secondary btn-sm" :disabled="busy" @click="refreshInstance(instance.id)">{{ t('admin.accounts.instances.refreshCredential') }}</button><button v-if="instance.admin_state !== 'ACTIVE' && instance.occupied === 0 && instance.active_bindings === 0" class="btn btn-secondary btn-sm text-red-600" :disabled="busy" @click="archiveInstance(instance.id)">{{ t('admin.accounts.instances.remove') }}</button></div></td>
              </tr>
              <tr v-if="!activeInstances.length"><td colspan="6" class="px-3 py-8 text-center text-sm text-gray-500">{{ t('admin.accounts.instances.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>

    </div>

    <BaseDialog v-if="editTarget" :show="true" :title="t('admin.accounts.instances.editDevice')" width="narrow" :z-index="70" @close="editTarget = null">
      <form class="space-y-4" @submit.prevent="saveInstance"><label class="block text-sm">{{ t('admin.accounts.instances.name') }}<input v-model="editDraft.name" class="input mt-1 w-full" maxlength="100" required /></label><label class="block text-sm">{{ t('admin.accounts.instances.maxConcurrency') }}<input v-model.number="editDraft.max_concurrency" class="input mt-1 w-full" type="number" min="0" max="2147483647" step="1" required /></label><div class="flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="editTarget = null">{{ t('common.cancel') }}</button><button class="btn btn-primary" :disabled="busy">{{ t('common.save') }}</button></div></form>
    </BaseDialog>
    <BaseDialog v-if="showAdd && principal" :show="true" :title="t(reauthorizeId ? 'admin.accounts.instances.reauthorize' : 'admin.accounts.instances.addDevice')" width="wide" :z-index="65" @close="showAdd = false"><OpenAIInstanceForm :principal="principal" :account-name="account?.name || principal.name" :account-limit="principal.account_max_concurrency" :initial-max="Math.max(1, principal.account_max_concurrency)" :proxy-id="principal.proxy_id" :replace-instance-id="reauthorizeId" @completed="handleAdded" /></BaseDialog>
    <ConfirmDialog v-if="archiveTarget" :show="true" :title="t('admin.accounts.instances.archiveDevice')" :message="t('admin.accounts.instances.archiveConfirm', { name: archiveTarget.name })" :confirm-text="t('admin.accounts.instances.remove')" :cancel-text="t('common.cancel')" :danger="true" @confirm="confirmArchive" @cancel="archiveTarget = null" />
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import OpenAIInstanceForm from './OpenAIInstanceForm.vue'
import { useStepUp, isStepUpCancelled } from '@/composables/useStepUp'
import { formatDateTime } from '@/utils/format'
import type { Account } from '@/types'
import { credentialErrorReason, getCredentialPrincipal, saveCredentialConfiguration, controlCredentialInstance, refreshCredentialInstance, type CredentialPrincipal, type CredentialInstance } from '@/api/admin/credentialPrincipals'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: []; 'instances-updated': []; updated: [principal: CredentialPrincipal] }>()
const { t } = useI18n()
const stepUp = useStepUp()
const principal = ref<CredentialPrincipal>()
const busy = ref(false)
const error = ref('')
const activeTab = ref<'devices' | 'runtime' | 'advanced'>('devices')
const editingLimit = ref(false)
const limitDraft = ref(0)
const showAdd = ref(false)
const editTarget = ref<CredentialInstance | null>(null)
const archiveTarget = ref<CredentialInstance | null>(null)
const editDraft = ref({ name: '', max_concurrency: 0 })
const headings = computed(() => [t('admin.accounts.instances.number'), t('admin.accounts.instances.deviceName'), t('admin.accounts.instances.status'), t('admin.accounts.instances.maxConcurrency'), t('admin.accounts.instances.lastRefresh'), t('common.actions')])
const activeInstances = computed(() => (principal.value?.instances || []).filter(instance => !instance.archived_at))
const effectiveCapacity = computed(() => Math.min(principal.value?.account_max_concurrency || 0, principal.value?.effective_configured_capacity || principal.value?.configured_capacity || 0))
const usagePercent = computed(() => effectiveCapacity.value > 0 ? Math.min(100, (principal.value?.occupied || 0) / effectiveCapacity.value * 100) : 0)
const accountStatusLabel = computed(() => props.account?.principal ? stateLabel(props.account.principal.health_state || props.account.principal.admin_state) : props.account?.status || '-')
const formatDate = (value?: string | number | null) => {
  if (!value) return '-'
  return formatDateTime(typeof value === 'number' ? new Date(value * 1000) : value)
}
const stateLabel = (state: string) => t(`admin.accounts.instances.states.${state}`, state)
const statusClass = (state: string) => ['inline-flex rounded-full px-2 py-0.5 text-xs font-medium', ['ACTIVE', 'AVAILABLE'].includes(state) ? 'bg-emerald-100 text-emerald-700' : ['NEEDS_REAUTH', 'UNKNOWN', 'REVOKED'].includes(state) ? 'bg-red-100 text-red-700' : 'bg-amber-100 text-amber-700']
async function load() {
  if (!props.account?.principal?.id) return
  busy.value = true; error.value = ''
  try { const nextPrincipal = await getCredentialPrincipal(props.account.principal.id); principal.value = nextPrincipal; limitDraft.value = nextPrincipal.account_max_concurrency; emit('updated', nextPrincipal) } catch { error.value = t('admin.accounts.instances.stale') } finally { busy.value = false }
}
async function mutate(action: () => Promise<CredentialPrincipal | void>) {
  if (!principal.value || busy.value) return
  busy.value = true; error.value = ''
  try { const result = await stepUp.run(action); if (result) { principal.value = result; limitDraft.value = result.account_max_concurrency; emit('updated', result) } emit('instances-updated') }
  catch (err) { if (!isStepUpCancelled(err)) { if (credentialErrorReason(err) === 'CONFIG_VERSION_CONFLICT') { await load(); error.value = t('admin.accounts.instances.errors.CONFIG_VERSION_CONFLICT') } else error.value = t('admin.accounts.instances.errors.UNAVAILABLE') } }
  finally { busy.value = false }
}
async function saveLimit() { if (!principal.value || !Number.isInteger(limitDraft.value) || limitDraft.value < 0) return; await mutate(() => saveCredentialConfiguration(principal.value!, { account_max_concurrency: limitDraft.value })); editingLimit.value = false }
async function saveInstance() { if (!editTarget.value || !principal.value || !editDraft.value.name.trim()) return; await mutate(() => saveCredentialConfiguration(principal.value!, { instances: activeInstances.value.map(item => ({ id: item.id, name: item.id === editTarget.value!.id ? editDraft.value.name.trim() : item.name, max_concurrency: item.id === editTarget.value!.id ? editDraft.value.max_concurrency : item.max_concurrency })) })); editTarget.value = null }
function editInstance(instance: CredentialInstance) { editTarget.value = instance; editDraft.value = { name: instance.name, max_concurrency: instance.max_concurrency } }
async function toggleDrain(instance: CredentialInstance) { await mutate(() => controlCredentialInstance(principal.value!, instance.id, { admin_state: instance.admin_state === 'ACTIVE' ? 'DRAINING' : 'ACTIVE', ...(instance.admin_state === 'ACTIVE' ? { drain_deadline: new Date(Date.now() + 86400000).toISOString() } : {}) })) }
function archiveInstance(id: number) { archiveTarget.value = activeInstances.value.find(instance => instance.id === id) || null }
async function confirmArchive() { if (!archiveTarget.value) return; const id = archiveTarget.value.id; archiveTarget.value = null; await mutate(() => controlCredentialInstance(principal.value!, id, { archive: true })) }
async function refreshInstance(id: number) { await mutate(async () => { await refreshCredentialInstance(id); return getCredentialPrincipal(principal.value!.id) }) }
function reauthorize(id: number) { editTarget.value = null; reauthorizeId.value = id; showAdd.value = true }
const reauthorizeId = ref<number>()
function openAdd() { reauthorizeId.value = undefined; showAdd.value = true }
async function handleAdded() { showAdd.value = false; reauthorizeId.value = undefined; await load() }
watch(() => [props.show, props.account?.principal?.id], ([visible]) => { if (visible) { activeTab.value = 'devices'; void load() } }, { immediate: true })
</script>
