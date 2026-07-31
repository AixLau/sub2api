<template>
  <BaseDialog
    :show="show"
    :title="dialogTitle"
    width="full"
    :close-on-click-outside="false"
    @close="emit('close')"
  >
    <form
      id="reward-campaign-form"
      class="min-h-[560px]"
      novalidate
      @submit.prevent="submit(false)"
    >
      <div class="border-b border-gray-200 dark:border-dark-700">
        <nav class="-mb-px flex gap-1 overflow-x-auto" :aria-label="t('admin.rewards.editor.sectionsLabel')">
          <button
            v-for="(section, index) in sections"
            :key="section.key"
            type="button"
            :class="sectionTabClass(section.key)"
            @click="activeSection = section.key"
          >
            <span
              :class="[
                'flex h-5 w-5 items-center justify-center rounded text-[11px] font-semibold',
                activeSection === section.key
                  ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-400'
              ]"
            >
              {{ index + 1 }}
            </span>
            <span class="whitespace-nowrap">{{ section.label }}</span>
          </button>
        </nav>
      </div>

      <div class="py-5">
        <section v-show="activeSection === 'basic'" class="space-y-5">
          <SectionHeading
            :title="t('admin.rewards.editor.basic.title')"
            :description="t('admin.rewards.editor.basic.description')"
          />

          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <div class="lg:col-span-2">
              <label class="input-label">{{ t('admin.rewards.editor.basic.name') }}</label>
              <input
                v-model.trim="form.name"
                type="text"
                class="input"
                maxlength="120"
                required
                :placeholder="t('admin.rewards.editor.basic.namePlaceholder')"
              />
            </div>
            <div class="lg:col-span-2">
              <label class="input-label">{{ t('admin.rewards.editor.basic.descriptionLabel') }}</label>
              <textarea
                v-model.trim="form.description"
                rows="3"
                class="input"
                :placeholder="t('admin.rewards.editor.basic.descriptionPlaceholder')"
              ></textarea>
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.basic.mode') }}</label>
              <div class="grid grid-cols-2 gap-2">
                <label :class="choiceClass(form.issuance_mode === 'on_access')">
                  <input
                    v-model="form.issuance_mode"
                    type="radio"
                    value="on_access"
                    class="sr-only"
                  />
                  <Icon name="bolt" size="md" />
                  <span>
                    <span class="block text-sm font-medium">{{ t('admin.rewards.modes.onAccess') }}</span>
                    <span class="mt-0.5 block text-xs opacity-70">{{ t('admin.rewards.editor.basic.onAccessHint') }}</span>
                  </span>
                </label>
                <label :class="choiceClass(form.issuance_mode === 'scheduled_batch')">
                  <input
                    v-model="form.issuance_mode"
                    type="radio"
                    value="scheduled_batch"
                    class="sr-only"
                  />
                  <Icon name="calendar" size="md" />
                  <span>
                    <span class="block text-sm font-medium">{{ t('admin.rewards.modes.scheduledBatch') }}</span>
                    <span class="mt-0.5 block text-xs opacity-70">{{ t('admin.rewards.editor.basic.batchHint') }}</span>
                  </span>
                </label>
              </div>
            </div>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.basic.priority') }}</label>
                <input v-model.number="form.priority" type="number" min="0" max="1000" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.basic.probability') }}</label>
                <div class="relative">
                  <input
                    :value="probabilityPercent"
                    type="number"
                    min="0"
                    max="100"
                    step="0.01"
                    class="input pr-8"
                    @input="setProbability(($event.target as HTMLInputElement).value)"
                  />
                  <span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-gray-400">%</span>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section v-show="activeSection === 'schedule'" class="space-y-5">
          <SectionHeading
            :title="t('admin.rewards.editor.schedule.title')"
            :description="t('admin.rewards.editor.schedule.description')"
          />

          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <div class="lg:col-span-2">
              <label class="input-label">{{ t('admin.rewards.editor.schedule.timezone') }}</label>
              <input
                v-model.trim="form.timezone"
                list="reward-campaign-timezones"
                class="input"
                autocomplete="off"
                required
              />
              <datalist id="reward-campaign-timezones">
                <option v-for="timezone in timezoneOptions" :key="timezone" :value="timezone" />
              </datalist>
              <p class="input-hint">{{ t('admin.rewards.editor.schedule.timezoneHint') }}</p>
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.schedule.startsAt') }}</label>
              <input v-model="form.starts_at" type="datetime-local" class="input" required />
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.schedule.endsAt') }}</label>
              <input v-model="form.ends_at" type="datetime-local" class="input" required />
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.schedule.evaluationInterval') }}</label>
              <div class="relative">
                <input
                  v-model.number="form.evaluation_interval_minutes"
                  type="number"
                  min="1"
                  step="1"
                  class="input pr-20"
                />
                <span class="field-suffix">{{ t('admin.rewards.units.minutes') }}</span>
              </div>
              <p class="input-hint">{{ t('admin.rewards.editor.schedule.evaluationIntervalHint') }}</p>
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.schedule.cooldown') }}</label>
              <div class="relative">
                <input v-model.number="form.cooldown_days" type="number" min="0" step="1" class="input pr-14" />
                <span class="field-suffix">{{ t('admin.rewards.units.days') }}</span>
              </div>
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.schedule.perUserLimit') }}</label>
              <div class="relative">
                <input v-model.number="form.max_grants_per_user" type="number" min="1" step="1" class="input pr-14" />
                <span class="field-suffix">{{ t('admin.rewards.units.times') }}</span>
              </div>
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.schedule.controlGroup') }}</label>
              <div class="relative">
                <input
                  v-model.number="form.control_group_percent"
                  type="number"
                  min="0"
                  max="100"
                  step="0.1"
                  class="input pr-8"
                />
                <span class="field-suffix">%</span>
              </div>
              <p class="input-hint">{{ t('admin.rewards.editor.schedule.controlGroupHint') }}</p>
            </div>
          </div>
        </section>

        <section v-show="activeSection === 'audience'" class="space-y-5">
          <SectionHeading
            :title="t('admin.rewards.editor.audience.title')"
            :description="t('admin.rewards.editor.audience.description')"
          />
          <RewardAudienceBuilder
            v-model="form.audience"
            :subscription-groups="subscriptionGroups"
          />
        </section>

        <section v-show="activeSection === 'budget'" class="space-y-5">
          <SectionHeading
            :title="t('admin.rewards.editor.budget.title')"
            :description="t('admin.rewards.editor.budget.description')"
          />

          <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.budget.totalBudget') }}</label>
              <div class="relative">
                <span class="field-prefix">$</span>
                <input
                  v-model.number="form.total_budget"
                  type="number"
                  min="0.01"
                  step="0.01"
                  class="input pl-7"
                />
              </div>
              <p v-if="minimumBudget > 0" class="input-hint">
                {{ t('admin.rewards.editor.budget.minimumBudget', { amount: formatCurrency(minimumBudget) }) }}
              </p>
            </div>
            <div class="metric-inline">
              <span>{{ t('admin.rewards.editor.budget.averageReward') }}</span>
              <strong>{{ formatCurrency(weightedAverage) }}</strong>
            </div>
            <div class="metric-inline">
              <span>{{ t('admin.rewards.editor.budget.maximumPerUser') }}</span>
              <strong>{{ formatCurrency(maximumPerUser) }}</strong>
            </div>
          </div>

          <div>
            <div class="mb-2 grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_56px] gap-3 px-1 text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
              <span>{{ t('admin.rewards.editor.budget.amount') }}</span>
              <span>{{ t('admin.rewards.editor.budget.weight') }}</span>
              <span></span>
            </div>
            <div class="space-y-2">
              <div
                v-for="(tier, index) in form.amount_tiers"
                :key="index"
                class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_56px] items-center gap-3"
              >
                <div class="relative">
                  <span class="field-prefix">$</span>
                  <input v-model.number="tier.amount" type="number" min="0.01" step="0.01" class="input pl-7" />
                </div>
                <div class="relative">
                  <input v-model.number="tier.weight" type="number" min="1" step="1" class="input pr-10" />
                  <span class="field-suffix">{{ tierShare(tier.weight) }}%</span>
                </div>
                <button
                  type="button"
                  class="icon-action"
                  :disabled="form.amount_tiers.length <= 1"
                  :title="t('admin.rewards.editor.budget.removeTier')"
                  @click="removeTier(index)"
                >
                  <Icon name="x" size="sm" />
                </button>
              </div>
            </div>
            <button type="button" class="btn btn-secondary mt-3" @click="addTier">
              <Icon name="plus" size="sm" class="mr-1" />
              {{ t('admin.rewards.editor.budget.addTier') }}
            </button>
          </div>

          <div
            v-if="campaign && ['active', 'scheduled', 'paused'].includes(campaign.status)"
            class="rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:border-blue-900/40 dark:bg-blue-900/20 dark:text-blue-300"
          >
            {{ t('admin.rewards.editor.budget.versionNotice', { version: campaign.current_version + 1 }) }}
          </div>
        </section>

        <section v-show="activeSection === 'skin-copy'" class="space-y-5">
          <SectionHeading
            :title="t('admin.rewards.editor.skinCopy.title')"
            :description="t('admin.rewards.editor.skinCopy.description')"
          />

          <RewardSkinPicker
            v-model="form.skin_allocations"
            :skins="skins"
            :uploading="uploadingSkin"
            :reset-token="skinResetToken"
            @upload="(file, metadata, fallback) => emit('uploadSkin', file, metadata, fallback)"
          />

          <div class="border-t border-gray-200 pt-5 dark:border-dark-700">
            <div class="mb-4 flex items-center justify-between gap-3">
              <p class="text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.rewards.editor.copy.title') }}
              </p>
              <div class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-700 dark:bg-dark-900">
                <button type="button" :class="localeButtonClass(copyLocale === 'zh')" @click="copyLocale = 'zh'">
                  {{ t('admin.rewards.editor.copy.zh') }}
                </button>
                <button type="button" :class="localeButtonClass(copyLocale === 'en')" @click="copyLocale = 'en'">
                  {{ t('admin.rewards.editor.copy.en') }}
                </button>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.copy.campaignTitle') }}</label>
                <input v-model.trim="activeCopy.title" type="text" maxlength="80" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.copy.hint') }}</label>
                <input v-model.trim="activeCopy.hint" type="text" maxlength="120" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.copy.scratchPrompt') }}</label>
                <input v-model.trim="activeCopy.scratch_prompt" type="text" maxlength="80" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.copy.claimCta') }}</label>
                <input v-model.trim="activeCopy.claim_cta" type="text" maxlength="40" class="input" />
              </div>
              <div class="lg:col-span-2">
                <label class="input-label">{{ t('admin.rewards.editor.copy.successMessage') }}</label>
                <input v-model.trim="activeCopy.success_message" type="text" maxlength="160" class="input" />
              </div>
            </div>
          </div>
        </section>

        <section v-show="activeSection === 'estimate'" class="space-y-5">
          <SectionHeading
            :title="t('admin.rewards.editor.estimate.title')"
            :description="t('admin.rewards.editor.estimate.description')"
          />

          <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-4 dark:border-dark-700">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.rewards.editor.estimate.previewTitle') }}
              </p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                {{ estimate
                  ? t('admin.rewards.editor.estimate.updatedAt', { time: formatDateTime(estimate.data_updated_at) })
                  : t('admin.rewards.editor.estimate.notRun') }}
              </p>
            </div>
            <button type="button" class="btn btn-secondary" :disabled="estimating" @click="requestEstimate">
              <Icon name="calculator" size="sm" class="mr-1" />
              {{ estimating ? t('admin.rewards.editor.estimate.running') : t('admin.rewards.editor.estimate.run') }}
            </button>
          </div>

          <div class="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-gray-200 bg-gray-200 dark:border-dark-700 dark:bg-dark-700 lg:grid-cols-5">
            <div v-for="metric in estimateMetrics" :key="metric.label" class="bg-white px-4 py-4 dark:bg-dark-900">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ metric.label }}</p>
              <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ metric.value }}</p>
            </div>
          </div>

          <div
            v-if="estimate?.warnings?.length"
            class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 dark:border-amber-900/40 dark:bg-amber-900/20"
          >
            <p class="text-sm font-medium text-amber-800 dark:text-amber-300">
              {{ t('admin.rewards.editor.estimate.warnings') }}
            </p>
            <ul class="mt-2 space-y-1 text-sm text-amber-700 dark:text-amber-300">
              <li v-for="warning in estimate.warnings" :key="warning">{{ warning }}</li>
            </ul>
          </div>

          <div class="grid grid-cols-1 gap-3 border-t border-gray-200 pt-5 text-sm dark:border-dark-700 lg:grid-cols-2">
            <SummaryRow :label="t('admin.rewards.editor.summary.mode')" :value="modeLabel(form.issuance_mode)" />
            <SummaryRow :label="t('admin.rewards.editor.summary.time')" :value="scheduleSummary" />
            <SummaryRow :label="t('admin.rewards.editor.summary.audience')" :value="audienceSummary" />
            <SummaryRow :label="t('admin.rewards.editor.summary.pool')" :value="poolSummary" />
            <SummaryRow :label="t('admin.rewards.editor.summary.skins')" :value="t('admin.rewards.editor.summary.skinCount', { count: form.skin_allocations.length })" />
            <SummaryRow :label="t('admin.rewards.editor.summary.version')" :value="versionSummary" />
          </div>

          <div
            v-if="validationErrors.length"
            class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 dark:border-red-900/40 dark:bg-red-900/20"
          >
            <p class="text-sm font-medium text-red-800 dark:text-red-300">
              {{ t('admin.rewards.editor.validation.title') }}
            </p>
            <ul class="mt-2 space-y-1 text-sm text-red-700 dark:text-red-300">
              <li v-for="message in validationErrors" :key="message">{{ message }}</li>
            </ul>
          </div>
        </section>
      </div>
    </form>

    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-2">
          <button
            v-if="currentSectionIndex > 0"
            type="button"
            class="btn btn-secondary"
            @click="goPrevious"
          >
            <Icon name="chevronLeft" size="sm" class="mr-1" />
            {{ t('admin.rewards.editor.previous') }}
          </button>
          <button
            v-if="currentSectionIndex < sections.length - 1"
            type="button"
            class="btn btn-secondary"
            @click="goNext"
          >
            {{ t('common.next') }}
            <Icon name="chevronRight" size="sm" class="ml-1" />
          </button>
        </div>
        <div class="flex items-center gap-2">
          <button type="button" class="btn btn-secondary" @click="emit('close')">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="reward-campaign-form" class="btn btn-secondary" :disabled="saving">
            {{ saving ? t('common.saving') : saveLabel }}
          </button>
          <button
            v-if="canPublish"
            type="button"
            class="btn btn-primary"
            :disabled="saving"
            @click="submit(true)"
          >
            <Icon name="play" size="sm" class="mr-1" />
            {{ t('admin.rewards.actions.publish') }}
          </button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import RewardAudienceBuilder from './RewardAudienceBuilder.vue'
