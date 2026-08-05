<template>
  <BaseDialog :show="show" :title="t('admin.merchant.userBindings.title')" width="extra-wide" @close="emit('close')">
    <div v-if="user" class="space-y-5">
      <div class="flex items-center gap-3 rounded-lg bg-surface-subtle p-4">
        <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="min-w-0">
          <p class="truncate font-medium text-content-primary">{{ user.email }}</p>
          <p class="text-xs text-content-tertiary">#{{ user.id }}<span v-if="user.username"> · {{ user.username }}</span></p>
        </div>
      </div>

      <div v-if="loading" class="py-8 text-center text-sm text-content-secondary">{{ t('common.loading') }}</div>
      <div v-else-if="bindings.length === 0" class="py-8 text-center text-sm text-content-secondary">{{ t('admin.merchant.userBindings.empty') }}</div>
      <div v-else class="grid gap-5 lg:grid-cols-[minmax(220px,0.8fr)_minmax(0,1.6fr)]">
        <div class="space-y-2">
          <button
            v-for="binding in bindings"
            :key="binding.id"
            type="button"
            class="w-full rounded-md border p-3 text-left transition-colors"
            :class="selectedBinding?.id === binding.id ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-line-subtle hover:bg-surface-subtle'"
            @click="selectBinding(binding)"
          >
            <div class="flex items-center justify-between gap-2">
              <span class="truncate text-sm font-medium text-content-primary">{{ binding.integration_name || binding.integration_code }}</span>
              <span class="text-xs text-content-tertiary">{{ binding.status }}</span>
            </div>
            <div class="mt-1 truncate text-xs text-content-secondary">{{ binding.external_account || binding.external_user_id }}</div>
            <div class="mt-2 text-[11px] text-content-tertiary">{{ t('admin.merchant.userBindings.lastSync') }}: {{ formatDate(binding.last_recharge_sync_at) }}</div>
          </button>
        </div>

        <div v-if="selectedBinding" class="min-w-0">
          <div class="mb-3 flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
            <div>
              <h3 class="text-sm font-semibold text-content-primary">{{ t('admin.merchant.userBindings.records') }}</h3>
              <p class="mt-1 font-mono text-xs text-content-tertiary">{{ selectedBinding.external_user_id }}</p>
            </div>
            <button v-if="selectedBinding.recharge_sync_available" class="btn btn-secondary" type="button" :disabled="syncing" @click="syncRecords">
              <Icon name="sync" size="sm" class="mr-2" :class="syncing ? 'animate-spin' : ''" />
              {{ syncing ? t('admin.merchant.userBindings.syncing') : t('admin.merchant.userBindings.sync') }}
            </button>
          </div>
          <div v-if="recordsLoading" class="py-8 text-center text-sm text-content-secondary">{{ t('common.loading') }}</div>
          <div v-else-if="records.length === 0" class="rounded-md border border-dashed border-line-subtle p-6 text-center text-sm text-content-secondary">{{ t('admin.merchant.userBindings.noRecords') }}</div>
          <div v-else class="overflow-x-auto rounded-md border border-line-subtle">
            <table class="min-w-full divide-y divide-line-subtle text-sm">
              <thead class="bg-surface-subtle text-left text-xs text-content-tertiary">
                <tr>
                  <th class="px-3 py-2 font-medium">{{ t('admin.merchant.userBindings.orderNo') }}</th>
                  <th class="px-3 py-2 font-medium">{{ t('admin.merchant.userBindings.amount') }}</th>
                  <th class="px-3 py-2 font-medium">{{ t('admin.merchant.userBindings.status') }}</th>
                  <th class="px-3 py-2 font-medium">{{ t('admin.merchant.userBindings.createdAt') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-line-subtle">
                <tr v-for="record in records" :key="record.id">
                  <td class="whitespace-nowrap px-3 py-2 font-mono text-xs text-content-primary">{{ record.order_no }}</td>
                  <td class="whitespace-nowrap px-3 py-2 text-content-primary">{{ record.amount }} {{ record.currency }}</td>
                  <td class="whitespace-nowrap px-3 py-2 text-content-secondary">{{ record.status || '-' }}</td>
                  <td class="whitespace-nowrap px-3 py-2 text-xs text-content-secondary">{{ formatDate(record.created_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div v-else class="flex min-h-48 items-center justify-center rounded-md border border-dashed border-line-subtle text-sm text-content-secondary">{{ t('admin.merchant.userBindings.selectBinding') }}</div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { MerchantBinding, MerchantRechargeRecord } from '@/api/admin/merchantIntegrations'
import type { AdminUser } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits<{ (event: 'close'): void }>()
const { t } = useI18n()
const appStore = useAppStore()

const bindings = ref<MerchantBinding[]>([])
const selectedBinding = ref<MerchantBinding | null>(null)
const records = ref<MerchantRechargeRecord[]>([])
const loading = ref(false)
const recordsLoading = ref(false)
const syncing = ref(false)

function formatDate(value?: string): string {
  if (!value) return t('merchant.never')
  try {
    return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  } catch {
    return value
  }
}

async function load(user: AdminUser) {
  loading.value = true
  selectedBinding.value = null
  records.value = []
  try {
    bindings.value = await adminAPI.merchantIntegrations.listUserBindings(user.id)
    if (bindings.value.length) await selectBinding(bindings.value[0])
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.userBindings.loadError')))
  } finally {
    loading.value = false
  }
}

async function selectBinding(binding: MerchantBinding) {
  selectedBinding.value = binding
  recordsLoading.value = true
  try {
    const result = await adminAPI.merchantIntegrations.listUserRechargeRecords(binding.user_id, binding.id)
    records.value = result.items
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.userBindings.loadError')))
  } finally {
    recordsLoading.value = false
  }
}

async function syncRecords() {
  if (!selectedBinding.value) return
  syncing.value = true
  try {
    const result = await adminAPI.merchantIntegrations.syncUserRechargeRecords(selectedBinding.value.user_id, selectedBinding.value.id)
    records.value = result.records
    const updated = await adminAPI.merchantIntegrations.listUserBindings(selectedBinding.value.user_id)
    bindings.value = updated
    selectedBinding.value = updated.find(item => item.id === selectedBinding.value?.id) ?? selectedBinding.value
    appStore.showSuccess(t('admin.merchant.userBindings.syncSuccess', { count: result.synced }))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.userBindings.syncError')))
  } finally {
    syncing.value = false
  }
}

watch(
  () => props.show,
  (show) => {
    if (show && props.user) void load(props.user)
  },
  { immediate: true }
)
</script>
