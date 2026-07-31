<template>
  <BaseDialog
    :show="show"
    :title="campaign ? t('admin.rewards.operations.title', { name: campaign.name }) : t('admin.rewards.operations.fallbackTitle')"
    width="extra-wide"
    @close="emit('close')"
  >
    <div v-if="campaign" class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <span :class="statusBadgeClass(campaign.status)">
            {{ statusLabel(campaign.status) }}
          </span>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.rewards.operations.version', { version: campaign.current_version }) }}
          </span>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ modeLabel(campaign.issuance_mode) }}
          </span>
        </div>
        <button
          v-if="campaign.issuance_mode === 'scheduled_batch' && ['scheduled', 'active', 'paused'].includes(campaign.status)"
          type="button"
          class="btn btn-primary"
          @click="emit('runBatch', campaign)"
        >
          <Icon name="play" size="sm" class="mr-1" />
          {{ t('admin.rewards.actions.runBatch') }}
        </button>
      </div>

      <nav class="flex gap-1 border-b border-gray-200 dark:border-dark-700">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          :class="tabClass(tab.key)"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
        </button>
      </nav>

      <div v-if="loading" class="flex min-h-72 items-center justify-center">
        <LoadingSpinner />
      </div>

      <template v-else>
        <section v-if="activeTab === 'stats'" class="space-y-5">
          <div class="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-gray-200 bg-gray-200 dark:border-dark-700 dark:bg-dark-700 lg:grid-cols-4">
            <div v-for="metric in budgetMetrics" :key="metric.label" class="bg-white px-4 py-4 dark:bg-dark-900">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ metric.label }}</p>
              <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ metric.value }}</p>
            </div>
          </div>

          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.rewards.operations.funnelTitle') }}
            </h4>
            <div class="mt-3 grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7">
              <div v-for="stage in funnelStages" :key="stage.label" class="min-w-0">
                <div class="mb-1 flex items-center justify-between gap-2 text-xs">
                  <span class="truncate text-gray-500 dark:text-dark-400">{{ stage.label }}</span>
                  <span class="font-medium text-gray-900 dark:text-white">{{ formatNumber(stage.value) }}</span>
                </div>
                <div class="h-2 overflow-hidden rounded bg-gray-100 dark:bg-dark-700">
                  <div class="h-full rounded bg-primary-500" :style="{ width: `${stage.percent}%` }"></div>
                </div>
                <p class="mt-1 text-[11px] text-gray-400">{{ stage.percent.toFixed(1) }}%</p>
              </div>
            </div>
          </div>

          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.rewards.operations.distributionTitle') }}
            </h4>
            <div class="mt-3 overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <table class="w-full min-w-[520px] text-sm">
                <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-900 dark:text-dark-400">
                  <tr>
                    <th class="px-4 py-3">{{ t('admin.rewards.operations.amount') }}</th>
                    <th class="px-4 py-3">{{ t('admin.rewards.operations.grantCount') }}</th>
                    <th class="px-4 py-3">{{ t('admin.rewards.operations.distributedTotal') }}</th>
                    <th class="px-4 py-3">{{ t('admin.rewards.operations.share') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                  <tr v-for="item in stats?.amount_distribution ?? []" :key="item.amount">
                    <td class="px-4 py-3 font-medium text-gray-900 dark:text-white">{{ formatCurrency(item.amount) }}</td>
                    <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ formatNumber(item.count) }}</td>
                    <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ formatCurrency(item.total) }}</td>
                    <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ distributionShare(item.count) }}%</td>
                  </tr>
                  <tr v-if="!stats?.amount_distribution?.length">
                    <td colspan="4" class="px-4 py-8 text-center text-gray-500 dark:text-dark-400">
                      {{ t('admin.rewards.operations.noDistribution') }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>

        <section v-else-if="activeTab === 'grants'">
          <div class="mb-3 flex flex-wrap items-center gap-2">
            <input
              v-model.trim="grantSearch"
              type="search"
              class="input max-w-64"
              :placeholder="t('admin.rewards.operations.searchGrants')"
              @keyup.enter="loadGrants"
            />
            <Select v-model="grantStatus" :options="grantStatusOptions" class="w-40" @change="loadGrants" />
            <button type="button" class="btn btn-secondary" @click="loadGrants">
              <Icon name="search" size="sm" />
            </button>
          </div>
          <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
            <table class="w-full min-w-[880px] text-sm">
              <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-900 dark:text-dark-400">
                <tr>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.grantId') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.user') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.status') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.amount') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.source') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.createdAt') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.claimedAt') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr v-for="grant in grants" :key="grant.id">
                  <td class="max-w-44 truncate px-4 py-3 font-mono text-xs text-gray-600 dark:text-gray-300" :title="grant.id">{{ grant.id }}</td>
                  <td class="px-4 py-3">
                    <span class="font-medium text-gray-900 dark:text-white">#{{ grant.user_id }}</span>
                    <span v-if="grant.user_email" class="ml-1 text-xs text-gray-500">{{ grant.user_email }}</span>
                  </td>
                  <td class="px-4 py-3">
                    <span :class="grantBadgeClass(grant.status)">{{ grantStatusLabel(grant.status) }}</span>
                  </td>
                  <td class="px-4 py-3 font-medium text-gray-900 dark:text-white">{{ formatCurrency(grant.amount) }}</td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ grant.source }}</td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ formatDateTime(grant.created_at) }}</td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ grant.claimed_at ? formatDateTime(grant.claimed_at) : '-' }}</td>
                </tr>
                <tr v-if="!grants.length">
                  <td colspan="7" class="px-4 py-8 text-center text-gray-500 dark:text-dark-400">
                    {{ t('admin.rewards.operations.noGrants') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section v-else>
          <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
            <table class="w-full min-w-[900px] text-sm">
              <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-900 dark:text-dark-400">
                <tr>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.jobId') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.status') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.versionColumn') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.progress') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.granted') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.attempts') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.scheduledAt') }}</th>
                  <th class="px-4 py-3">{{ t('admin.rewards.operations.lastError') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr v-for="job in jobs" :key="job.id">
                  <td class="max-w-40 truncate px-4 py-3 font-mono text-xs text-gray-600 dark:text-gray-300" :title="job.id">{{ job.id }}</td>
                  <td class="px-4 py-3"><span :class="jobBadgeClass(job.status)">{{ jobStatusLabel(job.status) }}</span></td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">v{{ job.campaign_version }}</td>
                  <td class="px-4 py-3">
                    <span class="font-medium text-gray-900 dark:text-white">{{ formatNumber(job.processed_users) }}</span>
                    <span class="text-gray-500"> / {{ formatNumber(job.matched_users) }}</span>
                  </td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ formatNumber(job.granted_users) }}</td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ job.attempts }}</td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ formatDateTime(job.scheduled_at) }}</td>
                  <td class="max-w-56 truncate px-4 py-3 text-red-600 dark:text-red-400" :title="job.last_error ?? ''">{{ job.last_error || '-' }}</td>
                </tr>
                <tr v-if="!jobs.length">
                  <td colspan="8" class="px-4 py-8 text-center text-gray-500 dark:text-dark-400">
                    {{ t('admin.rewards.operations.noJobs') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>

      <p v-if="error" class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
        {{ error }}
      </p>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import rewardsAPI, {
  type RewardCampaign,
  type RewardCampaignJob,
  type RewardCampaignStats,
  type RewardGrant,
  type RewardGrantStatus,
  type RewardJobStatus
} from '@/api/admin/rewards'
import { formatCurrency, formatDateTime, formatNumber } from '@/utils/format'

type OperationsTab = 'stats' | 'grants' | 'jobs'

const props = withDefaults(defineProps<{
  show: boolean
  campaign?: RewardCampaign | null
  refreshToken?: number
}>(), {
  campaign: null,
  refreshToken: 0
})

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'runBatch', campaign: RewardCampaign): void
}>()

