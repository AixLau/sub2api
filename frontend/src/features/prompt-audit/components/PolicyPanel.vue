<template>
  <section aria-labelledby="prompt-policy-title" class="py-6">
    <div>
      <h2 id="prompt-policy-title" class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.policy.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.policy.description') }}</p>
    </div>

    <div class="mt-5 grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(260px,0.45fr)]">
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700/60 dark:bg-dark-900/20 sm:p-5">
        <fieldset>
          <legend class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.policy.scope') }}</legend>
          <div class="mt-3 flex flex-wrap gap-5 text-sm text-gray-700 dark:text-dark-200">
            <label class="flex items-center gap-2">
              <input type="radio" name="prompt-audit-scope" :checked="draft.all_groups" @change="patch({ all_groups: true, group_ids: [] })" />
              {{ t('admin.promptAudit.policy.allGroups') }}
            </label>
            <label class="flex items-center gap-2">
              <input type="radio" name="prompt-audit-scope" :checked="!draft.all_groups" @change="patch({ all_groups: false })" />
              {{ t('admin.promptAudit.policy.selectedGroups') }}
            </label>
          </div>
        </fieldset>

        <div v-if="!draft.all_groups" class="mt-4">
          <label class="block text-sm text-gray-700 dark:text-dark-200">
            <span>{{ t('admin.promptAudit.policy.searchGroups') }}</span>
            <input v-model="groupSearch" type="search" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.policy.searchGroups')" />
          </label>
          <div class="mt-3 max-h-52 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-700">
            <label v-for="group in filteredGroups" :key="group.id" class="flex cursor-pointer items-center justify-between gap-3 rounded-md px-2 py-2 text-sm hover:bg-gray-50 dark:hover:bg-dark-800">
              <span class="flex items-center gap-2 text-gray-800 dark:text-dark-100">
                <input type="checkbox" :checked="draft.group_ids.includes(group.id)" @change="toggleGroup(group.id)" />
                {{ group.name }}
              </span>
              <span class="text-xs text-gray-500 dark:text-dark-400">{{ group.platform }} · {{ group.status }}</span>
            </label>
            <p v-if="filteredGroups.length === 0" class="px-2 py-4 text-center text-sm text-gray-500">{{ t('admin.promptAudit.policy.noGroups') }}</p>
          </div>
          <div v-if="missingGroupIds.length" class="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
            {{ t('admin.promptAudit.policy.missingGroups') }}: {{ missingGroupIds.join(', ') }}
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.policy.selectedCount', { count: draft.group_ids.length }) }}</p>
        </div>

        <fieldset class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-800">
          <legend class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.policy.accountScope') }}</legend>
          <p class="mt-1 text-xs text-gray-500">{{ t('admin.promptAudit.policy.accountScopeHint') }}</p>
          <div class="mt-3 flex flex-wrap gap-5 text-sm">
            <label class="flex items-center gap-2"><input type="radio" name="prompt-audit-accounts" :checked="!draft.selected_accounts" @change="patch({ selected_accounts: false, account_ids: [] })" />{{ t('admin.promptAudit.policy.allAccounts') }}</label>
            <label class="flex items-center gap-2"><input type="radio" name="prompt-audit-accounts" :checked="draft.selected_accounts" @change="patch({ selected_accounts: true })" />{{ t('admin.promptAudit.policy.selectedAccounts') }}</label>
          </div>
          <div v-if="draft.selected_accounts" class="mt-3">
            <input v-model="accountSearch" type="search" class="input w-full" :aria-label="t('admin.promptAudit.policy.searchAccounts')" :placeholder="t('admin.promptAudit.policy.searchAccounts')" />
            <div class="mt-3 max-h-52 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-700">
              <label v-for="account in filteredAccounts" :key="account.id" class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-sm hover:bg-gray-50 dark:hover:bg-dark-800">
                <input type="checkbox" :checked="draft.account_ids.includes(account.id)" @change="toggleAccount(account.id)" />
                <span>{{ account.name }} · #{{ account.id }}</span><span class="ml-auto text-xs text-gray-500">{{ account.status }}</span>
              </label>
              <p v-if="!filteredAccounts.length" class="py-4 text-center text-sm text-gray-500">{{ t('admin.promptAudit.policy.noAccounts') }}</p>
            </div>
            <p v-if="missingAccountIds.length" class="mt-2 text-sm text-amber-700">{{ t('admin.promptAudit.policy.missingAccounts') }}: {{ missingAccountIds.join(', ') }}</p>
            <button v-if="missingAccountIds.length" type="button" class="btn btn-secondary mt-2" @click="patch({ account_ids: draft.account_ids.filter(id => !missingAccountIds.includes(id)) })">{{ t('admin.promptAudit.policy.clearMissingAccounts') }}</button>
            <p class="mt-2 text-xs text-gray-500">{{ t('admin.promptAudit.policy.selectedAccountCount', { count: draft.account_ids.length }) }}</p>
          </div>
        </fieldset>

        <fieldset class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-800">
          <legend class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.policy.scanners') }}</legend>
          <div class="mt-3 grid gap-2 sm:grid-cols-2">
            <label v-for="scanner in SCANNER_CATALOG" :key="scanner.id" class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-800">
              <input type="checkbox" :checked="draft.scanners.includes(scanner.id)" :aria-label="scannerLabel(scanner.id)" @change="toggleScanner(scanner.id)" />
              <span>{{ scannerLabel(scanner.id) }}</span>
            </label>
          </div>
        </fieldset>

        <fieldset class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-800">
          <legend class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.policy.captureTitle') }}</legend>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.policy.captureDescription') }}</p>
          <div class="mt-3 grid gap-2 sm:grid-cols-[120px_minmax(0,1fr)_auto]">
            <input v-model.number="captureUserID" type="number" min="1" class="input" :placeholder="t('admin.promptAudit.policy.captureUserId')" />
            <input v-model.trim="captureEmail" type="email" class="input" :placeholder="t('admin.promptAudit.policy.captureEmail')" />
            <button type="button" class="btn btn-secondary" @click="addCaptureUser">{{ t('admin.promptAudit.policy.captureAdd') }}</button>
          </div>
          <div v-if="captureUsers.length" class="mt-3 space-y-2">
            <div v-for="(selector, index) in captureUsers" :key="`${selector.user_id ?? ''}-${selector.email ?? ''}-${index}`" class="flex items-center justify-between gap-3 rounded-md border border-gray-200 px-3 py-2 text-sm dark:border-dark-700">
              <span>{{ selector.email || `#${selector.user_id}` }}</span>
              <button type="button" class="text-red-600 hover:text-red-700" @click="removeCaptureUser(index)">{{ t('common.delete') }}</button>
            </div>
          </div>
          <label class="mt-3 block text-sm text-gray-700 dark:text-dark-200">
            <span>{{ t('admin.promptAudit.policy.captureMaxRecords') }}</span>
            <input :value="draft.capture_max_records" type="number" min="0" max="100000" class="input mt-1.5 w-full" @input="patch({ capture_max_records: Number(($event.target as HTMLInputElement).value) })" />
          </label>
        </fieldset>
      </div>

      <div class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700/60 dark:bg-dark-900/20 sm:p-5">
        <label class="block text-sm text-gray-700 dark:text-dark-200">
          <span>{{ t('admin.promptAudit.policy.workerCount') }}</span>
          <input :value="draft.worker_count" type="number" min="1" max="32" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.policy.workerCount')" @input="patch({ worker_count: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <label class="block text-sm text-gray-700 dark:text-dark-200">
          <span>{{ t('admin.promptAudit.policy.queueCapacity') }}</span>
          <input :value="draft.queue_capacity" type="number" min="1" max="100000" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.policy.queueCapacity')" @input="patch({ queue_capacity: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <div class="rounded-lg bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:bg-dark-900/50 dark:text-dark-300">
          <p class="font-medium text-gray-800 dark:text-dark-100">{{ t('admin.promptAudit.policy.strategy') }}</p>
          <p class="mt-1">priority · {{ t('admin.promptAudit.policy.strategyHint') }}</p>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PromptAuditDraft, PromptAuditGroup, PromptAuditAccount } from '../types'
