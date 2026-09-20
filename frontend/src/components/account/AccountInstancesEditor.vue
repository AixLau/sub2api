<template>
  <TotpStepUpDialog :controller="stepUp" />
  <section class="mt-6 border-t border-gray-200 pt-5 dark:border-dark-600" data-testid="account-instances-editor">
    <h4 class="mb-3 font-medium">{{ t('admin.accounts.credentials.title') }}</h4>
    <fieldset class="space-y-5" :disabled="disabled || busy">
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <template v-if="principal">
        <InstanceCapacitySummary :principal="principal" />
        <p v-if="principal.routing_mode !== 'GROUPED'" class="text-sm text-amber-700">{{ t('admin.accounts.instances.routingPending') }}</p>
        <p v-if="runtime" class="text-sm text-gray-600">{{ t('admin.accounts.instances.runtime', { running: runtime.running + runtime.dispatching, reserved: runtime.reserved, unknown: runtime.orphaned, queued: runtime.queued }) }}</p>
        <div v-if="runtime?.wait_reasons?.length" class="flex flex-wrap gap-2 text-xs">
          <span v-for="wait in runtime.wait_reasons" :key="wait.reason">{{ t(`admin.accounts.instances.wait.${wait.reason}`) }}: {{ wait.count }}</span>
        </div>
        <details v-if="runtime?.unresolved_leases?.length" class="rounded border p-3">
          <summary>{{ t('admin.accounts.instances.reconcile') }}</summary>
          <form class="mt-3 space-y-2" @submit.prevent="resolveLease">
            <select v-model="leaseId" class="input w-full" required><option value="" disabled>{{ t('admin.accounts.instances.selectLease') }}</option><option v-for="lease in runtime.unresolved_leases" :key="lease.id" :value="lease.id">#{{ lease.instance_id }} · {{ lease.id }} · {{ lease.observed_at }}</option></select>
            <label class="block text-sm">{{ t('admin.accounts.instances.evidence') }}<textarea v-model="evidence" class="input mt-1 w-full" minlength="8" maxlength="4096" required /></label>
            <label class="block text-sm">{{ t('admin.accounts.instances.reason') }}<input v-model="resolutionReason" class="input mt-1 w-full" minlength="4" maxlength="512" required /></label>
            <label class="block text-sm"><input v-model="confirmedTerminal" type="checkbox" required /> {{ t('admin.accounts.instances.confirmTerminal') }}</label>
            <button class="btn btn-secondary" :disabled="busy || !confirmedTerminal">{{ t('admin.accounts.instances.releaseConfirmed') }}</button>
          </form>
        </details>
        <form class="space-y-4" @submit.prevent="save">
          <div v-for="instance in editable" :key="instance.id" class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
            <div class="grid gap-3 sm:grid-cols-2">
              <label class="text-sm">{{ t('admin.accounts.instances.name') }}<input v-model="instance.name" class="input mt-1 w-full" required maxlength="100" /></label>
              <label class="text-sm">{{ t('admin.accounts.instances.maxConcurrency') }}<input v-model.number="instance.max_concurrency" class="input mt-1 w-full" type="number" min="0" max="2147483647" step="1" required /></label>
            </div>
            <p class="text-xs text-gray-500">#{{ instance.id }} · {{ t(`admin.accounts.instances.states.${instance.state}`) }} · {{ instance.occupied }}/{{ instance.max_concurrency }} · {{ t('admin.accounts.instances.bindings', { count: instance.active_bindings }) }}</p>
            <p v-if="instance.occupied > instance.max_concurrency" class="text-xs text-amber-700">{{ t('admin.accounts.instances.shrinking', { count: instance.occupied - instance.max_concurrency }) }}</p>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="control(instance.id, instance.admin_state === 'ACTIVE' ? 'DRAINING' : 'ACTIVE')">{{ t(instance.admin_state === 'ACTIVE' ? 'admin.accounts.instances.drain' : 'admin.accounts.instances.resume') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="refresh(instance.id)">{{ t('admin.accounts.instances.refreshCredential') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="replaceId = instance.id; adding = true">{{ t('admin.accounts.instances.reauthorize') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || instance.admin_state === 'ACTIVE' || instance.occupied > 0 || instance.active_bindings > 0" @click="archiveInstance(instance.id)">{{ t('admin.accounts.instances.remove') }}</button>
            </div>
          </div>
          <div class="flex flex-wrap gap-2">
            <button class="btn btn-secondary" :disabled="busy">{{ t('admin.accounts.instances.saveInstances') }}</button>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="replaceId = undefined; adding = !adding">{{ t('admin.accounts.instances.add') }}</button>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="load">{{ t('common.refresh') }}</button>
            <button type="button" class="btn btn-secondary" :disabled="busy || principal.occupied > 0 || editable.some(i => i.admin_state === 'ACTIVE' || i.occupied > 0 || i.active_bindings > 0)" @click="archiveAccount">{{ t('admin.accounts.instances.archiveAccount') }}</button>
          </div>
        </form>
        <OpenAIInstanceForm v-if="adding" :key="replaceId || 'add'" :principal="principal" :account-name="principal.name" :account-limit="principal.account_max_concurrency" :initial-max="10" :proxy-id="principal.proxy_id" :replace-instance-id="replaceId" @completed="added" />
      </template>
    </fieldset>
  </section>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStepUp, isStepUpCancelled } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import OpenAIInstanceForm from './OpenAIInstanceForm.vue'
import InstanceCapacitySummary from './InstanceCapacitySummary.vue'
import { resolveCredentialLease, getCredentialPrincipal, getCredentialRuntime, saveCredentialConfiguration, controlCredentialInstance, refreshCredentialInstance, credentialErrorReason, type CredentialPrincipal, type CredentialInstance, type CredentialRuntime } from '@/api/admin/credentialPrincipals'
const props = defineProps<{ principalId: number; disabled?: boolean }>()
const emit = defineEmits<{ archived: []; updated: [principal: CredentialPrincipal]; loaded: [principal: CredentialPrincipal]; busy: [busy: boolean] }>()
const { t } = useI18n()
const stepUp = useStepUp()
const principal = ref<CredentialPrincipal>()
const runtime = ref<CredentialRuntime>()
const editable = ref<CredentialInstance[]>([])
const busy = ref(false)
const error = ref('')
const leaseId = ref('')
const evidence = ref('')
const resolutionReason = ref('')
const confirmedTerminal = ref(false)
const adding = ref(false)
const replaceId = ref<number>()
function apply(value: CredentialPrincipal) {
  principal.value = value
  editable.value = value.instances.filter(i => !i.archived_at).map(i => ({ ...i }))
}
async function load() {
  busy.value = true
  try { const [value, state] = await Promise.all([getCredentialPrincipal(props.principalId), getCredentialRuntime(props.principalId)]); apply(value); runtime.value = state; emit('loaded', value) } catch { error.value = t('admin.accounts.instances.stale') } finally { busy.value = false }
}
async function mutate(action: () => Promise<CredentialPrincipal | void>) {
  if (busy.value || props.disabled) return
  busy.value = true; error.value = ''
  try { const value = await stepUp.run(action); if (value) { apply(value); emit('updated', value) } }
  catch (err) {
    if (isStepUpCancelled(err)) return
    const reason = credentialErrorReason(err)
    if (reason === 'CONFIG_VERSION_CONFLICT') { await load(); error.value = t('admin.accounts.instances.errors.CONFIG_VERSION_CONFLICT') }
    else if (reason === 'GROUPED_ROUTE_MIXED_UNSUPPORTED') error.value = t('admin.accounts.instances.mixedGroup')
    else error.value = t(reason === 'INSTANCE_EXIT_PENDING' ? 'admin.accounts.instances.exitPending' : 'admin.accounts.instances.errors.UNAVAILABLE')
  } finally { busy.value = false }
}
async function save() {
  if (!principal.value || editable.value.some(i => !Number.isInteger(i.max_concurrency) || i.max_concurrency < 0 || i.max_concurrency > 2147483647 || !i.name.trim())) return
  await mutate(() => saveCredentialConfiguration(principal.value!, { instances: editable.value.map(i => ({ id: i.id, name: i.name.trim(), max_concurrency: i.max_concurrency })) }))
}
async function control(id: number, state: string) {
  await mutate(() => controlCredentialInstance(principal.value!, id, { admin_state: state, ...(state === 'DRAINING' ? { drain_deadline: new Date(Date.now() + 86400000).toISOString() } : {}) }))
}
async function archiveInstance(id: number) { await mutate(() => controlCredentialInstance(principal.value!, id, { archive: true })) }
async function refresh(id: number) { await mutate(async () => { await refreshCredentialInstance(id); return getCredentialPrincipal(props.principalId) }) }
async function archiveAccount() { await mutate(async () => { await saveCredentialConfiguration(principal.value!, { archive: true }); emit('archived') }) }
async function resolveLease() {
  if (!confirmedTerminal.value || !leaseId.value) return
  await mutate(async () => { await resolveCredentialLease(leaseId.value, evidence.value, resolutionReason.value); runtime.value = await getCredentialRuntime(props.principalId); confirmedTerminal.value = false; return getCredentialPrincipal(props.principalId) })
}
async function added() { adding.value = false; await load(); if (principal.value) emit('updated', principal.value) }
function openAdd() { replaceId.value = undefined; adding.value = true }
defineExpose({ openAdd, load })
watch([busy, adding], ([working, authorizing]) => emit('busy', working || authorizing), { immediate: true, flush: 'sync' })
watch(() => props.principalId, () => { adding.value = false; principal.value = undefined; runtime.value = undefined; error.value = ''; void load() }, { immediate: true })
</script>
