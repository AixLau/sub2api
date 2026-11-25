<template>
  <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
    <div class="flex items-center justify-between gap-4">
      <div>
        <label class="input-label mb-0">{{ t('admin.accounts.openai.forceFast') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.openai.forceFastDesc') }}
        </p>
      </div>
      <Toggle
        :aria-label="t('admin.accounts.openai.forceFast')"
        :model-value="modelValue.force_openai_fast ?? promotionActive"
        @update:model-value="emit('update:modelValue', { ...modelValue, force_openai_fast: $event })"
      />
    </div>
    <div class="flex items-center justify-between gap-4">
      <div>
        <label class="input-label mb-0">{{ t('admin.accounts.openai.freeFast') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.openai.freeFastDesc') }}
        </p>
      </div>
      <Toggle
        :aria-label="t('admin.accounts.openai.freeFast')"
        :model-value="modelValue.free_openai_fast ?? promotionActive"
        @update:model-value="emit('update:modelValue', { ...modelValue, free_openai_fast: $event })"
      />
    </div>
    <p v-if="promotionActive" class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.openai.businessFastPromotion') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'

type OpenAIFastOverrides = Partial<Record<'force_openai_fast' | 'free_openai_fast', boolean>>

withDefaults(defineProps<{ modelValue: OpenAIFastOverrides; promotionActive?: boolean }>(), {
  promotionActive: false
})
const emit = defineEmits<{ 'update:modelValue': [value: OpenAIFastOverrides] }>()
const { t } = useI18n()
</script>