import RewardSkinPicker from './RewardSkinPicker.vue'
import { formatCurrency, formatDateTime } from '@/utils/format'
import type {
  RewardAudienceEstimate,
  RewardCampaign,
  RewardCampaignDraft,
  RewardIssuanceMode,
  RewardSkin,
  RewardSkinUploadMetadata
} from '@/api/admin/rewards'

type EditorSection = 'basic' | 'schedule' | 'audience' | 'budget' | 'skin-copy' | 'estimate'

const props = withDefaults(defineProps<{
  show: boolean
  campaign?: RewardCampaign | null
  skins?: RewardSkin[]
  subscriptionGroups?: Array<{ id: number; name: string }>
  estimate?: RewardAudienceEstimate | null
  saving?: boolean
  estimating?: boolean
  uploadingSkin?: boolean
  skinResetToken?: number
}>(), {
  campaign: null,
  skins: () => [],
  subscriptionGroups: () => [],
  estimate: null,
  saving: false,
  estimating: false,
  uploadingSkin: false,
  skinResetToken: 0
})

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'save', payload: RewardCampaignDraft, publishAfterSave: boolean): void
  (event: 'estimate', payload: RewardCampaignDraft): void
  (event: 'uploadSkin', file: File, metadata: RewardSkinUploadMetadata, canvasFallback: boolean): void
}>()

