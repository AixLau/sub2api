<template>
  <BaseDialog :show="show" :title="t('admin.accounts.instances.deviceManagement')" width="wide" @close="emit('close')">
    <div v-if="loading && !principal" class="flex min-h-48 items-center justify-center text-sm text-gray-500">
      {{ t('common.loading') }}
    </div>
    <div v-else-if="error && !principal" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
      {{ error }}
      <button class="btn btn-secondary btn-sm ml-3" @click="load">{{ t('common.retry') }}</button>
    </div>
    <template v-else-if="principal">
      <div class="space-y-5">
        <section class="rounded-lg border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-600 dark:bg-dark-800/60">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h4 class="text-base font-semibold text-gray-900 dark:text-white">{{ account?.name || principal.name }}</h4>
              <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                <span>{{ account?.platform || 'openai' }}</span>
                <span aria-hidden="true">·</span>
                <span :class="principal.admin_state === 'ACTIVE' ? 'text-emerald-600' : 'text-amber-600'">
                  {{ t(`admin.accounts.instances.states.${principal.health_state || principal.admin_state}`) }}
                </span>
              </div>
            </div>
            <button class="btn btn-secondary btn-sm" :disabled="loading" :title="t('common.refresh')" @click="load">
              <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
              <span>{{ t('common.refresh') }}</span>
            </button>
          </div>
          <dl class="mt-4 grid gap-3 text-xs text-gray-600 dark:text-gray-300 sm:grid-cols-2">
            <div><dt class="text-gray-400">{{ t('admin.accounts.instances.createdAt') }}</dt><dd class="mt-0.5">{{ formatDate(account?.created_at) }}</dd></div>
            <div><dt class="text-gray-400">{{ t('admin.accounts.instances.updatedAt') }}</dt><dd class="mt-0.5">{{ formatDate(principal.observed_at || account?.updated_at) }}</dd></div>
          </dl>
        </section>

        <section class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.instances.accountLimit') }}</div>
            <div v-if="!editingLimit" class="mt-1 flex items-center gap-2 text-xl font-semibold text-gray-900 dark:text-white">
              {{ principal.account_max_concurrency }}
              <button class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700" :aria-label="t('common.edit')" @click="editingLimit = true"><Icon name="edit" size="xs" /></button>
            </div>
            <div v-else class="mt-1 flex items-center gap-2">
              <input v-model.number="limitDraft" class="input h-8 w-24" type="number" min="0" max="2147483647" />
              <button class="btn btn-primary btn-sm" :disabled="savingLimit" @click="saveLimit">{{ t('common.save') }}</button>
              <button class="btn btn-secondary btn-sm" :disabled="savingLimit" @click="cancelLimit">{{ t('common.cancel') }}</button>
            </div>
            <p v-if="principal.occupied > principal.account_max_concurrency" class="mt-1 text-xs text-amber-600">{{ t('admin.accounts.instances.shrinking', { count: principal.occupied - principal.account_max_concurrency }) }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.instances.configuredCapacity') }}</div>
            <div class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ configuredCapacity }}</div>
            <div class="mt-1 text-xs text-gray-500">{{ capacityBreakdown }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.instances.currentUsage') }}</div>
            <div class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ principal.occupied }} / {{ effectiveCapacity }}</div>
            <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700"><div class="h-full rounded-full bg-primary-500 transition-all" :style="{ width: `${usagePercent}%` }"></div></div>
          </div>
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.instances.availableBalance') }}</div>
            <div class="mt-1 text-xl font-semibold text-emerald-600">{{ availableCapacity }}</div>
            <div class="mt-1 text-xs text-gray-500">{{ t('admin.accounts.instances.effectiveCapacity') }}: {{ effectiveCapacity }}</div>
          </div>
        </section>

        <div class="border-b border-gray-200 dark:border-dark-600">
          <nav class="-mb-px flex gap-5" role="tablist">
            <button v-for="tab in tabs" :key="tab.id" class="border-b-2 px-1 py-2 text-sm" :class="activeTab === tab.id ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-500 hover:text-gray-700'" role="tab" :aria-selected="activeTab === tab.id" @click="activeTab = tab.id">{{ tab.label }}</button>
          </nav>
        </div>

        <section v-if="activeTab === 'devices'" role="tabpanel">
          <div class="mb-3 flex items-center justify-between gap-2">
            <h4 class="font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.instances.deviceList') }}</h4>
            <button class="btn btn-primary btn-sm" :disabled="loading" @click="adding = true"><Icon name="plus" size="sm" />{{ t('admin.accounts.instances.addDevice') }}</button>
          </div>
          <AccountInstancesEditor :principal-id="principal.id" :disabled="loading" @loaded="onEditorLoaded" @updated="onEditorUpdated" @busy="loading = $event" />
          <div v-if="adding" class="mt-4 rounded-lg border border-primary-200 bg-primary-50/40 p-4 dark:border-primary-900 dark:bg-primary-950/20">
            <div class="mb-3 flex items-center justify-between"><h5 class="font-medium">{{ t('admin.accounts.instances.addDevice') }}</h5><button class="text-gray-500" :aria-label="t('common.close')" @click="adding = false"><Icon name="x" size="sm" /></button></div>
            <OpenAIInstanceForm :principal="principal" :account-name="principal.name" :account-limit="principal.account_max_concurrency" :initial-max="5" :proxy-id="principal.proxy_id" @completed="onAdded" />
          </div>
        </section>

        <section v-else-if="activeTab === 'runtime'" role="tabpanel" class="space-y-4">
          <div class="grid gap-3 sm:grid-cols-4">
            <div v-for="item in runtimeSummary" :key="item.label" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"><div class="text-xs text-gray-500">{{ item.label }}</div><div class="mt-1 text-xl font-semibold">{{ item.value }}</div></div>
          </div>
          <div v-if="runtime?.wait_reasons?.length" class="rounded-lg border border-gray-200 p-4 dark:border-dark-600"><h5 class="mb-2 font-medium">{{ t('admin.accounts.instances.waitReasons') }}</h5><div class="grid gap-2 sm:grid-cols-2"><div v-for="wait in runtime.wait_reasons" :key="wait.reason" class="flex justify-between text-sm"><span>{{ t(`admin.accounts.instances.wait.${wait.reason}`) }}</span><span class="font-medium">{{ wait.count }}</span></div></div></div>
          <details v-if="runtime?.unresolved_leases?.length" class="rounded-lg border border-amber-200 p-4 dark:border-amber-900"><summary class="cursor-pointer font-medium text-amber-700">{{ t('admin.accounts.instances.reconcile') }} ({{ runtime.unresolved_leases.length }})</summary><div class="mt-2 text-sm text-gray-600">{{ t('admin.accounts.instances.reconcileHint') }}</div></details>
          <p v-else class="rounded-lg border border-gray-200 p-4 text-sm text-gray-500 dark:border-dark-600">{{ t('admin.accounts.instances.noRuntimeIssues') }}</p>
        </section>

        <section v-else role="tabpanel" class="grid gap-3 sm:grid-cols-2">
          <div v-for="item in advancedPrincipal" :key="item.label" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"><div class="text-xs text-gray-500">{{ item.label }}</div><div class="mt-1 break-all text-sm text-gray-900 dark:text-white">{{ item.value }}</div></div>
          <div v-for="instance in activeInstances" :key="instance.id" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600 sm:col-span-2"><div class="mb-2 font-medium">{{ instance.name }} · #{{ instance.id }}</div><div class="grid gap-2 text-xs text-gray-500 sm:grid-cols-3"><span>Generation: {{ instance.generation }}</span><span>Credential: {{ instance.credential_version }}</span><span>Transport: {{ instance.transport_state }}</span><span>Identity: {{ instance.identity_source }}</span><span>Bindings: {{ instance.active_bindings }}</span><span>Unknown: {{ instance.unknown_occupied }}</span></div></div>
        </section>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import AccountInstancesEditor from './AccountInstancesEditor.vue'
