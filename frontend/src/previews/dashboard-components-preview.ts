import { Chart, ArcElement, DoughnutController, Tooltip } from 'chart.js'
import './dashboard-components-preview.css'

Chart.register(ArcElement, DoughnutController, Tooltip)

type TokenPeriod = 'today' | 'total'
type GroupState = 'many' | 'one' | 'empty'

const tokenData: Record<TokenPeriod, {
  total: string
  values: Array<{ label: string; value: number; display: string; color: string }>
}> = {
  today: {
    total: '360.5M',
    values: [
      { label: '输入', value: 278, display: '278.0M', color: '#4f7cff' },
      { label: '输出', value: 34, display: '34.0M', color: '#20d9a0' },
      { label: '缓存创建', value: 16, display: '16.0M', color: '#ffb84d' },
      { label: '缓存读取', value: 32.5, display: '32.5M', color: '#28c7c0' },
    ],
  },
  total: {
    total: '14.26B',
    values: [
      { label: '输入', value: 9.12, display: '9.12B', color: '#4f7cff' },
      { label: '输出', value: 1.62, display: '1.62B', color: '#20d9a0' },
      { label: '缓存创建', value: 0.34, display: '340M', color: '#ffb84d' },
      { label: '缓存读取', value: 3.18, display: '3.18B', color: '#28c7c0' },
    ],
  },
}

const surfaceButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-surface]'))
const surfaces = Array.from(document.querySelectorAll<HTMLElement>('[data-surface-panel]'))
const periodButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-token-period]'))
const groupStateButtons = Array.from(document.querySelectorAll<HTMLButtonElement>('[data-group-state]'))
const tokenTotal = document.querySelector<HTMLElement>('#token-total')
const tokenLegend = document.querySelector<HTMLElement>('#token-legend')
const groupContent = document.querySelector<HTMLElement>('#group-content')

let tokenPeriod: TokenPeriod = 'today'
let groupState: GroupState = 'many'
let tokenChart: Chart<'doughnut'> | null = null
let groupChart: Chart<'doughnut'> | null = null

function createRing(
  id: string,
  data: number[],
  colors: string[],
  cutout = '78%',
): Chart<'doughnut'> | null {
  const canvas = document.querySelector<HTMLCanvasElement>(`#${id}`)
  if (!canvas) return null
  return new Chart(canvas, {
    type: 'doughnut',
    data: {
      datasets: [{
        data,
        backgroundColor: colors,
        borderWidth: 0,
        borderRadius: 8,
        spacing: 2,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      cutout,
      animation: { duration: 700, easing: 'easeOutQuart' },
      plugins: { legend: { display: false }, tooltip: { enabled: false } },
    },
  })
}

createRing('api-key-ring', [9, 1], ['#20d9a0', '#1c2b41'])
createRing('account-health-ring', [41, 4, 3, 2], ['#20d9a0', '#ff6174', '#ffb84d', '#6e8cff'])

function renderTokenComposition(): void {
  const data = tokenData[tokenPeriod]
  tokenChart?.destroy()
  tokenChart = createRing('token-ring', data.values.map((item) => item.value), data.values.map((item) => item.color), '72%')
  if (tokenTotal) tokenTotal.textContent = data.total
  if (tokenLegend) {
    const total = data.values.reduce((sum, item) => sum + item.value, 0)
    tokenLegend.innerHTML = data.values.map((item) => `
      <article>
        <span><i style="--legend-color:${item.color}"></i>${item.label}</span>
        <strong>${item.display}</strong>
        <small>${((item.value / total) * 100).toFixed(1)}%</small>
      </article>
    `).join('')
  }
}

function renderGroupState(): void {
  groupChart?.destroy()
  groupChart = null
  if (!groupContent) return

  if (groupState === 'empty') {
    groupContent.innerHTML = `
      <div class="group-empty">
        <strong>当前时间范围暂无分组用量</strong>
        <span>产生请求后，分组构成会自动出现在这里</span>
      </div>
    `
    return
  }

  if (groupState === 'one') {
    groupContent.innerHTML = `
      <div class="single-group">
        <span class="single-badge">唯一分组</span>
        <strong>Production</strong>
        <b>$14,960.32</b>
        <div><span>101,984 请求</span><span>14.26B Token</span></div>
      </div>
    `
    return
  }

  const groups = [
    { name: 'Production', value: 8942.16, color: '#4f7cff' },
    { name: 'Development', value: 3180.04, color: '#20d9a0' },
    { name: 'Research', value: 1842.76, color: '#ffb84d' },
    { name: '其他', value: 995.36, color: '#728198' },
  ]
  const total = groups.reduce((sum, item) => sum + item.value, 0)
  groupContent.innerHTML = `
    <div class="group-layout">
      <div class="ring-stage group-ring-stage">
        <canvas id="group-ring" aria-label="分组使用构成"></canvas>
        <div class="ring-center"><strong>$14.96K</strong><span>实际消费</span></div>
      </div>
      <div class="group-list">
        ${groups.map((group) => `
          <article>
            <span><i style="--legend-color:${group.color}"></i>${group.name}</span>
            <strong>$${group.value.toLocaleString('en-US', { maximumFractionDigits: 0 })}</strong>
            <small>${((group.value / total) * 100).toFixed(1)}%</small>
          </article>
        `).join('')}
      </div>
    </div>
  `
  groupChart = createRing('group-ring', groups.map((item) => item.value), groups.map((item) => item.color), '72%')
}

surfaceButtons.forEach((button) => {
  button.addEventListener('click', () => {
    const target = button.dataset.surface
    surfaceButtons.forEach((item) => {
      const selected = item === button
      item.classList.toggle('is-active', selected)
      item.setAttribute('aria-selected', String(selected))
    })
    surfaces.forEach((surface) => surface.classList.toggle('is-active', surface.dataset.surfacePanel === target))
  })
})

periodButtons.forEach((button) => {
  button.addEventListener('click', () => {
    tokenPeriod = button.dataset.tokenPeriod as TokenPeriod
    periodButtons.forEach((item) => {
      const selected = item === button
      item.classList.toggle('is-active', selected)
      item.setAttribute('aria-selected', String(selected))
    })
    renderTokenComposition()
  })
})

groupStateButtons.forEach((button) => {
  button.addEventListener('click', () => {
    groupState = button.dataset.groupState as GroupState
    groupStateButtons.forEach((item) => {
      const selected = item === button
      item.classList.toggle('is-active', selected)
      item.setAttribute('aria-selected', String(selected))
    })
    renderGroupState()
  })
})

renderTokenComposition()
renderGroupState()
