<template>
  <div class="space-y-2">
    <Select
      v-model="pendingValue"
      :options="availableOptions"
      :placeholder="placeholder"
      :disabled="disabled || availableOptions.length === 0"
      @change="addValue"
    />

    <div v-if="modelValue.length" class="divide-y divide-line-default overflow-hidden rounded-lg border border-line-default">
      <div
        v-for="(value, index) in modelValue"
        :key="value"
        class="flex min-h-10 items-center gap-2 bg-surface-panel px-3 py-1.5"
        data-test="ordered-multi-select-item"
      >
        <span class="flex h-5 w-5 shrink-0 items-center justify-center rounded bg-primary-50 text-xs font-semibold text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
          {{ index + 1 }}
        </span>
        <span class="min-w-0 flex-1 truncate text-sm text-content-primary">{{ optionLabel(value) }}</span>
        <button
          type="button"
          class="icon-button"
          :disabled="disabled || index === 0"
          :aria-label="moveUpLabel"
          :title="moveUpLabel"
          @click="move(index, -1)"
        >
          <Icon name="chevronUp" size="sm" />
        </button>
        <button
          type="button"
          class="icon-button"
          :disabled="disabled || index === modelValue.length - 1"
          :aria-label="moveDownLabel"
          :title="moveDownLabel"
          @click="move(index, 1)"
        >
          <Icon name="chevronDown" size="sm" />
        </button>
        <button
          type="button"
          class="icon-button text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20"
          :disabled="disabled"
          :aria-label="removeLabel"
          :title="removeLabel"
          @click="remove(index)"
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'

interface OrderedMultiSelectOption {
  value: string | number | boolean | null
  label: string
  disabled?: boolean
  [key: string]: unknown
}

const props = withDefaults(defineProps<{
  modelValue: string[]
  options: OrderedMultiSelectOption[]
  placeholder?: string
  disabled?: boolean
  moveUpLabel?: string
  moveDownLabel?: string
  removeLabel?: string
}>(), {
  placeholder: '',
  disabled: false,
  moveUpLabel: 'Move up',
  moveDownLabel: 'Move down',
  removeLabel: 'Remove',
})

const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const pendingValue = ref<string | number | boolean | null>(null)

const availableOptions = computed(() => props.options.filter(
  (option) => typeof option.value === 'string' && !props.modelValue.includes(option.value),
))

function optionLabel(value: string): string {
  return props.options.find((option) => option.value === value)?.label || value
}

function addValue(value: string | number | boolean | null) {
  if (typeof value === 'string' && value && !props.modelValue.includes(value)) {
    emit('update:modelValue', [...props.modelValue, value])
  }
  pendingValue.value = null
}

function move(index: number, offset: -1 | 1) {
  const target = index + offset
  if (target < 0 || target >= props.modelValue.length) return
  const values = [...props.modelValue]
  const current = values[index]
  values[index] = values[target]
  values[target] = current
  emit('update:modelValue', values)
}

function remove(index: number) {
  emit('update:modelValue', props.modelValue.filter((_, itemIndex) => itemIndex !== index))
}
</script>

<style scoped>
.icon-button {
  @apply flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-content-secondary transition-colors;
  @apply hover:bg-surface-subtle hover:text-content-primary;
  @apply focus:outline-none focus:ring-2 focus:ring-line-focus/30;
  @apply disabled:cursor-not-allowed disabled:opacity-30;
}
</style>
