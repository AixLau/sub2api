<template>
  <BaseDialog
    :show="show"
    :title="t('admin.proxies.bindAccountsTitle', { name: proxy?.name || '' })"
    width="wide"
    @close="close"
  >
    <div class="space-y-4">
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
            :placeholder="t('admin.proxies.searchAccounts')"
          />
        </div>
        <span class="shrink-0 text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.proxies.accountsSelected', { count: selectedIds.size }) }}
        </span>
      </div>

      <div class="min-h-64 overflow-hidden rounded-md border border-gray-200 dark:border-dark-700">
        <div v-if="loading" class="flex min-h-64 items-center justify-center text-sm text-gray-500">
          <Icon name="refresh" size="md" class="mr-2 animate-spin" />
          {{ t('common.loading') }}
        </div>
        <div v-else-if="accounts.length === 0" class="flex min-h-64 items-center justify-center text-sm text-gray-500">
          {{ t('admin.proxies.noBindableAccounts') }}
        </div>
        <div v-else class="max-h-96 overflow-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="sticky top-0 bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="w-10 px-3 py-2 text-left">
                  <input
                    type="checkbox"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    :checked="allSelectableOnPageSelected"
                    :disabled="selectableAccounts.length === 0"
                    @change="togglePageSelection"
                  />
                </th>
                <th class="px-3 py-2 text-left">{{ t('admin.proxies.accountName') }}</th>
                <th class="px-3 py-2 text-left">{{ t('admin.proxies.accountEmail') }}</th>
                <th class="px-3 py-2 text-left">{{ t('admin.accounts.columns.platformType') }}</th>
                <th class="px-3 py-2 text-left">{{ t('admin.proxies.currentProxy') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
              <tr
                v-for="account in accounts"
                :key="account.id"
                :class="isAccountDisabled(account) && 'opacity-60'"
              >
                <td class="px-3 py-2">
                  <input
                    type="checkbox"
                    :data-account-id="account.id"
                    class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    :checked="selectedIds.has(account.id)"
                    :disabled="isAccountDisabled(account)"
                    @change="toggleAccount(account.id)"
                  />
                </td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ account.extra?.email_address || account.extra?.email || account.credentials?.email || '--' }}</td>
                <td class="px-3 py-2">
                  <div class="font-medium text-gray-900 dark:text-white">{{ account.name }}</div>
                  <div v-if="account.parent_account_id" class="text-xs text-gray-500">
                    {{ t('admin.proxies.shadowProxyInherited') }}
                  </div>
                </td>
                <td class="px-3 py-2">
                  <PlatformTypeBadge :platform="account.platform" :type="account.type" />
                </td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-300">
                  <span v-if="account.proxy_id === proxy?.id" class="text-primary-600 dark:text-primary-400">
                    {{ t('admin.proxies.alreadyBound') }}
                  </span>
                  <span v-else>{{ account.proxy?.name || t('admin.proxies.directConnection') }}</span>
                </td>
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
          :disabled="submitting || selectedIds.size === 0"
          @click="bindAccounts"
        >
          <Icon v-if="submitting" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ t('admin.proxies.bindSelectedAccounts', { count: selectedIds.size }) }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Account, Proxy } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  show: boolean
  proxy: Proxy | null
}>()

const emit = defineEmits<{
  close: []
  bound: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const accounts = ref<Account[]>([])
const selectedIds = ref(new Set<number>())
const search = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(false)
const submitting = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | null = null
let loadController: AbortController | null = null

const isAccountDisabled = (account: Account) =>
  account.proxy_id === props.proxy?.id || account.parent_account_id != null

const selectableAccounts = computed(() => accounts.value.filter((account) => !isAccountDisabled(account)))
const allSelectableOnPageSelected = computed(() =>
  selectableAccounts.value.length > 0 && selectableAccounts.value.every((account) => selectedIds.value.has(account.id))
)

const loadAccounts = async () => {
  if (!props.show) return
  loadController?.abort()
  const controller = new AbortController()
  loadController = controller
  loading.value = true
  try {
    const result = await adminAPI.accounts.list(
      page.value,
      pageSize,
      search.value.trim() ? { search: search.value.trim() } : undefined,
      { signal: controller.signal }
    )
    accounts.value = result.items || []
    total.value = result.total || 0
  } catch (error: any) {
    if (error?.name !== 'CanceledError' && error?.code !== 'ERR_CANCELED') {
      appStore.showError(error.response?.data?.detail || t('admin.proxies.accountsLoadFailed'))
      console.error('Failed to load accounts for proxy binding:', error)
    }
  } finally {
    if (loadController === controller) loading.value = false
  }
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      search.value = ''
      page.value = 1
      selectedIds.value = new Set()
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

const toggleAccount = (accountId: number) => {
  const next = new Set(selectedIds.value)
  if (next.has(accountId)) next.delete(accountId)
  else next.add(accountId)
  selectedIds.value = next
}

const togglePageSelection = () => {
  const next = new Set(selectedIds.value)
  if (allSelectableOnPageSelected.value) {
    selectableAccounts.value.forEach((account) => next.delete(account.id))
  } else {
    selectableAccounts.value.forEach((account) => next.add(account.id))
  }
  selectedIds.value = next
}

const changePage = (nextPage: number) => {
  page.value = nextPage
  void loadAccounts()
}

const close = () => {
  if (!submitting.value) emit('close')
}

const bindAccounts = async () => {
  if (!props.proxy || selectedIds.value.size === 0) return
  submitting.value = true
  try {
    const result = await adminAPI.accounts.bulkUpdate(Array.from(selectedIds.value), {
      proxy_id: props.proxy.id
    })
    if (result.failed > 0) {
      const succeeded = new Set(result.success_ids || [])
      selectedIds.value = new Set(Array.from(selectedIds.value).filter((id) => !succeeded.has(id)))
      appStore.showError(t('admin.proxies.accountsBindPartial', { success: result.success, failed: result.failed }))
      void loadAccounts()
      return
    }
    appStore.showSuccess(t('admin.proxies.accountsBindSuccess', { count: result.success }))
    emit('bound')
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.proxies.accountsBindFailed'))
    console.error('Failed to bind accounts to proxy:', error)
  } finally {
    submitting.value = false
  }
}
</script>
