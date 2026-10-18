import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { UserBreakdownItem } from '@/types'

import ModelDistributionChart from '../ModelDistributionChart.vue'

const { getUserBreakdown } = vi.hoisted(() => ({
  getUserBreakdown: vi.fn(),
}))

const messages: Record<string, string> = {
  'admin.dashboard.modelDistribution': 'Model Distribution',
  'admin.dashboard.spendingRankingTitle': 'User Spending Ranking',
  'admin.dashboard.viewModelDistribution': 'Model Distribution',
  'admin.dashboard.viewSpendingRanking': 'User Spending Ranking',
  'admin.dashboard.spendingRankingUser': 'User',
  'admin.dashboard.spendingRankingRequests': 'Requests',
  'admin.dashboard.spendingRankingTokens': 'Tokens',
  'admin.dashboard.spendingRankingSpend': 'Spend',
  'admin.dashboard.spendingRankingOther': 'Others',
  'admin.dashboard.model': 'Model',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.accountCost': 'Account Cost',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
  'admin.redeem.userPrefix': 'User #{id}',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const message = messages[key] ?? key
        return params
          ? message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name]))
          : message
      },
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

vi.mock('@/api/admin/dashboard', () => ({
  getUserBreakdown,
}))

describe('ModelDistributionChart', () => {
  beforeEach(() => {
    getUserBreakdown.mockReset().mockResolvedValue({ users: [] })
  })

  const modelStats = [
    {
      model: 'model-a',
      requests: 8,
      input_tokens: 100,
      output_tokens: 50,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 1000,
      cost: 1.5,
      actual_cost: 0.2,
      account_cost: 0.3,
    },
    {
      model: 'model-b',
      requests: 3,
      input_tokens: 40,
      output_tokens: 20,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 500,
      cost: 0.5,
      actual_cost: 1.4,
      account_cost: 0.6,
    },
  ]

  it('uses total_tokens and token ordering by default', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['model-a', 'model-b'])
    expect(chartData.datasets[0].data).toEqual([1000, 500])
    expect(chartData.datasets[0]).toMatchObject({
      borderRadius: 8,
      spacing: 2,
      hoverOffset: 4,
    })
    expect(chartData.datasets[0].backgroundColor.slice(0, 2)).toEqual(['#3B82F6', '#20D9A0'])
    expect(wrapper.get('[data-testid="model-ring-center"]').text()).toContain('1.50K')

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('model-a')
    expect(rows[1].text()).toContain('model-b')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    expect(options.cutout).toBe('72%')
    expect(options.animation.duration).toBe(700)
    const label = options.plugins.tooltip.callbacks.label({
      label: 'model-a',
      raw: 1000,
      dataset: { data: [1000, 500] },
    })
    expect(label).toBe('model-a: 1.00K (66.7%)')
  })

  it('uses actual_cost and reorders rows in actual cost mode', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
        metric: 'actual_cost',
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['model-b', 'model-a'])
    expect(chartData.datasets[0].data).toEqual([1.4, 0.2])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('model-b')
    expect(rows[1].text()).toContain('model-a')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'model-b',
      raw: 1.4,
      dataset: { data: [1.4, 0.2] },
    })
    expect(label).toBe('model-b: $1.40 (87.5%)')
  })

  it('can hide account cost for user usage stats without account_cost', () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
        showAccountCost: false,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.text()).not.toContain('Account Cost')
    expect(wrapper.findAll('thead th')).toHaveLength(5)
    expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(5)
  })

  it('uses the dashboard user label policy and renders Others with a dedicated chart color', async () => {
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats: [],
        enableRankingView: true,
        rankingItems: [
          { user_id: 1, email: 'alpha@example.com', username: 'alpha', actual_cost: 12, requests: 10, tokens: 1000 },
          { user_id: 2, email: 'beta@example.com', username: '   ', actual_cost: 8, requests: 6, tokens: 600 },
          { user_id: 3, email: '   ', username: '', actual_cost: 0, requests: 0, tokens: 0 },
        ],
        rankingTotalActualCost: 30,
        rankingTotalRequests: 20,
        rankingTotalTokens: 2000,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const rankingButton = wrapper.findAll('button').find((button) => button.text() === 'User Spending Ranking')
    expect(rankingButton).toBeTruthy()
    await rankingButton!.trigger('click')

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual([
      '#1 alpha',
      '#2 beta@example.com',
      '#3 User #3',
      'Others',
    ])
    expect(chartData.datasets[0].data).toEqual([12, 8, 0, 10])
    expect(chartData.datasets[0].backgroundColor[0]).toBe('#3B82F6')
    expect(chartData.datasets[0].backgroundColor[3]).toBe('#9CA3AF')
    expect(chartData.datasets[0].backgroundColor[3]).not.toBe(chartData.datasets[0].backgroundColor[0])
    expect(wrapper.get('[data-testid="ranking-ring-center"]').text()).toContain('$30.00')

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(4)
    expect(rows[0].text()).toContain('alpha')
    expect(rows[0].text()).not.toContain('alpha@example.com')
    expect(rows[1].text()).toContain('beta@example.com')
    expect(rows[2].text()).toContain('User #3')
    expect(rows[3].text()).toContain('Others')
    expect(rows[3].text()).toContain('4')
    expect(rows[3].text()).toContain('400')
    expect(rows[3].text()).toContain('$10.00')
  })

  it('passes the exact snapshot range and exclusions to user breakdown requests', async () => {
    getUserBreakdown.mockResolvedValue({ users: [] })
    const wrapper = mount(ModelDistributionChart, {
      props: {
        modelStats,
        startDate: '2026-05-31',
        endDate: '2026-06-01',
        filters: {
          exclude_user_ids: '101,202',
          start_time: '2026-05-31T08:42:00.000Z',
          end_time: '2026-06-01T08:42:00.000Z',
        },
      },
      global: {
        stubs: {
          LoadingSpinner: true,
          UserBreakdownSubTable: true,
        },
      },
    })

    await wrapper.find('tbody tr').trigger('click')

    expect(getUserBreakdown).toHaveBeenCalledWith(
      expect.objectContaining({
        exclude_user_ids: '101,202',
        start_date: '2026-05-31',
        end_date: '2026-06-01',
        start_time: '2026-05-31T08:42:00.000Z',
        end_time: '2026-06-01T08:42:00.000Z',
        model: 'model-a',
        model_source: 'requested',
      })
    )
  })

  const mountBreakdownChart = () => mount(ModelDistributionChart, {
    props: { modelStats },
    global: { stubs: { LoadingSpinner: true, UserBreakdownSubTable: true } },
  })

  const user: UserBreakdownItem = {
    user_id: 1, email: 'user@example.com', requests: 3,
    input_tokens: 40, output_tokens: 20, cache_tokens: 0, total_tokens: 60,
    cost: 0.5, actual_cost: 0.2, account_cost: 0.3,
  }

  it.each(['resolve', 'reject'] as const)('ignores an outdated request that will %s after another model opens', async (outcome) => {
    let resolveFirst!: (value: { users: UserBreakdownItem[] }) => void
    let rejectFirst!: (reason: Error) => void
    getUserBreakdown.mockReturnValueOnce(new Promise((resolve, reject) => {
      resolveFirst = resolve
      rejectFirst = reject
    }))
    getUserBreakdown.mockResolvedValueOnce({ users: [user] })
    const wrapper = mountBreakdownChart()

    await wrapper.findAll('tbody > tr')[0].trigger('click')
    await wrapper.findAll('tbody > tr').find((row) => row.text().includes('model-b'))!.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'UserBreakdownSubTable' }).props('items')).toEqual([user])

    if (outcome === 'resolve') resolveFirst({ users: [{ ...user, email: 'wrong-model@example.com' }] })
    else rejectFirst(new Error('Outdated request failed'))
    await flushPromises()
    expect(wrapper.findComponent({ name: 'UserBreakdownSubTable' }).props('items')).toEqual([user])
  })

  it('keeps the current model loading when an earlier request completes', async () => {
    let resolveFirst!: (value: { users: UserBreakdownItem[] }) => void
    getUserBreakdown.mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
    getUserBreakdown.mockReturnValueOnce(new Promise(() => {}))
    const wrapper = mountBreakdownChart()

    await wrapper.findAll('tbody > tr')[0].trigger('click')
    await wrapper.findAll('tbody > tr').find((row) => row.text().includes('model-b'))!.trigger('click')
    resolveFirst({ users: [user] })
    await flushPromises()

    const breakdown = wrapper.findComponent({ name: 'UserBreakdownSubTable' })
    expect(breakdown.props('items')).toEqual([])
    expect(breakdown.props('loading')).toBe(true)
  })

  it.each([
    { modelStats: [...modelStats] },
    { startDate: '2026-06-02' },
    { endDate: '2026-06-03' },
    { source: 'upstream' as const, upstreamModelStats: modelStats },
    { filters: { exclude_user_ids: '101' } },
  ])('closes stale breakdowns when the data or query context changes: %j', async (props) => {
    getUserBreakdown.mockResolvedValue({ users: [user] })
    const wrapper = mountBreakdownChart()
    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'UserBreakdownSubTable' }).exists()).toBe(true)

    await wrapper.setProps(props)
    expect(wrapper.findComponent({ name: 'UserBreakdownSubTable' }).exists()).toBe(false)
  })

  it('shows an empty breakdown after a current request fails', async () => {
    getUserBreakdown.mockRejectedValue(new Error('Request failed'))
    const wrapper = mountBreakdownChart()
    await wrapper.find('tbody tr').trigger('click')
    await flushPromises()
    const breakdown = wrapper.findComponent({ name: 'UserBreakdownSubTable' })
    expect(breakdown.props('items')).toEqual([])
    expect(breakdown.props('loading')).toBe(false)
  })
})
