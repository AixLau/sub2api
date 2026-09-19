<template>
  <TotpStepUpDialog :controller="stepUp" />
  <div class="space-y-4">
    <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.accounts.instances.authorizationHint') }}</p>
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-sm">{{ t('admin.accounts.instances.name') }}
        <input v-model="name" class="input mt-1 w-full" maxlength="100" :disabled="!!pending" />
      </label>
      <label class="text-sm">{{ t('admin.accounts.instances.maxConcurrency') }}
        <input v-model.number="maximum" class="input mt-1 w-full" type="number" min="0" max="2147483647" step="1" :disabled="!!pending" />
      </label>
    </div>
    <p class="text-sm">{{ t('admin.accounts.instances.accountLimit') }}: {{ pending?.principal?.account_max_concurrency ?? pending?.account.account_max_concurrency ?? principal?.account_max_concurrency ?? accountLimit }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <a v-if="duplicatePrincipal" class="text-sm text-primary-600 underline" :href="`/admin/accounts?principal_id=${duplicatePrincipal}`">{{ t('admin.accounts.instances.openExisting') }}</a>
    <div v-if="pending" class="space-y-3 rounded-lg bg-blue-50 p-3 dark:bg-blue-950">
      <p v-if="!pending.principal" class="font-medium">{{ pending.account.name }}</p>
      <p>{{ t(pending.verified ? 'admin.accounts.instances.pendingCreation' : 'admin.accounts.instances.pendingVerification') }}</p>
      <p class="break-all text-xs">{{ pending.importID }}</p>
      <button v-if="pending.verified" class="btn btn-primary" :disabled="busy" @click="complete">{{ t('admin.accounts.instances.resumeOperation') }}</button>
      <button v-else class="btn btn-secondary" :disabled="busy" @click="reverify">{{ t('common.retry') }}</button>
      <button v-if="duplicatePrincipal" class="btn btn-secondary" :disabled="busy" @click="attachExisting">{{ t('admin.accounts.instances.attachExisting') }}</button>
      <button class="btn btn-secondary" :disabled="busy" @click="restart">{{ t('admin.accounts.instances.restart') }}</button>
      <div v-if="mismatch" class="space-y-2">
        <label class="text-sm">{{ t('admin.accounts.accountName') }}<input v-model="newAccountName" class="input mt-1 w-full" /></label>
        <button class="btn btn-secondary" :disabled="busy || !newAccountName.trim()" @click="createSeparate">{{ t('admin.accounts.instances.createSeparate') }}</button>
      </div>
    </div>
    <template v-else>
      <OAuthAuthorizationFlow ref="flow" platform="openai" add-method="oauth"
        :auth-url="oauth.authUrl.value" :session-id="oauth.sessionId.value"
        :loading="busy || oauth.loading.value" :error="oauth.error.value"
        :show-help="false" :show-manual-option="true" initial-input-method="manual" :show-proxy-warning="false" :show-cookie-option="false"
        :show-refresh-token-option="true" :show-mobile-refresh-token-option="true"
        :show-codex-session-import-option="true" :show-agent-identity-option="!principal" :show-codex-pat-option="true"
        :allow-multiple="false"
        @generate-url="oauth.generateAuthUrl(proxyId)" @validate-refresh-token="refreshToken"
        @validate-mobile-refresh-token="refreshToken($event, 'app_LlGpXReQgckcGGUo2JrYvtJK')"
        @import-codex-session="importSession" @import-codex-pat="authorize({ access_token: $event })" />
      <button v-if="flow?.inputMethod === 'manual'" class="btn btn-primary" :disabled="busy || !flow?.authCode || !valid" @click="exchange">
        {{ t('admin.accounts.oauth.completeAuth') }}
      </button>
      <button v-if="retrySecret" class="btn btn-secondary" :disabled="busy" @click="authorize(retrySecret)">{{ t('common.retry') }}</button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useStepUp, isStepUpCancelled } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import OAuthAuthorizationFlow from './OAuthAuthorizationFlow.vue'
import { useOpenAIOAuth, type OpenAITokenInfo } from '@/composables/useOpenAIOAuth'
import { reverifyCredentialImport, getCredentialPrincipal, addCredentialInstance, createCredentialAccount, importCredential, credentialErrorReason, type CredentialPrincipal, type CredentialAccountInput, type CredentialInstanceInput } from '@/api/admin/credentialPrincipals'

