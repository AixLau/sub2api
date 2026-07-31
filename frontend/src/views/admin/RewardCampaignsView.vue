<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="min-w-52 flex-1 sm:max-w-72">
            <input
              v-model.trim="search"
              type="search"
              class="input"
              :placeholder="t('admin.rewards.searchPlaceholder')"
              @input="scheduleSearch"
            />
          </div>
          <Select v-model="filters.status" :options="statusFilterOptions" class="w-40" @change="resetAndLoad" />
          <div class="flex flex-1 items-center justify-end gap-2">
            <button
              type="button"
              class="btn btn-secondary"
              :title="t('common.refresh')"
              :aria-label="t('common.refresh')"
              :disabled="loading"
              @click="loadCampaigns"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-secondary" @click="showSkinLibrary = true">
              <Icon name="grid" size="md" class="mr-1" />
              {{ t('admin.rewards.actions.skinLibrary') }}
            </button>
            <button type="button" class="btn btn-primary" @click="openCreate">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.rewards.actions.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="campaigns"
          :loading="loading"
        >
          <template #cell-name="{ row }">
            <button type="button" class="max-w-72 text-left" @click="openOperations(row)">
              <span class="block truncate font-medium text-gray-900 hover:text-primary-600 dark:text-white dark:hover:text-primary-400">
                {{ row.name }}
              </span>
              <span class="mt-1 flex items-center gap-1.5 text-xs text-gray-500 dark:text-dark-400">
                <span>#{{ row.id }}</span>
                <span>·</span>
                <span>v{{ row.current_version }}</span>
                <span v-if="row.description" class="max-w-40 truncate">· {{ row.description }}</span>
              </span>
            </button>
          </template>

          <template #cell-status="{ row }">
            <span :class="statusBadgeClass(row.status)">{{ statusLabel(row.status) }}</span>
          </template>

          <template #cell-issuance_mode="{ row }">
            <div class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <Icon :name="row.issuance_mode === 'on_access' ? 'bolt' : 'calendar'" size="sm" class="text-gray-400" />
              <span>{{ modeLabel(row.issuance_mode) }}</span>
            </div>
          </template>

          <template #cell-budget="{ row }">
            <div class="min-w-48">
              <div class="flex items-center justify-between gap-3 text-xs">
                <span class="font-medium text-gray-900 dark:text-white">{{ formatCurrency(row.spent_budget) }}</span>
                <span class="text-gray-500 dark:text-dark-400">{{ formatCurrency(row.total_budget) }}</span>
              </div>
              <div class="mt-1.5 h-1.5 overflow-hidden rounded bg-gray-100 dark:bg-dark-700">
                <div class="flex h-full">
                  <div class="bg-emerald-500" :style="{ width: `${budgetPercent(row.spent_budget, row.total_budget)}%` }"></div>
                  <div class="bg-amber-400" :style="{ width: `${budgetPercent(row.reserved_budget, row.total_budget)}%` }"></div>
                </div>
              </div>
              <div class="mt-1 flex items-center gap-3 text-[11px] text-gray-400 dark:text-dark-500">
                <span>{{ t('admin.rewards.budget.spent') }} {{ formatCurrency(row.spent_budget) }}</span>
                <span>{{ t('admin.rewards.budget.reserved') }} {{ formatCurrency(row.reserved_budget) }}</span>
              </div>
            </div>
          </template>

          <template #cell-time_window="{ row }">
            <div class="min-w-44 text-xs text-gray-600 dark:text-gray-300">
              <div>{{ formatDateTime(row.starts_at) }}</div>
              <div class="mt-1 text-gray-400 dark:text-dark-500">{{ formatDateTime(row.ends_at) }}</div>
              <div class="mt-1 text-gray-400 dark:text-dark-500">{{ row.timezone }}</div>
            </div>
          </template>

          <template #cell-rules="{ row }">
            <div class="min-w-36 text-xs text-gray-600 dark:text-gray-300">
              <div>{{ formatPercent(row.win_probability) }} · {{ t('admin.rewards.perUserCount', { count: row.max_grants_per_user }) }}</div>
              <div class="mt-1 text-gray-400 dark:text-dark-500">
                {{ row.audience?.any_of?.length
                  ? t('admin.rewards.audienceGroupCount', { count: row.audience.any_of.length })
                  : t('admin.rewards.allUsers') }}
              </div>
            </div>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-0.5">
              <button type="button" class="icon-action" :title="t('admin.rewards.actions.viewOperations')" @click="openOperations(row)">
                <Icon name="chartBar" size="sm" />
              </button>
              <button
                v-if="isEditable(row)"
                type="button"
                class="icon-action"
                :title="t('common.edit')"
                @click="openEdit(row)"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button type="button" class="icon-action" :title="t('admin.rewards.actions.clone')" @click="cloneCampaign(row)">
                <Icon name="copy" size="sm" />
              </button>
              <button
                v-if="row.status === 'draft'"
                type="button"
                class="icon-action text-emerald-600 dark:text-emerald-400"
                :title="t('admin.rewards.actions.publish')"
                @click="askAction(row, 'publish')"
              >
                <Icon name="play" size="sm" />
              </button>
              <button
                v-if="['scheduled', 'active'].includes(row.status)"
                type="button"
                class="icon-action text-amber-600 dark:text-amber-400"
                :title="t('admin.rewards.actions.pause')"
                @click="askAction(row, 'pause')"
              >
                <Icon name="ban" size="sm" />
              </button>
              <button
                v-if="row.status === 'paused'"
                type="button"
                class="icon-action text-emerald-600 dark:text-emerald-400"
                :title="t('admin.rewards.actions.resume')"
                @click="askAction(row, 'resume')"
              >
                <Icon name="play" size="sm" />
              </button>
              <button
                v-if="['scheduled', 'active', 'paused'].includes(row.status)"
                type="button"
                class="icon-action text-red-600 dark:text-red-400"
                :title="t('admin.rewards.actions.end')"
                @click="askAction(row, 'end')"
              >
                <Icon name="xCircle" size="sm" />
              </button>
              <button
                v-if="row.status === 'ended'"
                type="button"
                class="icon-action"
                :title="t('admin.rewards.actions.archive')"
                @click="askAction(row, 'archive')"
              >
                <Icon name="inbox" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.rewards.emptyTitle')"
              :description="t('admin.rewards.emptyDescription')"
              :action-text="t('admin.rewards.actions.create')"
              @action="openCreate"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :page-size="pagination.page_size"
          :total="pagination.total"
          @update:page="changePage"
          @update:pageSize="changePageSize"
        />
      </template>
    </TablePageLayout>

    <RewardCampaignEditorDialog
      :show="showEditor"
      :campaign="editingCampaign"
      :skins="skins"
      :subscription-groups="subscriptionGroups"
      :estimate="estimate"
      :saving="saving"
      :estimating="estimating"
      :uploading-skin="uploadingSkin"
      :skin-reset-token="skinResetToken"
      @close="closeEditor"
      @save="saveCampaign"
      @estimate="estimateCampaign"
      @upload-skin="uploadSkin"
    />

    <RewardCampaignOperationsDialog
      :show="showOperations"
      :campaign="operationsCampaign"
      :refresh-token="operationsRefreshToken"
      @close="closeOperations"
      @run-batch="(campaign) => askAction(campaign, 'batch')"
    />

    <RewardSkinLibraryDialog
      :show="showSkinLibrary"
      :skins="skins"
      :uploading="uploadingSkin"
      :reset-token="skinResetToken"
      @close="showSkinLibrary = false"
      @upload="uploadSkin"
      @update="updateSkin"
      @archive="archiveSkin"
    />

    <ConfirmDialog
      :show="!!pendingAction"
      :title="pendingAction ? actionTitle(pendingAction.action) : ''"
      :message="pendingAction ? actionMessage(pendingAction.campaign, pendingAction.action) : ''"
      :confirm-text="pendingAction ? actionLabel(pendingAction.action) : ''"
      :danger="pendingAction?.action === 'end'"
      @confirm="confirmAction"
      @cancel="pendingAction = null"
    />

    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import rewardsAPI, {
  type RewardAudienceEstimate,
  type RewardCampaign,
  type RewardCampaignDraft,
  type RewardCampaignStatus,
  type RewardIssuanceMode,
  type RewardSkin,
  type RewardSkinUploadMetadata
} from '@/api/admin/rewards'
import groupsAPI from '@/api/admin/groups'
import { formatCurrency, formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/types'

import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import RewardCampaignEditorDialog from '@/components/admin/rewards/RewardCampaignEditorDialog.vue'
import RewardCampaignOperationsDialog from '@/components/admin/rewards/RewardCampaignOperationsDialog.vue'
import RewardSkinLibraryDialog from '@/components/admin/rewards/RewardSkinLibraryDialog.vue'

type CampaignAction = 'publish' | 'pause' | 'resume' | 'end' | 'archive' | 'batch'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const campaigns = ref<RewardCampaign[]>([])
const skins = ref<RewardSkin[]>([])
const subscriptionGroups = ref<Array<{ id: number; name: string }>>([])
const loading = ref(false)
const saving = ref(false)
const estimating = ref(false)
const uploadingSkin = ref(false)
const search = ref('')
const estimate = ref<RewardAudienceEstimate | null>(null)
const showEditor = ref(false)
const editingCampaign = ref<RewardCampaign | null>(null)
const showOperations = ref(false)
const operationsCampaign = ref<RewardCampaign | null>(null)
const showSkinLibrary = ref(false)
const operationsRefreshToken = ref(0)
const skinResetToken = ref(0)
const pendingAction = ref<{ campaign: RewardCampaign; action: CampaignAction } | null>(null)

const filters = reactive<{ status: RewardCampaignStatus | '' }>({ status: '' })

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.rewards.filters.allStatuses') },
  ...(['draft', 'scheduled', 'active', 'paused', 'ended', 'archived'] as RewardCampaignStatus[])
    .map((status) => ({ value: status, label: statusLabel(status) }))
])

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.rewards.columns.campaign') },
  { key: 'status', label: t('admin.rewards.columns.status') },
  { key: 'issuance_mode', label: t('admin.rewards.columns.mode') },
  { key: 'budget', label: t('admin.rewards.columns.budget') },
  { key: 'time_window', label: t('admin.rewards.columns.timeWindow') },
  { key: 'rules', label: t('admin.rewards.columns.rules') },
  { key: 'actions', label: t('admin.rewards.columns.actions') }
])

