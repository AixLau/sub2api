<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <p class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.rewards.editor.audience.logicTitle') }}
        </p>
        <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.rewards.editor.audience.logicHint') }}
        </p>
      </div>

      <div class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-700 dark:bg-dark-900">
        <button
          type="button"
          :class="modeButtonClass(mode === 'all')"
          @click="setMode('all')"
        >
          {{ t('admin.rewards.editor.audience.allUsers') }}
        </button>
        <button
          type="button"
          :class="modeButtonClass(mode === 'targeted')"
          @click="setMode('targeted')"
        >
          {{ t('admin.rewards.editor.audience.targeted') }}
        </button>
      </div>
    </div>

    <div
      v-if="mode === 'all'"
      class="rounded-lg border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
    >
      {{ t('admin.rewards.editor.audience.allUsersHint') }}
    </div>

    <template v-else>
      <div
        v-for="(group, groupIndex) in audience.any_of"
        :key="groupIndex"
        class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900"
      >
        <div class="flex items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <span class="flex h-6 min-w-6 items-center justify-center rounded bg-primary-50 px-1.5 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              {{ groupIndex + 1 }}
            </span>
            <span class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('admin.rewards.editor.audience.groupTitle') }}
            </span>
            <span class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.rewards.editor.audience.andRelation') }}
            </span>
          </div>
          <button
            type="button"
            class="icon-action"
            :title="t('admin.rewards.editor.audience.removeGroup')"
            :aria-label="t('admin.rewards.editor.audience.removeGroup')"
            @click="removeGroup(groupIndex)"
          >
            <Icon name="x" size="sm" />
          </button>
        </div>

        <div class="mt-3 space-y-2">
          <div
            v-for="(condition, conditionIndex) in group.all_of"
            :key="conditionIndex"
            class="grid grid-cols-1 items-end gap-2 border-t border-gray-100 pt-3 first:border-0 first:pt-0 md:grid-cols-[minmax(180px,1fr)_150px_minmax(180px,1.4fr)_32px]"
          >
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.audience.field') }}</label>
              <Select
                :model-value="condition.field"
                :options="fieldOptions"
                @update:model-value="setField(groupIndex, conditionIndex, String($event) as RewardAudienceField)"
              />
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.audience.operator') }}</label>
              <Select
                :model-value="condition.operator"
                :options="operatorOptions(condition.field)"
                @update:model-value="setOperator(groupIndex, conditionIndex, String($event) as RewardAudienceOperator)"
              />
            </div>
            <div>
              <label class="input-label">{{ t('admin.rewards.editor.audience.value') }}</label>
              <input
                v-if="fieldKind(condition.field) === 'date' && condition.operator !== 'within_days'"
                :value="String(condition.value ?? '')"
                type="datetime-local"
                class="input"
                @input="setValue(groupIndex, conditionIndex, ($event.target as HTMLInputElement).value)"
              />
              <Select
                v-else-if="condition.field === 'registration_source'"
                :model-value="String(condition.value ?? '')"
                :options="registrationSourceOptions"
                @update:model-value="setValue(groupIndex, conditionIndex, String($event))"
              />
              <Select
                v-else-if="condition.field === 'subscription_group_id' && subscriptionGroups.length"
                :model-value="Number(condition.value ?? 0)"
                :options="subscriptionGroupOptions"
                @update:model-value="setValue(groupIndex, conditionIndex, Number($event))"
              />
              <input
                v-else
                :value="displayValue(condition)"
                :type="fieldKind(condition.field) === 'number' || condition.operator === 'within_days' ? 'number' : 'text'"
                :step="isMoneyField(condition.field) ? '0.01' : '1'"
                :placeholder="valuePlaceholder(condition.field)"
                class="input"
                @input="setInputValue(groupIndex, conditionIndex, condition, ($event.target as HTMLInputElement).value)"
              />
            </div>
            <button
              type="button"
              class="icon-action mb-1"
              :title="t('admin.rewards.editor.audience.removeCondition')"
              :aria-label="t('admin.rewards.editor.audience.removeCondition')"
              @click="removeCondition(groupIndex, conditionIndex)"
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
        </div>

        <button
          type="button"
          class="mt-3 inline-flex items-center text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400"
          @click="addCondition(groupIndex)"
        >
          <Icon name="plus" size="sm" class="mr-1" />
          {{ t('admin.rewards.editor.audience.addCondition') }}
        </button>
      </div>

      <div class="flex items-center gap-3">
        <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
        <span class="text-xs font-semibold text-gray-400 dark:text-dark-500">
          {{ t('admin.rewards.editor.audience.orRelation') }}
        </span>
        <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
      </div>

      <button type="button" class="btn btn-secondary w-full" @click="addGroup">
        <Icon name="plus" size="sm" class="mr-1" />
        {{ t('admin.rewards.editor.audience.addGroup') }}
      </button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type {
  RewardAudience,
  RewardAudienceCondition,
  RewardAudienceField,
  RewardAudienceOperator
} from '@/api/admin/rewards'