const { t } = useI18n()
const activeSection = ref<EditorSection>('basic')
const copyLocale = ref<'zh' | 'en'>('zh')

function emptyCopy(locale: 'zh' | 'en') {
  return locale === 'zh'
    ? {
        title: '一份奖励正在等你',
        hint: '刮开卡片，领取账户余额',
        scratch_prompt: '刮开看看',
        claim_cta: '收下奖励',
        success_message: '奖励已到账'
      }
    : {
        title: 'A reward is waiting for you',
        hint: 'Scratch the card to claim account balance',
        scratch_prompt: 'Scratch to reveal',
        claim_cta: 'Claim reward',
        success_message: 'Your reward has been credited'
      }
}

function defaultDraft(): RewardCampaignDraft {
  const now = new Date()
  const end = new Date(now.getTime() + 30 * 24 * 60 * 60 * 1000)
  return {
    name: '',
    description: '',
    issuance_mode: 'on_access',
    timezone: resolveTimezone(),
    starts_at: toLocalInput(now.toISOString()),
    ends_at: toLocalInput(end.toISOString()),
    priority: 100,
    win_probability: 0.1,
    max_grants_per_user: 1,
    evaluation_interval_minutes: 1440,
    cooldown_days: 30,
    control_group_percent: 0,
    total_budget: 100,
    amount_tiers: [
      { amount: 1, weight: 60 },
      { amount: 3, weight: 30 },
      { amount: 5, weight: 10 }
    ],
    audience: { any_of: [] },
    skin_allocations: [],
    copy: {
      zh: emptyCopy('zh'),
      en: emptyCopy('en')
    }
  }
}

