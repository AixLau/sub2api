<template>
  <section id="models" class="content-section model-catalog-section" aria-labelledby="models-title">
    <div class="content-inner">
      <div class="model-catalog-header">
        <div class="section-heading">
          <p>Model catalog</p>
          <h2 id="models-title">模型与价格，<br />接入前就看清。</h2>
          <span>查看当前可用模型、计费方式与公开价格。</span>
        </div>
        <router-link class="inline-action" to="/register">
          注册并开始调用
          <ArrowUpRightIcon />
        </router-link>
      </div>

      <div class="model-catalog-toolbar">
        <div class="model-filter-list" role="group" aria-label="模型平台筛选">
          <button
            v-for="filter in filterOptions"
            :key="filter.id"
            class="model-filter-button"
            type="button"
            :aria-pressed="activeFilter === filter.id"
            @click="activeFilter = filter.id"
          >
            {{ filter.label }}
          </button>
        </div>

        <div class="model-catalog-tools">
          <span v-if="status === 'ready'" class="model-result-count">
            {{ filteredModels.length }} 个模型
          </span>
          <label class="model-search-field">
            <svg
              aria-hidden="true"
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.3-4.3" />
            </svg>
            <span class="sr-only">搜索模型</span>
            <input
              v-model="query"
              placeholder="搜索模型名称"
              :disabled="status !== 'ready' || models.length === 0"
            />
          </label>
        </div>
      </div>

      <div class="model-catalog-content" aria-live="polite" :aria-busy="status === 'loading'">
        <div v-if="status === 'loading'" class="model-catalog-state">
          <span class="model-loading-indicator" aria-hidden="true" />
          <strong>正在读取可用模型</strong>
          <p>模型信息正在更新，请稍候。</p>
        </div>

        <div v-else-if="status === 'error'" class="model-catalog-state">
          <strong>模型目录暂时无法加载</strong>
          <p>请稍后重试。</p>
          <button class="inline-action" type="button" @click="loadModels">重新加载</button>
        </div>

        <div v-else-if="models.length === 0" class="model-catalog-state">
          <strong>当前没有可公开调用的模型</strong>
          <p>请稍后再来查看。</p>
        </div>

        <div v-else-if="filteredModels.length === 0" class="model-catalog-state">
          <strong>没有找到匹配模型</strong>
          <p>换一个模型名称，或切换到“全部模型”继续浏览。</p>
          <button class="inline-action" type="button" @click="clearFilters">清除筛选</button>
        </div>

        <div v-else class="model-card-grid">
          <article
            v-for="model in filteredModels"
            :key="`${model.platform}:${model.name}`"
            class="model-card"
          >
            <div class="model-card-topline">
              <span :class="`model-platform-badge model-platform-badge--${platformBadgeClass(model.platform)}`">
                {{ platformLabel(model.platform) }}
              </span>
            </div>
            <div>
              <h3>{{ model.name }}</h3>
              <p>{{ pricingModeLabel(model.pricing) }}</p>
            </div>

            <div v-if="!model.pricing" class="model-price-grid model-price-grid--unavailable">
              <div>
                <span>价格</span>
                <strong>暂未配置</strong>
              </div>
            </div>
            <div
              v-else-if="model.pricing.billing_mode === 'per_request' || model.pricing.billing_mode === 'video'"
              class="model-price-grid model-price-grid--compact"
            >
              <div>
                <span>单次请求</span>
                <strong>{{ formatPrice(model.pricing.per_request_price) }}</strong>
              </div>
            </div>
            <div v-else-if="model.pricing.billing_mode === 'image'" class="model-price-grid">
              <div>
                <span>图片输入 / 1M tokens</span>
                <strong>{{ formatPrice(model.pricing.image_input_price ?? model.pricing.input_price, 1_000_000) }}</strong>
              </div>
              <div>
                <span>图片输出</span>
                <strong>{{ formatPrice(model.pricing.image_output_price) }}</strong>
              </div>
            </div>
            <div v-else class="model-price-grid">
              <div>
                <span>输入 / 1M tokens</span>
                <strong>{{ formatPrice(model.pricing.input_price, 1_000_000) }}</strong>
              </div>
              <div>
                <span>输出 / 1M tokens</span>
                <strong>{{ formatPrice(model.pricing.output_price, 1_000_000) }}</strong>
              </div>
              <div>
                <span>缓存创建 / 1M tokens</span>
                <strong>{{ formatPrice(model.pricing.cache_write_price, 1_000_000) }}</strong>
              </div>
              <div>
                <span>缓存读取 / 1M tokens</span>
                <strong>{{ formatPrice(model.pricing.cache_read_price, 1_000_000) }}</strong>
              </div>
            </div>
          </article>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { fetchPublicModels, type PublicModel, type PublicModelPricing } from '@/api/models'

