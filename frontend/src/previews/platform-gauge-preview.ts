import { Chart, ArcElement, DoughnutController, Tooltip } from 'chart.js'
import './platform-gauge-preview.css'

Chart.register(ArcElement, DoughnutController, Tooltip)

type MetricKey = 'spend' | 'requests' | 'tokens'

interface PlatformMetric {
  id: string
  name: string
  shortName: string
  model: string
  accent: string
  mutedAccent: string
  todayCost: number
  spend: number
  requests: number
  tokens: number
}

interface ModelMetric {
  name: string
  platform: string
  platformCode: string
  requests: number
  tokens: number
  cost: number
  accent: string
}

const platforms: PlatformMetric[] = [
  {
    id: 'claude',
    name: 'Claude',
    shortName: 'C',
    model: 'Anthropic',
    accent: '#20d9a0',
    mutedAccent: 'rgba(32, 217, 160, 0.18)',
    todayCost: 86.18,
    spend: 4280.72,
    requests: 18426,
    tokens: 2_840_000_000,
  },
  {
    id: 'openai',
    name: 'OpenAI',
    shortName: 'O',
    model: 'OpenAI Platform',
    accent: '#ffb84d',
    mutedAccent: 'rgba(255, 184, 77, 0.18)',
    todayCost: 611.23,
    spend: 9880.16,
    requests: 72616,
    tokens: 9_800_000_000,
  },
  {
    id: 'gemini',
    name: 'Gemini',
    shortName: 'G',
    model: 'Google AI',
    accent: '#6e8cff',
    mutedAccent: 'rgba(110, 140, 255, 0.18)',
    todayCost: 25.63,
    spend: 799.44,
    requests: 10942,
    tokens: 1_620_000_000,
  },
]

const gaugeGrid = document.querySelector<HTMLDivElement>('#gauge-grid')
const metricButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-metric]'))
const refreshButton = document.querySelector<HTMLButtonElement>('#refresh-button')
const toast = document.querySelector<HTMLDivElement>('#toast')
const syncTime = document.querySelector<HTMLTimeElement>('#sync-time')
const modelCountSelect = document.querySelector<HTMLSelectElement>('#model-count')
const modelList = document.querySelector<HTMLDivElement>('#model-list')
const modelCountCaption = document.querySelector<HTMLSpanElement>('#model-count-caption')
const charts = new Map<string, Chart<'doughnut'>>()
let activeMetric: MetricKey = 'spend'
let modelCount = 3

const models: ModelMetric[] = [
  { name: 'gpt-5.6-sol', platform: 'OpenAI', platformCode: 'O', requests: 15246, tokens: 2_157_900_000, cost: 4393.2738, accent: '#ffb84d' },
  { name: 'gpt-5.5', platform: 'OpenAI', platformCode: 'O', requests: 936, tokens: 116_700_000, cost: 220.4922, accent: '#ffb84d' },
  { name: 'gpt-5.6-luna', platform: 'OpenAI', platformCode: 'O', requests: 739, tokens: 80_000_000, cost: 47.3681, accent: '#ffb84d' },
  { name: 'gpt-5.4', platform: 'OpenAI', platformCode: 'O', requests: 382, tokens: 22_300_000, cost: 35.2628, accent: '#ffb84d' },
  { name: 'claude-opus-4-8', platform: 'Claude', platformCode: 'C', requests: 25, tokens: 393_100, cost: 1.6293, accent: '#20d9a0' },
  { name: 'gemini-2.5-pro', platform: 'Gemini', platformCode: 'G', requests: 18, tokens: 218_000, cost: 0.8421, accent: '#6e8cff' },
]

function money(value: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 0,
  }).format(value)
}

function compact(value: number): string {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return value.toLocaleString('en-US')
}

function metricLabel(metric: MetricKey): string {
  return { spend: '消费占比', requests: '请求占比', tokens: 'Token 占比' }[metric]
}

function metricValue(platform: PlatformMetric, metric: MetricKey): string {
  if (metric === 'spend') return money(platform.spend)
  return compact(platform[metric])
}

function totalFor(metric: MetricKey): number {
  return platforms.reduce((total, platform) => total + platform[metric], 0)
}

function shareFor(platform: PlatformMetric, metric: MetricKey): number {
  return Math.round((platform[metric] / totalFor(metric)) * 100)
}

function statusFor(percent: number): { label: string; className: string } {
  if (percent >= 60) return { label: '主力平台', className: 'warning' }
  if (percent >= 20) return { label: '稳定贡献', className: 'healthy' }
  return { label: '低频使用', className: 'neutral' }
}