const form = reactive<RewardCampaignDraft>(defaultDraft())

const sections = computed<Array<{ key: EditorSection; label: string }>>(() => [
  { key: 'basic', label: t('admin.rewards.editor.sections.basic') },
  { key: 'schedule', label: t('admin.rewards.editor.sections.schedule') },
  { key: 'audience', label: t('admin.rewards.editor.sections.audience') },
  { key: 'budget', label: t('admin.rewards.editor.sections.budget') },
  { key: 'skin-copy', label: t('admin.rewards.editor.sections.skinCopy') },
  { key: 'estimate', label: t('admin.rewards.editor.sections.estimate') }
])

const timezoneOptions = computed(() => {
  const common = ['UTC', 'Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Tokyo', 'Europe/London', 'America/New_York', 'America/Los_Angeles']
  try {
    const supported = (Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] })
      .supportedValuesOf?.('timeZone') ?? []
    return Array.from(new Set([resolveTimezone(), ...common, ...supported]))
  } catch {
    return Array.from(new Set([resolveTimezone(), ...common]))
  }
})

const dialogTitle = computed(() =>
  props.campaign
    ? t('admin.rewards.editor.editTitle', { name: props.campaign.name })
    : t('admin.rewards.editor.createTitle')
)
const currentSectionIndex = computed(() =>
  sections.value.findIndex((section) => section.key === activeSection.value)
)
const activeCopy = computed(() => form.copy[copyLocale.value])
const probabilityPercent = computed(() => Number((form.win_probability * 100).toFixed(2)))
const totalTierWeight = computed(() =>
  form.amount_tiers.reduce((sum, tier) => sum + Math.max(0, Number(tier.weight) || 0), 0)
)
const weightedAverage = computed(() => {
  if (!totalTierWeight.value) return 0
  return form.amount_tiers.reduce(
    (sum, tier) => sum + (Number(tier.amount) || 0) * Math.max(0, Number(tier.weight) || 0),
    0
  ) / totalTierWeight.value
})
const maximumPerUser = computed(() =>
  Math.max(0, ...form.amount_tiers.map((tier) => Number(tier.amount) || 0)) *
  Math.max(1, Number(form.max_grants_per_user) || 1)
)
const minimumBudget = computed(() =>
  props.campaign ? Number(props.campaign.spent_budget || 0) + Number(props.campaign.reserved_budget || 0) : 0
)
const canPublish = computed(() => !props.campaign || props.campaign.status === 'draft')
const saveLabel = computed(() => {
  if (!props.campaign || props.campaign.status === 'draft') return t('admin.rewards.actions.saveDraft')
  return t('admin.rewards.actions.saveVersion')
})
const estimateMetrics = computed(() => [
  {
    label: t('admin.rewards.editor.estimate.matchedUsers'),
    value: formatCount(props.estimate?.matched_users)
  },
  {
    label: t('admin.rewards.editor.estimate.expectedWinners'),
    value: formatCount(props.estimate?.expected_winners)
  },
  {
    label: t('admin.rewards.editor.estimate.expectedCost'),
    value: props.estimate ? formatCurrency(props.estimate.expected_cost) : '-'
  },
  {
    label: t('admin.rewards.editor.estimate.maximumCost'),
    value: props.estimate ? formatCurrency(props.estimate.maximum_cost) : '-'
  },
  {
    label: t('admin.rewards.editor.estimate.controlUsers'),
    value: formatCount(props.estimate?.control_group_users)
  }
])
const scheduleSummary = computed(() =>
  `${form.starts_at || '-'} - ${form.ends_at || '-'} (${form.timezone})`
)
const audienceSummary = computed(() =>
  form.audience.any_of.length
    ? t('admin.rewards.editor.summary.audienceGroups', { count: form.audience.any_of.length })
    : t('admin.rewards.editor.audience.allUsers')
)
const poolSummary = computed(() =>
  t('admin.rewards.editor.summary.poolValue', {
    budget: formatCurrency(form.total_budget),
    tiers: form.amount_tiers.length
  })
)
const versionSummary = computed(() =>
  props.campaign
    ? t('admin.rewards.editor.summary.nextVersion', { version: props.campaign.current_version + 1 })
    : t('admin.rewards.editor.summary.firstVersion')
)

