<template>
  <BaseDialog
    :show="show"
    :title="t('admin.plugins.selectAccountsTitle', { name: pluginName })"
    width="wide"
    @close="close"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-300">
        {{ t('admin.plugins.accountScopeHint') }}
      </p>
      <p v-if="editing" class="text-sm text-gray-600 dark:text-gray-300">
        {{ t('admin.plugins.updateAccountsHint') }}
      </p>

      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="relative flex-1">
          <Icon
            name="search"
            size="sm"
            class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
          />
          <input
            v-model="search"
            type="search"
            class="input pl-9"
            :placeholder="t('admin.plugins.searchAccounts')"
          />
        </div>
        <span class="shrink-0 text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.plugins.selectedAccountCount', { count: selectedIds.size }) }}
        </span>
      </div>

      <div class="min-h-64 overflow-hidden rounded-md border border-gray-200 dark:border-dark-700">
        <div v-if="loading" class="flex min-h-64 items-center justify-center text-sm text-gray-500">
          <Icon name="refresh" size="md" class="mr-2 animate-spin" />
          {{ t('common.loading') }}
        </div>
        <div
          v-else-if="accounts.length === 0"
          class="flex min-h-64 items-center justify-center px-6 text-center text-sm text-gray-500"
        >
          {{ t('admin.plugins.noOAuthAccounts') }}
        </div>
        <div v-else class="max-h-96 overflow-auto">
          <table class="min-w-full table-fixed divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="sticky top-0 bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="w-12 px-3 py-2 text-left">
                  <input
                    type="checkbox"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    :checked="allPageAccountsSelected"
                    :aria-label="t('admin.plugins.selectPageAccounts')"
                    @change="togglePageSelection"
                  />
                </th>
                <th class="w-5/12 px-3 py-2 text-left">{{ t('admin.plugins.accountName') }}</th>
                <th class="px-3 py-2 text-left">{{ t('admin.plugins.accountIdentity') }}</th>
                <th class="w-24 px-3 py-2 text-left">{{ t('admin.plugins.accountStatus') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
              <tr v-for="account in accounts" :key="account.id">
                <td class="px-3 py-2">
                  <input
                    type="checkbox"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    :checked="selectedIds.has(account.id)"
                    :aria-label="t('admin.plugins.selectAccount', { name: account.name })"
                    @change="toggleAccount(account.id)"
                  />
                </td>
                <td class="px-3 py-2">
                  <div class="truncate font-medium text-gray-900 dark:text-white">{{ account.name }}</div>
                  <div class="font-mono text-xs text-gray-500">#{{ account.id }}</div>
                </td>
                <td class="truncate px-3 py-2 text-gray-600 dark:text-gray-300" :title="accountIdentity(account)">
                  {{ accountIdentity(account) }}
                </td>
                <td class="px-3 py-2 text-xs text-gray-600 dark:text-gray-300">{{ account.status }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <Pagination
        v-if="total > 0"
        :page="page"
        :total="total"
        :page-size="pageSize"
        :show-page-size-selector="false"
        @update:page="changePage"
      />
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          data-test="confirm-plugin-accounts"
          :disabled="submitting || selectedIds.size === 0"
          @click="confirm"
        >
          <Icon v-if="submitting" name="refresh" size="sm" class="mr-2 animate-spin" />
          <Icon v-else :name="editing ? 'check' : 'play'" size="sm" />
          {{ t(editing ? 'admin.plugins.saveSelectedAccounts' : 'admin.plugins.enableSelectedAccounts', { count: selectedIds.size }) }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AccountListItem } from '@/types'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  show: boolean
  pluginName: string
  initialAccountIds: number[]
  submitting: boolean
  editing?: boolean
}>()

const emit = defineEmits<{
  close: []
  confirm: [accountIds: number[]]
}>()

const { t } = useI18n()
const appStore = useAppStore()
const accounts = ref<AccountListItem[]>([])
const selectedIds = ref(new Set<number>())
const search = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | null = null
let loadController: AbortController | null = null

const allPageAccountsSelected = computed(
  () => accounts.value.length > 0 && accounts.value.every((account) => selectedIds.value.has(account.id))
)

function accountIdentity(account: AccountListItem): string {
  const value = account.extra?.email_address || account.extra?.email || account.credentials?.email
  return typeof value === 'string' && value.trim() ? value : '--'
}

async function loadAccounts(): Promise<void> {
  if (!props.show) return
  loadController?.abort()
  const controller = new AbortController()
  loadController = controller
  loading.value = true
  try {
    const result = await adminAPI.accounts.list(
      page.value,
      pageSize,
      {
        platform: 'openai',
        type: 'oauth',
        search: search.value.trim() || undefined,
        lite: '1',
        sort_by: 'name',
        sort_order: 'asc'
      },
      { signal: controller.signal }
    )
    accounts.value = result.items || []
    total.value = result.total || 0
  } catch (error: any) {
    if (error?.name !== 'CanceledError' && error?.code !== 'ERR_CANCELED') {
      accounts.value = []
      total.value = 0
      appStore.showError(error?.response?.data?.detail || t('admin.plugins.accountsLoadFailed'))
    }
  } finally {
    if (loadController === controller) loading.value = false
  }
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      selectedIds.value = new Set(props.initialAccountIds)
      search.value = ''
      page.value = 1
      void loadAccounts()
    } else {
      loadController?.abort()
    }
  },
  { immediate: true }
)

watch(search, () => {
  if (!props.show) return
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    page.value = 1
    void loadAccounts()
  }, 300)
})

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
  loadController?.abort()
})

function toggleAccount(accountId: number): void {
  const next = new Set(selectedIds.value)
  if (next.has(accountId)) next.delete(accountId)
  else next.add(accountId)
  selectedIds.value = next
}

function togglePageSelection(): void {
  const next = new Set(selectedIds.value)
  if (allPageAccountsSelected.value) {
    accounts.value.forEach((account) => next.delete(account.id))
  } else {
    accounts.value.forEach((account) => next.add(account.id))
  }
  selectedIds.value = next
}

function changePage(nextPage: number): void {
  page.value = nextPage
  void loadAccounts()
}

function close(): void {
  if (!props.submitting) emit('close')
}

function confirm(): void {
  if (selectedIds.value.size === 0 || props.submitting) return
  emit('confirm', Array.from(selectedIds.value).sort((a, b) => a - b))
}
</script>
