<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="page-header">
        <h1 class="page-title">{{ t('models.title') }}</h1>
        <p class="page-description">{{ t('models.description') }}</p>
      </div>

      <!-- Available Models Card -->
      <AvailableModelsCard
        :title="t('models.availableModels')"
        :models="models"
        :loading="loading"
        :initial-display-count="8"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AvailableModelsCard from '@/components/model/AvailableModelsCard.vue'
import type { ModelInfo } from '@/components/model/AvailableModelsCard.vue'

const { t } = useI18n()
const loading = ref(true)
const models = ref<ModelInfo[]>([])

// 示例数据
const fetchModels = async () => {
  loading.value = true
  try {
    // 模拟 API 调用
    await new Promise((resolve) => setTimeout(resolve, 1000))

    // 示例模型数据
    models.value = [
      {
        id: 'claude-opus-4',
        name: 'Claude Opus 4',
        provider: 'Anthropic',
        status: 'active',
        tags: ['200K context', 'Vision', 'Extended thinking'],
        pricing: { input: 15, output: 75 }
      },
      {
        id: 'claude-sonnet-4',
        name: 'Claude Sonnet 4',
        provider: 'Anthropic',
        status: 'active',
        tags: ['200K context', 'Vision', 'Fast'],
        pricing: { input: 3, output: 15 }
      },
      {
        id: 'gpt-4-turbo',
        name: 'GPT-4 Turbo',
        provider: 'OpenAI',
        status: 'active',
        tags: ['128K context', 'Vision', 'JSON mode'],
        pricing: { input: 10, output: 30 }
      },
      {
        id: 'gpt-4o',
        name: 'GPT-4o',
        provider: 'OpenAI',
        status: 'active',
        tags: ['128K context', 'Vision', 'Audio', 'Fast'],
        pricing: { input: 5, output: 15 }
      },
      {
        id: 'gemini-pro-1.5',
        name: 'Gemini 1.5 Pro',
        provider: 'Google',
        status: 'active',
        tags: ['1M context', 'Vision', 'Multimodal'],
        pricing: { input: 3.5, output: 10.5 }
      },
      {
        id: 'gemini-flash-1.5',
        name: 'Gemini 1.5 Flash',
        provider: 'Google',
        status: 'active',
        tags: ['1M context', 'Fast', 'Cost-effective'],
        pricing: { input: 0.35, output: 1.05 }
      },
      {
        id: 'llama-3.1-405b',
        name: 'Llama 3.1 405B',
        provider: 'Meta',
        status: 'active',
        tags: ['128K context', 'Open source'],
        pricing: { input: 3, output: 6 }
      },
      {
        id: 'mistral-large-2',
        name: 'Mistral Large 2',
        provider: 'Mistral',
        status: 'active',
        tags: ['128K context', 'Fast'],
        pricing: { input: 3, output: 9 }
      },
      {
        id: 'claude-3-opus',
        name: 'Claude 3 Opus',
        provider: 'Anthropic',
        status: 'deprecated',
        tags: ['200K context', 'Vision'],
        pricing: { input: 15, output: 75 }
      },
    ]
  } catch (error) {
    console.error('Failed to fetch models:', error)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchModels()
})
</script>