const validationErrors = computed(() => {
  const messages: string[] = []
  if (!form.name.trim()) messages.push(t('admin.rewards.editor.validation.name'))
  if (!isValidTimezone(form.timezone)) messages.push(t('admin.rewards.editor.validation.timezone'))
  if (!form.starts_at || !form.ends_at || new Date(form.ends_at).getTime() <= new Date(form.starts_at).getTime()) {
    messages.push(t('admin.rewards.editor.validation.timeRange'))
  }
  if (form.win_probability < 0 || form.win_probability > 1) {
    messages.push(t('admin.rewards.editor.validation.probability'))
  }
  if (form.total_budget <= 0 || form.total_budget + Number.EPSILON < minimumBudget.value) {
    messages.push(t('admin.rewards.editor.validation.budget', { amount: formatCurrency(minimumBudget.value) }))
  }
  if (!form.amount_tiers.length || form.amount_tiers.some((tier) => tier.amount <= 0 || tier.weight <= 0)) {
    messages.push(t('admin.rewards.editor.validation.tiers'))
  }
  if (!form.skin_allocations.length || form.skin_allocations.some((allocation) => allocation.weight <= 0)) {
    messages.push(t('admin.rewards.editor.validation.skins'))
  }
  if (!form.copy.zh.title.trim() || !form.copy.zh.hint.trim() || !form.copy.en.title.trim() || !form.copy.en.hint.trim()) {
    messages.push(t('admin.rewards.editor.validation.copy'))
  }
  for (const group of form.audience.any_of) {
    if (
      !group.all_of.length ||
      group.all_of.some((condition) =>
        condition.value === '' ||
        condition.value === null ||
        (Array.isArray(condition.value) && condition.value.length === 0)
      )
    ) {
      messages.push(t('admin.rewards.editor.validation.audience'))
      break
    }
  }
  return messages
})