function cardTemplate(platform: PlatformMetric): string {
  const percent = shareFor(platform, activeMetric)
  const status = statusFor(percent)
  const total = totalFor(activeMetric)

  return `
    <article class="gauge-card" data-platform="${platform.id}" style="--accent:${platform.accent};--accent-muted:${platform.mutedAccent}">
      <div class="platform-heading">
        <div class="platform-ident">
          <span class="platform-mark">${platform.shortName}</span>
          <div>
            <h3>${platform.name}</h3>
            <span>${platform.model}</span>
          </div>
        </div>
        <span class="status-pill ${status.className}">
          <i aria-hidden="true"></i>${status.label}
        </span>
      </div>

      <div class="gauge-stage">
        <canvas id="gauge-${platform.id}" aria-label="${platform.name} ${metricLabel(activeMetric)} ${percent}%"></canvas>
        <div class="gauge-readout">
          <span class="readout-label">SHARE</span>
          <strong class="readout-value">${percent}<small>%</small></strong>
          <span class="readout-delta">${metricValue(platform, activeMetric)}</span>
        </div>
        <span class="scale scale-start">0</span>
        <span class="scale scale-mid">50</span>
        <span class="scale scale-end">100</span>
      </div>

      <div class="quota-row">
        <div>
          <span>平台${metricLabel(activeMetric).replace('占比', '')}</span>
          <strong class="quota-used">${metricValue(platform, activeMetric)}</strong>
        </div>
        <div class="quota-limit">
          <span>全平台总量</span>
          <strong>${activeMetric === 'spend' ? money(total) : compact(total)}</strong>
        </div>
      </div>

      <div class="metric-row">
        <div><span>请求</span><strong>${platform.requests.toLocaleString('en-US')}</strong></div>
        <div><span>Token</span><strong>${compact(platform.tokens)}</strong></div>
        <div><span>今日消费</span><strong>${money(platform.todayCost)}</strong></div>
      </div>

      <footer class="card-footer">
        <span>数据口径：累计使用</span>
        <span class="pulse-line"><i></i><i></i><i></i><i></i><i></i><i></i></span>
      </footer>
    </article>
  `
}

function renderCards(): void {
  if (!gaugeGrid) return

  for (const chart of charts.values()) chart.destroy()
  charts.clear()
  gaugeGrid.innerHTML = platforms.map(cardTemplate).join('')

  for (const platform of platforms) {
    const canvas = document.querySelector<HTMLCanvasElement>(`#gauge-${platform.id}`)
    if (!canvas) continue

    const percent = shareFor(platform, activeMetric)
    const chart = new Chart(canvas, {
      type: 'doughnut',
      data: {
        datasets: [{
          data: [percent, 100 - percent],
          backgroundColor: [platform.accent, 'rgba(127, 151, 188, 0.10)'],
          borderWidth: 0,
          borderRadius: 14,
          spacing: 2,
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: {
          duration: 900,
          easing: 'easeOutQuart',
        },
        rotation: -130,
        circumference: 260,
        cutout: '82%',
        plugins: {
          legend: { display: false },
          tooltip: { enabled: false },
        },
      },
    })
    charts.set(platform.id, chart)
  }

}

function updateSyncTime(): void {
  if (!syncTime) return
  const now = new Date()
  syncTime.textContent = now.toLocaleTimeString('zh-CN', { hour12: false })
  syncTime.dateTime = now.toISOString()
}

function renderModels(): void {
  if (!modelList || !modelCountCaption) return

  const sortedModels = [...models].sort((a, b) => b.cost - a.cost)
  const visibleModels = sortedModels.slice(0, modelCount)
  const maxCost = sortedModels[0]?.cost ?? 0

  if (visibleModels.length === 0) {
    modelList.innerHTML = `
      <div class="model-empty">
        <strong>当前时间范围暂无模型消耗</strong>
        <span>产生请求后，模型排行会自动出现在这里</span>
      </div>
    `
    modelCountCaption.textContent = '当前没有已使用模型'
    return
  }

  modelList.innerHTML = visibleModels.map((model, index) => {
    const width = maxCost > 0 ? Math.max(2, (model.cost / maxCost) * 100) : 0
    return `
      <article class="model-row" style="--model-accent:${model.accent};--bar-width:${width}%">
        <span class="model-bar" aria-hidden="true"></span>
        <div class="model-name-cell">
          <span class="rank">${String(index + 1).padStart(2, '0')}</span>
          <strong>${model.name}</strong>
        </div>
        <div class="model-platform-cell">
          <span class="model-platform-mark">${model.platformCode}</span>
          <span>${model.platform}</span>
        </div>
        <strong class="numeric-cell">${model.requests.toLocaleString('en-US')}</strong>
        <strong class="numeric-cell">${compact(model.tokens)}</strong>
        <strong class="numeric-cell model-cost">${money(model.cost)}</strong>
      </article>
    `
  }).join('')

  modelCountCaption.textContent = `展示 ${visibleModels.length} / 共 ${sortedModels.length} 个已使用模型`
}

metricButtons.forEach((button) => {
  button.addEventListener('click', () => {
    activeMetric = button.dataset.metric as MetricKey
    metricButtons.forEach((item) => {
      const selected = item === button
      item.classList.toggle('is-active', selected)
      item.setAttribute('aria-selected', String(selected))
    })
    renderCards()
  })
})

refreshButton?.addEventListener('click', () => {
  refreshButton.classList.add('is-refreshing')
  window.setTimeout(() => refreshButton.classList.remove('is-refreshing'), 700)
  updateSyncTime()
  if (toast) {
    toast.classList.add('is-visible')
    window.setTimeout(() => toast.classList.remove('is-visible'), 1800)
  }
  renderCards()
  renderModels()
})

modelCountSelect?.addEventListener('change', () => {
  modelCount = Number(modelCountSelect.value)
  renderModels()
})

updateSyncTime()
renderCards()
renderModels()
