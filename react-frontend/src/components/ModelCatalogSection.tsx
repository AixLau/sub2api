import { ArrowUpRight, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import {
  fetchPublicModels,
  type PublicModel,
  type PublicModelPricing,
} from '../api/modelCatalog'

type CatalogStatus = 'loading' | 'ready' | 'error'

const platformPriority = ['openai', 'anthropic', 'gemini', 'grok', 'antigravity']

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

function PriceSummary({ model }: { model: PublicModel }) {
  const pricing = model.pricing
  if (!pricing) {
    return (
      <div className="model-price-grid model-price-grid--unavailable">
        <div>
          <span>价格</span>
          <strong>暂未配置</strong>
        </div>
      </div>
    )
  }

  if (pricing.billing_mode === 'per_request' || pricing.billing_mode === 'video') {
    return (
      <div className="model-price-grid model-price-grid--compact">
        <div>
          <span>单次请求</span>
          <strong>{formatPrice(pricing.per_request_price)}</strong>
        </div>
      </div>
    )
  }

  if (pricing.billing_mode === 'image') {
    return (
      <div className="model-price-grid">
        <div>
          <span>图片输入 / 1M tokens</span>
          <strong>{formatPrice(pricing.image_input_price ?? pricing.input_price, 1_000_000)}</strong>
        </div>
        <div>
          <span>图片输出</span>
          <strong>{formatPrice(pricing.image_output_price)}</strong>
        </div>
      </div>
    )
  }

  return (
    <div className="model-price-grid">
      <div>
        <span>输入 / 1M tokens</span>
        <strong>{formatPrice(pricing.input_price, 1_000_000)}</strong>
      </div>
      <div>
        <span>输出 / 1M tokens</span>
        <strong>{formatPrice(pricing.output_price, 1_000_000)}</strong>
      </div>
      <div>
        <span>缓存创建 / 1M tokens</span>
        <strong>{formatPrice(pricing.cache_write_price, 1_000_000)}</strong>
      </div>
      <div>
        <span>缓存读取 / 1M tokens</span>
        <strong>{formatPrice(pricing.cache_read_price, 1_000_000)}</strong>
      </div>
    </div>
  )
}

export function ModelCatalogSection() {
  const [models, setModels] = useState<PublicModel[]>([])
  const [status, setStatus] = useState<CatalogStatus>('loading')
  const [reloadRevision, setReloadRevision] = useState(0)
  const [activeFilter, setActiveFilter] = useState('all')
  const [query, setQuery] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    setStatus('loading')

    fetchPublicModels(controller.signal)
      .then((nextModels) => {
        setModels(nextModels)
        setStatus('ready')
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === 'AbortError') return
        setModels([])
        setStatus('error')
      })

    return () => controller.abort()
  }, [reloadRevision])

  const filterOptions = useMemo(() => {
    const platforms = Array.from(new Set(models.map((model) => normalizePlatform(model.platform))))
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
      ...platforms.map((platform) => ({ id: platform, label: platformLabel(platform) })),
    ]
  }, [models])

  const filteredModels = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase()
    return models.filter((model) => {
      if (activeFilter !== 'all' && normalizePlatform(model.platform) !== activeFilter) return false
      if (!normalizedQuery) return true
      return `${model.name} ${model.platform} ${platformLabel(model.platform)}`
        .toLowerCase()
        .includes(normalizedQuery)
    })
  }, [activeFilter, models, query])

  return (
    <section className="content-section model-catalog-section" id="models" aria-labelledby="models-title">
      <div className="content-inner">
        <div className="model-catalog-header">
          <div className="section-heading">
            <p>Model catalog</p>
            <h2 id="models-title">
              模型与价格，<br />接入前就看清。
            </h2>
            <span>
              查看当前可用模型、计费方式与公开价格。
            </span>
          </div>
          <a className="inline-action" href="/register">
            注册并开始调用
            <ArrowUpRight aria-hidden="true" size={16} />
          </a>
        </div>

        <div className="model-catalog-toolbar">
          <div className="model-filter-list" role="group" aria-label="模型平台筛选">
            {filterOptions.map((filter) => (
              <button
                className="model-filter-button"
                type="button"
                aria-pressed={activeFilter === filter.id}
                key={filter.id}
                onClick={() => setActiveFilter(filter.id)}
              >
                {filter.label}
              </button>
            ))}
          </div>

          <div className="model-catalog-tools">
            {status === 'ready' && <span className="model-result-count">{filteredModels.length} 个模型</span>}
            <label className="model-search-field">
              <Search aria-hidden="true" size={16} />
              <span className="sr-only">搜索模型</span>
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索模型名称"
                disabled={status !== 'ready' || models.length === 0}
              />
            </label>
          </div>
        </div>

        <div className="model-catalog-content" aria-live="polite" aria-busy={status === 'loading'}>
          {status === 'loading' ? (
            <div className="model-catalog-state">
              <span className="model-loading-indicator" aria-hidden="true" />
              <strong>正在读取可用模型</strong>
              <p>模型信息正在更新，请稍候。</p>
            </div>
          ) : status === 'error' ? (
            <div className="model-catalog-state">
              <strong>模型目录暂时无法加载</strong>
              <p>请稍后重试。</p>
              <button className="inline-action" type="button" onClick={() => setReloadRevision((value) => value + 1)}>
                重新加载
              </button>
            </div>
          ) : models.length === 0 ? (
            <div className="model-catalog-state">
              <strong>当前没有可公开调用的模型</strong>
              <p>请稍后再来查看。</p>
            </div>
          ) : filteredModels.length === 0 ? (
            <div className="model-catalog-state">
              <strong>没有找到匹配模型</strong>
              <p>换一个模型名称，或切换到“全部模型”继续浏览。</p>
              <button
                className="inline-action"
                type="button"
                onClick={() => {
                  setActiveFilter('all')
                  setQuery('')
                }}
              >
                清除筛选
              </button>
            </div>
          ) : (
            <div className="model-card-grid">
              {filteredModels.map((model) => (
                <article className="model-card" key={`${model.platform}:${model.name}`}>
                  <div className="model-card-topline">
                    <span className={`model-platform-badge model-platform-badge--${platformBadgeClass(model.platform)}`}>
                      {platformLabel(model.platform)}
                    </span>
                  </div>
                  <div>
                    <h3>{model.name}</h3>
                    <p>{pricingModeLabel(model.pricing)}</p>
                  </div>
                  <PriceSummary model={model} />
                </article>
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  )
}