const props = defineProps<{
  extra?: Record<string, unknown>
  principal?: CredentialPrincipal
  accountName: string
  accountLimit: number
  initialName?: string
  initialMax?: number
  groupIds?: number[]
  proxyId?: number | null
  priority?: number
  rateMultiplier?: number
  replaceInstanceId?: number
}>()
const emit = defineEmits<{ completed: []; 'agent-identity': [content: string] }>()
const { t } = useI18n()
const stepUp = useStepUp()
const oauth = useOpenAIOAuth()
const flow = ref<InstanceType<typeof OAuthAuthorizationFlow>>()
const busy = ref(false)
const error = ref('')
const duplicatePrincipal = ref<number>()
const mismatch = ref(false)
const name = ref(props.initialName || `${props.accountName} 1`)
const maximum = ref(props.initialMax ?? props.accountLimit)
const newAccountName = ref('')
const valid = computed(() => name.value.trim() && Number.isInteger(maximum.value) && maximum.value >= 0 && maximum.value <= 2147483647)
let operation = crypto.randomUUID()
const retrySecret = ref<OpenAITokenInfo | null>(null)
const storageKey = `openai-instance-operation:${props.principal?.id ?? 'new'}:${props.replaceInstanceId ?? 'add'}`
interface Pending { importID: string; verified: boolean; operation: string; input: CredentialInstanceInput; account: CredentialAccountInput; principal?: CredentialPrincipal }
const pending = ref<Pending | null>(readPending())
if (pending.value) { name.value = pending.value.input.name; maximum.value = pending.value.input.max_concurrency }
function readPending(): Pending | null {
  try { return JSON.parse(sessionStorage.getItem(storageKey) || 'null') } catch { return null }
}
function persist() { if (pending.value) sessionStorage.setItem(storageKey, JSON.stringify(pending.value)) }
function showError(err: unknown) {
  if (isStepUpCancelled(err)) return
  const metadata = (err as { metadata?: { principal_id?: string } })?.metadata
  const id = Number(metadata?.principal_id)
  duplicatePrincipal.value = Number.isSafeInteger(id) && id > 0 ? id : undefined
  const reason = credentialErrorReason(err)
  mismatch.value = reason === 'CREDENTIAL_OWNERSHIP_MISMATCH'
  const known = ['CREDENTIAL_OWNERSHIP_MISMATCH', 'CREDENTIAL_DUPLICATE', 'CREDENTIAL_UNVERIFIED', 'CREDENTIAL_IMPORT_EXPIRED', 'CONFIG_VERSION_CONFLICT']
  error.value = t(`admin.accounts.instances.errors.${known.includes(reason) ? reason : 'UNAVAILABLE'}`)
}
async function authorize(info: OpenAITokenInfo) {
  if (!valid.value || busy.value || !info.access_token) return
  busy.value = true; error.value = ''; retrySecret.value = info
  try {
    const result = await importCredential(info.access_token, info.refresh_token || '', operation, info.client_id)
    const input: CredentialInstanceInput = { credential_import_id: result.import_id, name: name.value.trim(), max_concurrency: maximum.value }
    if (props.replaceInstanceId) { input.replace_instance_id = props.replaceInstanceId; input.drain_deadline = new Date(Date.now() + 86400000).toISOString() }
    pending.value = { importID: result.import_id, verified: result.verification_state === 'VERIFIED', operation: crypto.randomUUID(), input,
      account: { extra: props.extra, name: props.accountName, account_max_concurrency: props.accountLimit, instances: [input], group_ids: props.groupIds, proxy_id: props.proxyId, priority: props.priority, rate_multiplier: props.rateMultiplier }, principal: props.principal }
    persist(); retrySecret.value = null; flow.value?.reset()
  } catch (err) { showError(err) } finally { busy.value = false }
  if (pending.value?.verified) await complete()
}
async function complete() {
  const op = pending.value
  if (!op || !op.verified || busy.value) return
  busy.value = true; error.value = ''
  try {
    if (op.principal) await stepUp.run(() => addCredentialInstance(op.principal!, op.input, op.operation))
    else await createCredentialAccount(op.account, op.operation)
    sessionStorage.removeItem(storageKey); pending.value = null; emit('completed')
  } catch (err) {
    showError(err)
    if (credentialErrorReason(err) === 'CONFIG_VERSION_CONFLICT' && op.principal) {
      try { op.principal = await getCredentialPrincipal(op.principal.id); persist() } catch { /* Keep the operation available for retry. */ }
    }
  } finally { busy.value = false }
}
async function reverify() {
  if (!pending.value || busy.value) return
  busy.value = true
  try {
    const data = await reverifyCredentialImport(pending.value.importID)
    pending.value.verified = data.verification_state === 'VERIFIED'; persist()
  } catch (err) { showError(err) } finally { busy.value = false }
}
function restart() { sessionStorage.removeItem(storageKey); pending.value = null; retrySecret.value = null; duplicatePrincipal.value = undefined; operation = crypto.randomUUID(); error.value = ''; mismatch.value = false; oauth.resetState(); flow.value?.reset() }
async function attachExisting() {
  if (!pending.value || !duplicatePrincipal.value) return
  try { pending.value.principal = await getCredentialPrincipal(duplicatePrincipal.value); persist(); await complete() } catch (err) { showError(err) }
}
async function createSeparate() {
  if (!pending.value) return
  pending.value.principal = undefined; pending.value.account.name = newAccountName.value.trim()
  pending.value.operation = crypto.randomUUID(); mismatch.value = false; persist(); await complete()
}
async function exchange() {
  if (!valid.value || busy.value || oauth.loading.value) return
  const info = await oauth.exchangeAuthCode(flow.value?.authCode || '', oauth.sessionId.value, flow.value?.oauthState || oauth.oauthState.value, props.proxyId)
  if (info) await authorize(info)
}
async function refreshToken(value: string, clientId?: string) {
  if (!valid.value || busy.value || oauth.loading.value) return
  const info = await oauth.validateRefreshToken(value, props.proxyId, clientId)
  if (info) await authorize(info)
}
async function importSession(value: string) {
  try {
    const parsed = JSON.parse(value)
    if (!props.principal && (parsed.agent_identity || parsed.agentIdentity || parsed.auth_mode === 'agentIdentity' || parsed.authMode === 'agentIdentity')) { emit('agent-identity', value); return }
    const tokens = parsed.tokens || parsed
    if (typeof tokens.access_token !== 'string') throw new Error('missing token')
    await authorize(tokens)
  } catch { error.value = t('admin.accounts.instances.errors.CREDENTIAL_UNVERIFIED') }
}
</script>
