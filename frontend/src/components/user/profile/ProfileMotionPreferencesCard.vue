<template>
  <section class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-medium text-gray-900 dark:text-white">
        {{ t('profile.motion.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('profile.motion.description') }}
      </p>
    </div>

    <div class="flex items-center justify-between gap-6 px-6 py-6">
      <div>
        <p class="font-medium text-gray-900 dark:text-white">
          {{ t('profile.motion.reduceMotion') }}
        </p>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{
            systemReducedMotion
              ? t('profile.motion.systemPreferenceActive')
              : t('profile.motion.reduceMotionHint')
          }}
        </p>
      </div>
      <Toggle
        :model-value="applicationReducedMotion"
        :aria-label="t('profile.motion.reduceMotion')"
        @update:model-value="setApplicationReducedMotion"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { usePrefersReducedMotion } from '@/composables/usePrefersReducedMotion'

const { t } = useI18n()
const {
  applicationReducedMotion,
  systemReducedMotion,
  setApplicationReducedMotion
} = usePrefersReducedMotion()
</script>