import OpenAIInstanceForm from './OpenAIInstanceForm.vue'
import { getCredentialPrincipal, getCredentialRuntime, saveCredentialConfiguration, type CredentialPrincipal, type CredentialRuntime } from '@/api/admin/credentialPrincipals'
import type { Account } from '@/types'

const props = defineProps<{ show: boolean; principalId: number | null; account?: Account | null }>()
const emit = defineEmits<{ close: []; 'instances-updated': [principal: CredentialPrincipal] }>()
const { t } = useI18n()
const principal = ref<CredentialPrincipal>()
const runtime = ref<CredentialRuntime>()
const loading = ref(false)
const savingLimit = ref(false)
const error = ref('')
const adding = ref(false)
const editingLimit = ref(false)
const limitDraft = ref(0)
const activeTab = ref<'devices' | 'runtime' | 'advanced'>('devices')
const tabs = computed(() => [{ id: 'devices' as const, label: t('admin.accounts.instances.deviceList') }, { id: 'runtime' as const, label: t('admin.accounts.instances.runtimeStatus') }, { id: 'advanced' as const, label: t('admin.accounts.instances.advancedInfo') }])
const activeInstances = computed(() => (principal.value?.instances || []).filter(instance => !instance.archived_at))
const configuredCapacity = computed(() => activeInstances.value.reduce((sum, instance) => sum + Math.max(0, instance.max_concurrency), 0))
const capacityBreakdown = computed(() => activeInstances.value.map(instance => instance.max_concurrency).join(' + ') || '0')
const effectiveCapacity = computed(() => Math.min(principal.value?.account_max_concurrency || 0, configuredCapacity.value))
const availableCapacity = computed(() => Math.max(0, effectiveCapacity.value - (principal.value?.occupied || 0)))
const usagePercent = computed(() => effectiveCapacity.value ? Math.min(100, Math.round((principal.value?.occupied || 0) / effectiveCapacity.value * 100)) : 0)
const runtimeSummary = computed(() => [{ label: t('admin.accounts.instances.running'), value: (runtime.value?.running || 0) + (runtime.value?.dispatching || 0) }, { label: t('admin.accounts.instances.reserved'), value: runtime.value?.reserved || 0 }, { label: t('admin.accounts.instances.queued'), value: runtime.value?.queued || 0 }, { label: t('admin.accounts.instances.unresolved'), value: runtime.value?.orphaned || 0 }])
const advancedPrincipal = computed(() => principal.value ? [{ label: 'Principal ID', value: principal.value.id }, { label: 'Account ID', value: principal.value.account_id }, { label: 'Config Version', value: principal.value.config_version }, { label: 'Routing Mode', value: principal.value.routing_mode }, { label: 'Admission State', value: principal.value.admission_state }, { label: 'Verification State', value: principal.value.verification_state }, { label: 'Health State', value: principal.value.health_state }, { label: 'Unknown Occupied', value: principal.value.unknown_occupied }, { label: 'Observed At', value: formatDate(principal.value.observed_at) }] : [])
function formatDate(value?: string | number | null) { if (!value) return '-'; const date = new Date(typeof value === 'number' ? value * 1000 : value); return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString() }
function apply(value: CredentialPrincipal) { principal.value = value; limitDraft.value = value.account_max_concurrency }
async function load() { if (!props.principalId) return; loading.value = true; error.value = ''; try { const [value, state] = await Promise.all([getCredentialPrincipal(props.principalId), getCredentialRuntime(props.principalId)]); apply(value); runtime.value = state } catch { error.value = t('admin.accounts.instances.stale') } finally { loading.value = false } }
async function saveLimit() { if (!principal.value || !Number.isInteger(limitDraft.value) || limitDraft.value < 0) return; savingLimit.value = true; try { const value = await saveCredentialConfiguration(principal.value, { account_max_concurrency: limitDraft.value }); apply(value); editingLimit.value = false; emit('instances-updated', value) } catch (err) { error.value = t('admin.accounts.instances.errors.UNAVAILABLE') } finally { savingLimit.value = false } }
function cancelLimit() { limitDraft.value = principal.value?.account_max_concurrency || 0; editingLimit.value = false }
function onEditorLoaded(value: CredentialPrincipal) { apply(value); void getCredentialRuntime(value.id).then(state => { runtime.value = state }) }
function onEditorUpdated(value: CredentialPrincipal) { apply(value); emit('instances-updated', value) }
function onAdded() { adding.value = false; void load(); }
watch(() => [props.show, props.principalId] as const, ([show]) => { if (show) { activeTab.value = 'devices'; adding.value = false; void load() } }, { immediate: true })
</script>