let listController: AbortController | null = null
let searchTimer: number | null = null
let estimateController: AbortController | null = null

async function loadCampaigns() {
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  loading.value = true
  try {
    const response = await rewardsAPI.listCampaigns({
      page: pagination.page,
      page_size: pagination.page_size,
      status: filters.status,
      search: search.value || undefined
    }, { signal: controller.signal })
    if (controller.signal.aborted || controller !== listController) return
    campaigns.value = response.items
    pagination.page = response.page
    pagination.page_size = response.page_size
    pagination.total = response.total
    pagination.pages = response.pages
  } catch (cause: any) {
    if (controller.signal.aborted || cause?.code === 'ERR_CANCELED') return
    console.error('Failed to load reward campaigns:', cause)
    appStore.showError(cause?.message || t('admin.rewards.messages.loadFailed'))
  } finally {
    if (listController === controller) {
      loading.value = false
      listController = null
    }
  }
}

async function loadReferenceData() {
  const [skinsResult, groupsResult] = await Promise.allSettled([
    rewardsAPI.listSkins({ page: 1, page_size: 200 }),
    groupsAPI.getAll()
  ])
  if (skinsResult.status === 'fulfilled') skins.value = skinsResult.value.items
  if (groupsResult.status === 'fulfilled') {
    subscriptionGroups.value = (groupsResult.value ?? [])
      .filter((group: any) => group.subscription_type === 'subscription')
      .map((group: any) => ({ id: group.id, name: group.name }))
  }
}