const { t } = useI18n()
const activeTab = ref<OperationsTab>('stats')
const loading = ref(false)
const error = ref('')
const stats = ref<RewardCampaignStats | null>(null)
const grants = ref<RewardGrant[]>([])
const jobs = ref<RewardCampaignJob[]>([])
const grantStatus = ref<RewardGrantStatus | ''>('')
const grantSearch = ref('')

const tabs = computed<Array<{ key: OperationsTab; label: string }>>(() => [
  { key: 'stats', label: t('admin.rewards.operations.tabs.stats') },
  { key: 'grants', label: t('admin.rewards.operations.tabs.grants') },
  { key: 'jobs', label: t('admin.rewards.operations.tabs.jobs') }
])

const grantStatusOptions = computed(() => [
  { value: '', label: t('admin.rewards.filters.allStatuses') },
  { value: 'pending', label: t('admin.rewards.grantStatuses.pending') },
  { value: 'claimed', label: t('admin.rewards.grantStatuses.claimed') },
  { value: 'expired', label: t('admin.rewards.grantStatuses.expired') },
  { value: 'cancelled', label: t('admin.rewards.grantStatuses.cancelled') }
])

const budgetMetrics = computed(() => [
  { label: t('admin.rewards.budget.total'), value: formatCurrency(stats.value?.total_budget ?? props.campaign?.total_budget) },
  { label: t('admin.rewards.budget.reserved'), value: formatCurrency(stats.value?.reserved_budget ?? props.campaign?.reserved_budget) },
  { label: t('admin.rewards.budget.spent'), value: formatCurrency(stats.value?.spent_budget ?? props.campaign?.spent_budget) },
  { label: t('admin.rewards.budget.released'), value: formatCurrency(stats.value?.released_budget ?? props.campaign?.released_budget) }
])

