<template>
  <div class="card-glass p-6">
    <div class="mb-4 flex items-center justify-between">
      <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ title }}
      </h3>
      <span v-if="modelCount" class="badge badge-primary">
        {{ modelCount }} {{ t('models.available') }}
      </span>
    </div>

    <!-- Loading State -->
    <div v-if="loading" class="flex items-center justify-center py-12">
      <LoadingSpinner />
    </div>

    <!-- Models Grid -->
    <div v-else-if="models.length > 0" class="space-y-3">
      <div
        v-for="model in displayModels"
        :key="model.id"
        class="group relative overflow-hidden rounded-xl border border-gray-100/50 bg-white/50 p-4 transition-all hover:-translate-y-0.5 hover:border-gray-200 hover:shadow-lg dark:border-dark-700/50 dark:bg-dark-800/30 dark:hover:border-dark-600"
      >
        <!-- Model Header -->
        <div class="mb-2 flex items-start justify-between">
          <div class="flex items-center gap-3">
            <!-- Provider Icon -->
            <div
              :class="[
                'flex h-10 w-10 items-center justify-center rounded-lg text-sm font-semibold transition-transform group-hover:scale-110',
                providerColorClass(model.provider),
              ]"
            >
              {{ providerInitial(model.provider) }}
            </div>
            <!-- Model Name -->
            <div>
              <h4 class="font-medium text-gray-900 dark:text-white">
                {{ model.name }}
              </h4>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ model.provider }}
              </p>
            </div>
          </div>
          <!-- Status Badge -->
          <span
            v-if="model.status"
            :class="[
              'rounded-full px-2 py-0.5 text-xs font-medium',
              statusClass(model.status),
            ]"
          >
            {{ statusLabel(model.status) }}
          </span>
        </div>

        <!-- Model Features/Tags -->
        <div v-if="model.tags && model.tags.length > 0" class="flex flex-wrap gap-1.5">
          <span
            v-for="tag in model.tags"
            :key="tag"
            class="rounded-md bg-gray-100/80 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-700/50 dark:text-gray-300"
          >
            {{ tag }}
          </span>
        </div>

        <!-- Pricing Info (if available) -->
        <div
          v-if="model.pricing"
          class="mt-3 flex items-center gap-4 border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700/50 dark:text-gray-400"
        >
          <div v-if="model.pricing.input" class="flex items-center gap-1">
            <Icon name="arrowDown" size="xs" />
            <span>${{ model.pricing.input }}/M tokens</span>
          </div>
          <div v-if="model.pricing.output" class="flex items-center gap-1">
            <Icon name="arrowUp" size="xs" />
            <span>${{ model.pricing.output }}/M tokens</span>
          </div>
        </div>
      </div>

      <!-- Show More Button -->
      <button
        v-if="hasMore && !showAll"
        @click="showAll = true"
        class="w-full rounded-xl border border-gray-200 bg-white/50 py-2.5 text-sm font-medium text-gray-600 transition-all hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800/30 dark:text-gray-300 dark:hover:bg-dark-800/50"
      >
        {{ t('common.showMore') }} ({{ models.length - initialDisplayCount }})
      </button>
    </div>

    <!-- Empty State -->
    <div v-else class="flex flex-col items-center justify-center py-12 text-center">
      <div
        class="mb-3 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-800"
      >
        <Icon name="cube" size="lg" class="text-gray-400 dark:text-gray-500" />
      </div>
      <p class="text-sm font-medium text-gray-900 dark:text-white">
        {{ t('models.noModelsAvailable') }}
      </p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('models.noModelsDescription') }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { platformBadgeLightClass } from '@/utils/platformColors'

export interface ModelInfo {
  id: string
  name: string
  provider: string
  status?: 'active' | 'beta' | 'deprecated'
  tags?: string[]
  pricing?: {
    input?: number
    output?: number
  }
}

const props = withDefaults(
  defineProps<{
    title?: string
    models: ModelInfo[]
    loading?: boolean
    initialDisplayCount?: number
  }>(),
  {
    title: 'Available Models',
    initialDisplayCount: 6,
  }
)

const { t } = useI18n()
const showAll = ref(false)

const modelCount = computed(() => props.models.length)

const hasMore = computed(() => props.models.length > props.initialDisplayCount)

const displayModels = computed(() => {
  if (showAll.value || !hasMore.value) {
    return props.models
  }
  return props.models.slice(0, props.initialDisplayCount)
})

const providerInitial = (provider: string): string => {
  return provider.charAt(0).toUpperCase()
}

const providerColorClass = (provider: string): string => {
  return platformBadgeLightClass(provider)
}

const statusClass = (status: string): string => {
  const classes: Record<string, string> = {
    active: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400',
    beta: 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
    deprecated: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400',
  }
  return classes[status] || classes.active
}

const statusLabel = (status: string): string => {
  const labels: Record<string, string> = {
    active: t('common.active'),
    beta: t('common.beta'),
    deprecated: t('common.deprecated'),
  }
  return labels[status] || status
}
</script>