function resetAndLoad() {
  pagination.page = 1
  loadCampaigns()
}

function scheduleSearch() {
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(resetAndLoad, 300)
}

function changePage(page: number) {
  pagination.page = page
  loadCampaigns()
}

function changePageSize(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  loadCampaigns()
}

function openCreate() {
  editingCampaign.value = null
  estimate.value = null
  showEditor.value = true
}

function openEdit(campaign: RewardCampaign) {
  editingCampaign.value = campaign
  estimate.value = null
  showEditor.value = true
}

function closeEditor() {
  showEditor.value = false
  editingCampaign.value = null
  estimate.value = null
  estimateController?.abort()
}

function openOperations(campaign: RewardCampaign) {
  operationsCampaign.value = campaign
  showOperations.value = true
}

function closeOperations() {
  showOperations.value = false
  operationsCampaign.value = null
}

async function saveCampaign(payload: RewardCampaignDraft, publishAfterSave: boolean) {
  saving.value = true
  try {
    const current = editingCampaign.value
    const action = () => current
      ? rewardsAPI.updateCampaign(current.id, payload)
      : rewardsAPI.createCampaign(payload)
    const saved = current ? await stepUp.run(action) : await action()

    let finalCampaign = saved
    if (publishAfterSave) {
      finalCampaign = await stepUp.run(() => rewardsAPI.publishCampaign(saved.id))
    }

    appStore.showSuccess(
      publishAfterSave
        ? t('admin.rewards.messages.published')
        : current
          ? t('admin.rewards.messages.updated')
          : t('admin.rewards.messages.created')
    )
    closeEditor()
    await loadCampaigns()
    if (operationsCampaign.value?.id === finalCampaign.id) {
      operationsCampaign.value = finalCampaign
      operationsRefreshToken.value++
    }
  } catch (cause: any) {
    handleActionError(cause, t('admin.rewards.messages.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function estimateCampaign(payload: RewardCampaignDraft) {
  estimateController?.abort()
  const controller = new AbortController()
  estimateController = controller
  estimating.value = true
  try {
    const result = await rewardsAPI.estimateAudience(payload, { signal: controller.signal })
    if (!controller.signal.aborted && controller === estimateController) estimate.value = result
  } catch (cause: any) {
    if (controller.signal.aborted || cause?.code === 'ERR_CANCELED') return
    console.error('Failed to estimate reward audience:', cause)
    appStore.showError(cause?.message || t('admin.rewards.messages.estimateFailed'))
  } finally {
    if (estimateController === controller) {
      estimating.value = false
      estimateController = null
    }
  }
}

async function cloneCampaign(campaign: RewardCampaign) {
  try {
    const clone = await rewardsAPI.cloneCampaign(campaign.id)
    appStore.showSuccess(t('admin.rewards.messages.cloned'))
    await loadCampaigns()
    openEdit(clone)
  } catch (cause: any) {
    handleActionError(cause, t('admin.rewards.messages.cloneFailed'))
  }
}

function askAction(campaign: RewardCampaign, action: CampaignAction) {
  pendingAction.value = { campaign, action }
}

async function confirmAction() {
  const pending = pendingAction.value
  if (!pending) return
  pendingAction.value = null
  try {
    if (pending.action === 'batch') {
      await stepUp.run(() => rewardsAPI.createCampaignJob(pending.campaign.id))
      operationsRefreshToken.value++
    } else {
      const apiCall = {
        publish: rewardsAPI.publishCampaign,
        pause: rewardsAPI.pauseCampaign,
        resume: rewardsAPI.resumeCampaign,
        end: rewardsAPI.endCampaign,
        archive: rewardsAPI.archiveCampaign
      }[pending.action]
      const sensitive = ['publish', 'resume', 'end'].includes(pending.action)
      const updated = sensitive
        ? await stepUp.run(() => apiCall(pending.campaign.id))
        : await apiCall(pending.campaign.id)
      if (operationsCampaign.value?.id === updated.id) {
        operationsCampaign.value = updated
        operationsRefreshToken.value++
      }
      await loadCampaigns()
    }
    appStore.showSuccess(t(`admin.rewards.messages.${pending.action}Success`))
  } catch (cause: any) {
    handleActionError(cause, t(`admin.rewards.messages.${pending.action}Failed`))
  }
}

async function uploadSkin(
  file: File,
  metadata: RewardSkinUploadMetadata,
  _canvasFallback: boolean
) {
  uploadingSkin.value = true
  try {
    const skin = await rewardsAPI.uploadSkin(file, metadata)
    skins.value = [skin, ...skins.value.filter((item) => item.id !== skin.id)]
    skinResetToken.value++
    appStore.showSuccess(t('admin.rewards.messages.skinUploaded'))
  } catch (cause: any) {
    handleActionError(cause, t('admin.rewards.messages.skinUploadFailed'))
  } finally {
    uploadingSkin.value = false
  }
}

async function updateSkin(
  skin: RewardSkin,
  payload: Pick<RewardSkin, 'name' | 'description' | 'alt_text' | 'status'>
) {
  try {
    const updated = await rewardsAPI.updateSkin(skin.id, payload)
    replaceSkin(updated)
    appStore.showSuccess(t('admin.rewards.messages.skinUpdated'))
  } catch (cause: any) {
    handleActionError(cause, t('admin.rewards.messages.skinUpdateFailed'))
  }
}

async function archiveSkin(skin: RewardSkin) {
  try {
    const updated = await rewardsAPI.archiveSkin(skin.id)
    replaceSkin(updated)
    appStore.showSuccess(t('admin.rewards.messages.skinArchived'))
  } catch (cause: any) {
    handleActionError(cause, t('admin.rewards.messages.skinArchiveFailed'))
  }
}

function replaceSkin(updated: RewardSkin) {
  const index = skins.value.findIndex((skin) => skin.id === updated.id)
  if (index >= 0) skins.value.splice(index, 1, updated)
  else skins.value.unshift(updated)
}

function handleActionError(cause: any, fallback: string) {
  if (isStepUpCancelled(cause)) return
  if (isStepUpBlocked(cause)) {
    appStore.showError(
      stepUpBlockReason(cause) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
        ? t('stepUp.adminApiKeyForbidden')
        : t('stepUp.notEnabled')
    )
    return
  }
  console.error('Reward campaign action failed:', cause)
  appStore.showError(cause?.message || fallback)
}

function isEditable(campaign: RewardCampaign) {
  if (
    campaign.issuance_mode === 'scheduled_batch' &&
    ['scheduled', 'active'].includes(campaign.status)
  ) {
    return false
  }
  return ['draft', 'scheduled', 'active', 'paused'].includes(campaign.status)
}

function statusLabel(status: RewardCampaignStatus) {
  return t(`admin.rewards.statuses.${status}`)
}

function modeLabel(mode: RewardIssuanceMode) {
  return mode === 'on_access' ? t('admin.rewards.modes.onAccess') : t('admin.rewards.modes.scheduledBatch')
}

function statusBadgeClass(status: RewardCampaignStatus) {
  const classes: Record<RewardCampaignStatus, string> = {
    draft: 'badge badge-gray',
    scheduled: 'badge badge-primary',
    active: 'badge badge-success',
    paused: 'badge badge-warning',
    ended: 'badge badge-gray',
    archived: 'badge badge-gray'
  }
  return classes[status]
}

function budgetPercent(amount: number, total: number) {
  return Math.max(0, Math.min(100, total > 0 ? (Number(amount) / Number(total)) * 100 : 0))
}

function formatPercent(probability: number) {
  return `${(Number(probability) * 100).toFixed(Number(probability) * 100 % 1 ? 1 : 0)}%`
}

function actionLabel(action: CampaignAction) {
  return t(`admin.rewards.actions.${action}`)
}

function actionTitle(action: CampaignAction) {
  return t('admin.rewards.confirm.title', { action: actionLabel(action) })
}

function actionMessage(campaign: RewardCampaign, action: CampaignAction) {
  return t(`admin.rewards.confirm.${action}`, { name: campaign.name })
}

onMounted(async () => {
  await loadReferenceData()
  await loadCampaigns()
})

onUnmounted(() => {
  listController?.abort()
  estimateController?.abort()
  if (searchTimer) window.clearTimeout(searchTimer)
})
</script>

<style scoped>
.icon-action {
  @apply inline-flex h-8 w-8 flex-none items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-gray-200;
}
</style>