import { cloneData, SCANNER_CATALOG } from '../viewModel'

const props = defineProps<{ draft: PromptAuditDraft; groups: PromptAuditGroup[]; accounts?: PromptAuditAccount[] }>()
const emit = defineEmits<{ (event: 'update:draft', value: PromptAuditDraft): void }>()
const { t } = useI18n()
const groupSearch = ref('')
const accountSearch = ref('')
const eligibleAccounts = computed(() => (props.accounts ?? []).filter(account => account.platform === 'openai' && account.type === 'oauth'))
const filteredAccounts = computed(() => eligibleAccounts.value.filter(account => `${account.name} ${account.id}`.toLowerCase().includes(accountSearch.value.trim().toLowerCase())))
const missingAccountIds = computed(() => (props.draft.account_ids ?? []).filter(id => !eligibleAccounts.value.some(account => account.id === id)))
function toggleAccount(id: number) {
  const selected = new Set(props.draft.account_ids)
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
  patch({ account_ids: [...selected].sort((a, b) => a - b) })
}
const captureUserID = ref<number | undefined>()
const captureEmail = ref('')
const captureUsers = computed(() => props.draft.capture_users ?? [])

const filteredGroups = computed(() => {
  const query = groupSearch.value.trim().toLowerCase()
  if (!query) return props.groups
  return props.groups.filter((group) => `${group.name} ${group.id} ${group.platform}`.toLowerCase().includes(query))
})
const knownGroupIds = computed(() => new Set(props.groups.map((group) => group.id)))
const missingGroupIds = computed(() => props.draft.group_ids.filter((id) => !knownGroupIds.value.has(id)))

function patch(value: Partial<PromptAuditDraft>) {
  emit('update:draft', { ...cloneData(props.draft), ...value })
}
function toggleGroup(id: number) {
  const selected = new Set(props.draft.group_ids)
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
  patch({ group_ids: [...selected].sort((a, b) => a - b) })
}
function toggleScanner(id: string) {
  const selected = new Set(props.draft.scanners)
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
  patch({ scanners: SCANNER_CATALOG.map((item) => item.id).filter((item) => selected.has(item)) })
}
function addCaptureUser() {
  const userID = Number(captureUserID.value)
  const email = captureEmail.value.trim().toLowerCase()
  if (!Number.isInteger(userID) && !email) return
  const duplicate = captureUsers.value.some((item) => (userID > 0 && item.user_id === userID) || (email && item.email === email))
  if (duplicate) return
  patch({ capture_users: [...captureUsers.value, { user_id: userID > 0 ? userID : undefined, email: email || undefined }] })
  captureUserID.value = undefined
  captureEmail.value = ''
}
function removeCaptureUser(index: number) {
  patch({ capture_users: captureUsers.value.filter((_, current) => current !== index) })
}
function scannerLabel(id: string): string {
  return t(`admin.promptAudit.scanners.${id}`)
}
</script>