type CatalogStatus = 'loading' | 'ready' | 'error'

const platformPriority = ['openai', 'anthropic', 'gemini', 'grok', 'antigravity']

const ArrowUpRightIcon = () =>
  h(
    'svg',
    {
      'aria-hidden': 'true',
      width: 16,
      height: 16,
      viewBox: '0 0 24 24',
      fill: 'none',
      stroke: 'currentColor',
      'stroke-width': 2,
      'stroke-linecap': 'round',
      'stroke-linejoin': 'round'
    },
    [h('path', { d: 'M7 7h10v10' }), h('path', { d: 'M7 17 17 7' })]
  )

const models = ref<PublicModel[]>([])
const status = ref<CatalogStatus>('loading')
const activeFilter = ref('all')
const query = ref('')

let controller: AbortController | null = null

function normalizePlatform(platform: string) {
  return platform.trim().toLowerCase()
}

function platformLabel(platform: string) {
  switch (normalizePlatform(platform)) {
    case 'openai':
      return 'Codex / OpenAI'
    case 'anthropic':
      return 'Claude'
    case 'gemini':
      return 'Gemini'
    case 'grok':
      return 'Grok'
    case 'antigravity':
      return 'Antigravity'
    default:
      return platform || '其他平台'
  }
}

function platformBadgeClass(platform: string) {
  const normalized = normalizePlatform(platform)
  if (normalized === 'openai') return 'codex'
  if (normalized === 'anthropic') return 'claude'
  return normalized.replace(/[^a-z0-9_-]/g, '') || 'other'
}

function formatPrice(value: number | null, scale = 1) {
  if (value == null) return '—'
  if (value === 0) return '免费'
  return `$${new Intl.NumberFormat('en-US', { maximumFractionDigits: 6 }).format(value * scale)}`
}

function pricingModeLabel(pricing: PublicModelPricing | null) {
  switch (pricing?.billing_mode) {
    case 'image':
      return '图片计费'
    case 'per_request':
      return '按次计费'
    case 'video':
      return '视频计费'
    default:
      return 'Token 计费'
  }
}

async function loadModels() {
  controller?.abort()
  controller = new AbortController()
  status.value = 'loading'

  try {
    models.value = await fetchPublicModels(controller.signal)
    status.value = 'ready'
  } catch (error: unknown) {
    if (error instanceof DOMException && error.name === 'AbortError') return
    models.value = []
    status.value = 'error'
  }
}

function clearFilters() {
  activeFilter.value = 'all'
  query.value = ''
}

const filterOptions = computed(() => {
  const platforms = Array.from(new Set(models.value.map((model) => normalizePlatform(model.platform))))
    .filter(Boolean)
    .sort((left, right) => {
      const leftPriority = platformPriority.indexOf(left)
      const rightPriority = platformPriority.indexOf(right)
      if (leftPriority === -1 && rightPriority === -1) return left.localeCompare(right)
      if (leftPriority === -1) return 1
      if (rightPriority === -1) return -1
      return leftPriority - rightPriority
    })

  return [
    { id: 'all', label: '全部模型' },
    ...platforms.map((platform) => ({ id: platform, label: platformLabel(platform) }))
  ]
})

const filteredModels = computed(() => {
  const normalizedQuery = query.value.trim().toLowerCase()
  return models.value.filter((model) => {
    if (activeFilter.value !== 'all' && normalizePlatform(model.platform) !== activeFilter.value) {
      return false
    }
    if (!normalizedQuery) return true
    return `${model.name} ${model.platform} ${platformLabel(model.platform)}`
      .toLowerCase()
      .includes(normalizedQuery)
  })
})

onMounted(loadModels)

onBeforeUnmount(() => {
  controller?.abort()
})
</script>