const funnelStages = computed(() => {
  const base = Math.max(1, stats.value?.evaluated ?? 0)
  const stages = [
    { label: t('admin.rewards.funnel.evaluated'), value: stats.value?.evaluated ?? 0 },
    { label: t('admin.rewards.funnel.granted'), value: stats.value?.granted ?? 0 },
    { label: t('admin.rewards.funnel.viewed'), value: stats.value?.viewed ?? 0 },
    { label: t('admin.rewards.funnel.scratched'), value: stats.value?.scratched ?? 0 },
    { label: t('admin.rewards.funnel.claimed'), value: stats.value?.claimed ?? 0 },
    { label: t('admin.rewards.funnel.pending'), value: stats.value?.pending ?? 0 },
    { label: t('admin.rewards.funnel.expired'), value: stats.value?.expired ?? 0 }
  ]
  return stages.map((stage) => ({ ...stage, percent: Math.min(100, (stage.value / base) * 100) }))
})

watch(
  () => [props.show, props.campaign?.id, props.refreshToken, activeTab.value] as const,
  async ([show]) => {
    if (!show || !props.campaign) return
    await loadActiveTab()
  },
  { immediate: true }
)

async function loadActiveTab() {
  if (!props.campaign) return
  loading.value = true
  error.value = ''
  try {
    if (activeTab.value === 'stats') {
      stats.value = await rewardsAPI.getCampaignStats(props.campaign.id)
    } else if (activeTab.value === 'grants') {
      await loadGrants()
    } else {
      const response = await rewardsAPI.listCampaignJobs(props.campaign.id, { page: 1, page_size: 50 })
      jobs.value = response.items
    }
  } catch (cause: any) {
    console.error('Failed to load reward campaign operations:', cause)
    error.value = cause?.message || t('admin.rewards.operations.loadFailed')
  } finally {
    loading.value = false
  }
}

async function loadGrants() {
  if (!props.campaign) return
  loading.value = true
  error.value = ''
  try {
    const response = await rewardsAPI.listCampaignGrants(props.campaign.id, {
      page: 1,
      page_size: 50,
      status: grantStatus.value,
      search: grantSearch.value || undefined
    })
    grants.value = response.items
  } catch (cause: any) {
    console.error('Failed to load reward grants:', cause)
    error.value = cause?.message || t('admin.rewards.operations.loadFailed')
  } finally {
    loading.value = false
  }
}

function distributionShare(count: number) {
  const total = stats.value?.amount_distribution.reduce((sum, item) => sum + item.count, 0) ?? 0
  return total ? ((count / total) * 100).toFixed(1) : '0.0'
}

function modeLabel(mode: RewardCampaign['issuance_mode']) {
  return mode === 'on_access' ? t('admin.rewards.modes.onAccess') : t('admin.rewards.modes.scheduledBatch')
}

function statusLabel(status: RewardCampaign['status']) {
  return t(`admin.rewards.statuses.${status}`)
}

function grantStatusLabel(status: RewardGrantStatus) {
  return t(`admin.rewards.grantStatuses.${status}`)
}

function jobStatusLabel(status: RewardJobStatus) {
  return t(`admin.rewards.jobStatuses.${status}`)
}

function statusBadgeClass(status: RewardCampaign['status']) {
  const colors: Record<RewardCampaign['status'], string> = {
    draft: 'badge badge-gray',
    scheduled: 'badge badge-primary',
    active: 'badge badge-success',
    paused: 'badge badge-warning',
    ended: 'badge badge-gray',
    archived: 'badge badge-gray'
  }
  return colors[status]
}

function grantBadgeClass(status: RewardGrantStatus) {
  return status === 'claimed'
    ? 'badge badge-success'
    : status === 'pending'
      ? 'badge badge-primary'
      : status === 'expired'
        ? 'badge badge-warning'
        : 'badge badge-gray'
}

function jobBadgeClass(status: RewardJobStatus) {
  if (status === 'completed') return 'badge badge-success'
  if (status === 'running') return 'badge badge-primary'
  if (status === 'failed' || status === 'dead') return 'badge badge-danger'
  if (status === 'paused') return 'badge badge-warning'
  return 'badge badge-gray'
}

function tabClass(tab: OperationsTab) {
  return [
    '-mb-px border-b-2 px-4 py-3 text-sm font-medium transition-colors',
    activeTab.value === tab
      ? 'border-primary-500 text-primary-700 dark:text-primary-300'
      : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white'
  ]
}
</script>