const props = withDefaults(defineProps<{
  modelValue: RewardAudience
  subscriptionGroups?: Array<{ id: number; name: string }>
}>(), {
  subscriptionGroups: () => []
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: RewardAudience): void
}>()

const { t } = useI18n()

const audience = computed(() => props.modelValue ?? { any_of: [] })
const mode = computed(() => audience.value.any_of.length > 0 ? 'targeted' : 'all')

const fieldOptions = computed(() => [
  { value: 'registered_at', label: t('admin.rewards.editor.audience.fields.registeredAt') },
  { value: 'registration_source', label: t('admin.rewards.editor.audience.fields.registrationSource') },
  { value: 'last_active_at', label: t('admin.rewards.editor.audience.fields.lastActiveAt') },
  { value: 'balance', label: t('admin.rewards.editor.audience.fields.balance') },
  { value: 'subscription_group_id', label: t('admin.rewards.editor.audience.fields.subscriptionGroup') },
  { value: 'user_id', label: t('admin.rewards.editor.audience.fields.userId') },
  { value: 'request_count_7d', label: t('admin.rewards.editor.audience.fields.requests7d') },
  { value: 'request_count_30d', label: t('admin.rewards.editor.audience.fields.requests30d') },
  { value: 'actual_cost_7d', label: t('admin.rewards.editor.audience.fields.cost7d') },
  { value: 'actual_cost_30d', label: t('admin.rewards.editor.audience.fields.cost30d') },
  { value: 'last_api_used_at', label: t('admin.rewards.editor.audience.fields.lastApiUsedAt') },
  { value: 'recharge_amount_30d', label: t('admin.rewards.editor.audience.fields.recharge30d') },
  { value: 'recharge_amount_total', label: t('admin.rewards.editor.audience.fields.rechargeTotal') }
])

const registrationSourceOptions = computed(() => [
  { value: 'email', label: t('admin.rewards.editor.audience.sources.email') },
  { value: 'linuxdo', label: t('admin.rewards.editor.audience.sources.linuxdo') },
  { value: 'wechat', label: t('admin.rewards.editor.audience.sources.wechat') },
  { value: 'oidc', label: t('admin.rewards.editor.audience.sources.oidc') },
  { value: 'github', label: t('admin.rewards.editor.audience.sources.github') },
  { value: 'google', label: t('admin.rewards.editor.audience.sources.google') },
  { value: 'dingtalk', label: t('admin.rewards.editor.audience.sources.dingtalk') }
])

const subscriptionGroupOptions = computed(() => [
  { value: 0, label: t('admin.rewards.editor.audience.selectGroup') },
  ...props.subscriptionGroups.map((group) => ({ value: group.id, label: group.name }))
])

const dateFields = new Set<RewardAudienceField>([
  'registered_at',
  'last_active_at',
  'last_api_used_at'
])

const moneyFields = new Set<RewardAudienceField>([
  'balance',
  'actual_cost_7d',
  'actual_cost_30d',
  'recharge_amount_30d',
  'recharge_amount_total'
])

const setFields = new Set<RewardAudienceField>([
  'registration_source',
  'subscription_group_id',
  'user_id'
])

function fieldKind(field: RewardAudienceField): 'date' | 'number' | 'set' {
  if (dateFields.has(field)) return 'date'
  if (setFields.has(field)) return 'set'
  return 'number'
}

function isMoneyField(field: RewardAudienceField) {
  return moneyFields.has(field)
}

function defaultOperator(field: RewardAudienceField): RewardAudienceOperator {
  if (dateFields.has(field)) return 'after'
  if (setFields.has(field)) return 'eq'
  return 'gte'
}

function defaultValue(field: RewardAudienceField): string | number {
  if (dateFields.has(field)) return ''
  if (field === 'registration_source') return 'email'
  return 0
}

function defaultCondition(): RewardAudienceCondition {
  return { field: 'registered_at', operator: 'after', value: '' }
}