watch(
  () => [props.show, props.campaign] as const,
  ([show]) => {
    if (!show) return
    activeSection.value = 'basic'
    copyLocale.value = 'zh'
    Object.assign(form, props.campaign ? campaignToDraft(props.campaign) : defaultDraft())
  },
  { immediate: true }
)

function campaignToDraft(campaign: RewardCampaign): RewardCampaignDraft {
  return {
    name: campaign.name,
    description: campaign.description ?? '',
    issuance_mode: campaign.issuance_mode,
    timezone: campaign.timezone || 'UTC',
    starts_at: toZonedInput(campaign.starts_at, campaign.timezone),
    ends_at: toZonedInput(campaign.ends_at, campaign.timezone),
    priority: campaign.priority,
    win_probability: Number(campaign.win_probability),
    max_grants_per_user: campaign.max_grants_per_user,
    evaluation_interval_minutes: campaign.evaluation_interval_minutes,
    cooldown_days: campaign.cooldown_days,
    control_group_percent: Number(campaign.control_group_percent),
    total_budget: Number(campaign.total_budget),
    amount_tiers: campaign.amount_tiers.map((tier) => ({
      amount: Number(tier.amount),
      weight: Number(tier.weight)
    })),
    audience: audienceToDraft(campaign.audience ?? { any_of: [] }, campaign.timezone),
    skin_allocations: campaign.skin_allocations.map((allocation) => ({ ...allocation })),
    copy: JSON.parse(JSON.stringify(campaign.copy ?? { zh: emptyCopy('zh'), en: emptyCopy('en') }))
  }
}