function operatorOptions(field: RewardAudienceField) {
  if (dateFields.has(field)) {
    return [
      { value: 'after', label: t('admin.rewards.editor.audience.operators.after') },
      { value: 'before', label: t('admin.rewards.editor.audience.operators.before') },
      { value: 'within_days', label: t('admin.rewards.editor.audience.operators.withinDays') }
    ]
  }
  if (setFields.has(field)) {
    const options = [
      { value: 'eq', label: t('admin.rewards.editor.audience.operators.eq') },
      { value: 'neq', label: t('admin.rewards.editor.audience.operators.neq') }
    ]
    if (field === 'user_id') {
      options.push(
        { value: 'in', label: t('admin.rewards.editor.audience.operators.in') },
        { value: 'not_in', label: t('admin.rewards.editor.audience.operators.notIn') }
      )
    }
    return options
  }
  return [
    { value: 'gte', label: t('admin.rewards.editor.audience.operators.gte') },
    { value: 'gt', label: t('admin.rewards.editor.audience.operators.gt') },
    { value: 'lte', label: t('admin.rewards.editor.audience.operators.lte') },
    { value: 'lt', label: t('admin.rewards.editor.audience.operators.lt') },
    { value: 'eq', label: t('admin.rewards.editor.audience.operators.eq') }
  ]
}

function valuePlaceholder(field: RewardAudienceField) {
  if (field === 'user_id') return t('admin.rewards.editor.audience.userIdsPlaceholder')
  return t('admin.rewards.editor.audience.valuePlaceholder')
}

function cloneAudience(): RewardAudience {
  return JSON.parse(JSON.stringify(audience.value))
}

function update(mutator: (draft: RewardAudience) => void) {
  const draft = cloneAudience()
  mutator(draft)
  emit('update:modelValue', draft)
}

function setMode(next: 'all' | 'targeted') {
  if (next === 'all') {
    emit('update:modelValue', { any_of: [] })
  } else if (audience.value.any_of.length === 0) {
    emit('update:modelValue', { any_of: [{ all_of: [defaultCondition()] }] })
  }
}

function addGroup() {
  update((draft) => draft.any_of.push({ all_of: [defaultCondition()] }))
}

function removeGroup(groupIndex: number) {
  update((draft) => draft.any_of.splice(groupIndex, 1))
}

function addCondition(groupIndex: number) {
  update((draft) => draft.any_of[groupIndex]?.all_of.push(defaultCondition()))
}

function removeCondition(groupIndex: number, conditionIndex: number) {
  update((draft) => {
    const group = draft.any_of[groupIndex]
    group?.all_of.splice(conditionIndex, 1)
    if (group && group.all_of.length === 0) draft.any_of.splice(groupIndex, 1)
  })
}

function setField(groupIndex: number, conditionIndex: number, field: RewardAudienceField) {
  update((draft) => {
    const condition = draft.any_of[groupIndex]?.all_of[conditionIndex]
    if (!condition) return
    condition.field = field
    condition.operator = defaultOperator(field)
    condition.value = defaultValue(field)
  })
}

function setOperator(groupIndex: number, conditionIndex: number, operator: RewardAudienceOperator) {
  update((draft) => {
    const condition = draft.any_of[groupIndex]?.all_of[conditionIndex]
    if (!condition) return
    condition.operator = operator
    if (operator === 'within_days') {
      condition.value = 30
    } else if (operator === 'in' || operator === 'not_in') {
      if (!Array.isArray(condition.value)) {
        condition.value = condition.value === '' || condition.value === null
          ? []
          : [condition.value]
      }
    } else if (Array.isArray(condition.value)) {
      condition.value = condition.value[0] ?? defaultValue(condition.field)
    }
  })
}

function setValue(groupIndex: number, conditionIndex: number, value: string | number | Array<string | number>) {
  update((draft) => {
    const condition = draft.any_of[groupIndex]?.all_of[conditionIndex]
    if (condition) condition.value = value
  })
}

function displayValue(condition: RewardAudienceCondition): string {
  return Array.isArray(condition.value) ? condition.value.join(', ') : String(condition.value ?? '')
}

function setInputValue(
  groupIndex: number,
  conditionIndex: number,
  condition: RewardAudienceCondition,
  raw: string
) {
  if (condition.field === 'user_id' && ['in', 'not_in'].includes(condition.operator)) {
    setValue(
      groupIndex,
      conditionIndex,
      raw.split(',').map((value) => Number(value.trim())).filter(Number.isFinite)
    )
    return
  }
  if (fieldKind(condition.field) === 'number' || condition.operator === 'within_days') {
    setValue(groupIndex, conditionIndex, raw === '' ? 0 : Number(raw))
    return
  }
  setValue(groupIndex, conditionIndex, raw)
}

function modeButtonClass(active: boolean) {
  return [
    'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
    active
      ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
      : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white'
  ]
}
</script>

<style scoped>
.icon-action {
  @apply inline-flex h-8 w-8 flex-none items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-gray-200;
}
</style>