function payload(): RewardCampaignDraft {
  return JSON.parse(JSON.stringify(form))
}

function audienceToDraft(
  audience: RewardCampaignDraft['audience'],
  timezone: string
): RewardCampaignDraft['audience'] {
  return {
    any_of: audience.any_of.map((group) => ({
      all_of: group.all_of.map((condition) => ({
        ...condition,
        value: (
          ['registered_at', 'last_active_at', 'last_api_used_at'].includes(condition.field) &&
          ['before', 'after'].includes(condition.operator) &&
          typeof condition.value === 'string'
        )
          ? toZonedInput(condition.value, timezone)
          : Array.isArray(condition.value)
            ? [...condition.value]
            : condition.value
      }))
    }))
  }
}

function requestEstimate() {
  if (!validateForAction()) return
  emit('estimate', payload())
}

function submit(publishAfterSave: boolean) {
  if (!validateForAction()) return
  emit('save', payload(), publishAfterSave)
}

function validateForAction() {
  if (!validationErrors.value.length) return true
  activeSection.value = 'estimate'
  return false
}

function addTier() {
  const lastAmount = form.amount_tiers[form.amount_tiers.length - 1]?.amount ?? 0
  form.amount_tiers.push({ amount: Math.max(0.01, Number(lastAmount) + 1), weight: 1 })
}

function removeTier(index: number) {
  if (form.amount_tiers.length > 1) form.amount_tiers.splice(index, 1)
}

function tierShare(weight: number) {
  if (!totalTierWeight.value) return '0'
  return ((Math.max(0, Number(weight) || 0) / totalTierWeight.value) * 100).toFixed(0)
}

function setProbability(raw: string) {
  const percent = Number(raw)
  form.win_probability = Number.isFinite(percent) ? Math.max(0, Math.min(100, percent)) / 100 : 0
}

function modeLabel(mode: RewardIssuanceMode) {
  return mode === 'on_access'
    ? t('admin.rewards.modes.onAccess')
    : t('admin.rewards.modes.scheduledBatch')
}

function formatCount(value?: number) {
  return value === undefined ? '-' : new Intl.NumberFormat().format(value)
}

function resolveTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

function isValidTimezone(timezone: string) {
  if (!timezone.trim()) return false
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: timezone }).format()
    return true
  } catch {
    return false
  }
}

function toLocalInput(value?: string | null) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value.slice(0, 16)
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function toZonedInput(value: string, timezone: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value.slice(0, 16)
  try {
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone: timezone,
      hourCycle: 'h23',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    }).formatToParts(date)
    const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
    return `${values.year}-${values.month}-${values.day}T${values.hour}:${values.minute}`
  } catch {
    return toLocalInput(value)
  }
}

function goPrevious() {
  activeSection.value = sections.value[Math.max(0, currentSectionIndex.value - 1)].key
}

function goNext() {
  activeSection.value = sections.value[Math.min(sections.value.length - 1, currentSectionIndex.value + 1)].key
}

function sectionTabClass(section: EditorSection) {
  return [
    'inline-flex items-center gap-2 border-b-2 px-3 py-3 text-sm font-medium transition-colors focus:outline-none',
    activeSection.value === section
      ? 'border-primary-500 text-primary-700 dark:text-primary-300'
      : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white'
  ]
}

function choiceClass(selected: boolean) {
  return [
    'flex min-h-[88px] cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
    selected
      ? 'border-primary-400 bg-primary-50 text-primary-800 dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-200'
      : 'border-gray-200 bg-white text-gray-600 hover:border-gray-300 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300'
  ]
}

function localeButtonClass(selected: boolean) {
  return [
    'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
    selected
      ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
      : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white'
  ]
}

const SectionHeading = defineComponent({
  props: { title: { type: String, required: true }, description: { type: String, required: true } },
  setup(componentProps) {
    return () => h('div', [
      h('h4', { class: 'text-base font-semibold text-gray-900 dark:text-white' }, componentProps.title),
      h('p', { class: 'mt-1 text-sm text-gray-500 dark:text-dark-400' }, componentProps.description)
    ])
  }
})

const SummaryRow = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, required: true } },
  setup(componentProps) {
    return () => h(
      'div',
      { class: 'flex items-start justify-between gap-4 border-b border-gray-100 pb-2 dark:border-dark-800' },
      [
        h('span', { class: 'text-gray-500 dark:text-dark-400' }, componentProps.label),
        h('span', { class: 'max-w-[70%] text-right font-medium text-gray-900 dark:text-white' }, componentProps.value)
      ]
    )
  }
})
</script>

<style scoped>
.field-suffix {
  @apply pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-gray-400 dark:text-dark-500;
}

.field-prefix {
  @apply pointer-events-none absolute inset-y-0 left-3 flex items-center text-sm text-gray-400 dark:text-dark-500;
}

.metric-inline {
  @apply flex min-h-[72px] flex-col justify-center border-l border-gray-200 pl-4 text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400;
}

.metric-inline strong {
  @apply mt-1 text-lg font-semibold text-gray-900 dark:text-white;
}

.icon-action {
  @apply inline-flex h-8 w-8 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-30 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-gray-200;
}
</style>
