import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'

const { getSnapshotV2, getUserUsageTrend, getActiveUsersTrend, getUserGrowthRetention, getUserSpendingRanking, listAdmins } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getActiveUsersTrend: vi.fn(),
  getUserGrowthRetention: vi.fn(),
  getUserSpendingRanking: vi.fn(),
  listAdmins: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { listAdmins },
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getActiveUsersTrend,
      getUserGrowthRetention,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0,
  recent_1m_active_users: 3
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getActiveUsersTrend.mockReset()
    getUserGrowthRetention.mockReset()
    getUserSpendingRanking.mockReset()
    listAdmins.mockReset().mockResolvedValue([{ id: 101 }, { id: 202 }])

    getSnapshotV2.mockImplementation(async (params) => ({
      stats: createDashboardStats(),
      trend: [],
      models: [],
      start_time: params.start_time ?? `${params.start_date}T00:00:00.000Z`,
      end_time: params.end_time ?? new Date(new Date(`${params.end_date}T00:00:00.000Z`).getTime() + 24 * 60 * 60 * 1000).toISOString()
    }))
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getActiveUsersTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserGrowthRetention.mockResolvedValue({
      cohorts: [],
      summary: {
        d1_rate: null,
        d7_rate: null,
        d30_rate: null,
        paid_rate: null,
        repeat_buy_rate: null
      },
      start_date: '',
      end_date: ''
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it('renders the admin dashboard skeleton in the first frame while data is pending', () => {
    getSnapshotV2.mockReturnValue(new Promise(() => {}))

    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          ActiveUsersTrend: true,
          UserGrowthRetention: true
        }
      }
    })

    expect(wrapper.find('[data-testid="admin-dashboard-skeleton"]').exists()).toBe(true)
  })

  it('uses last 24 hours as default dashboard range', async () => {
    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))

    const range = getSnapshotV2.mock.calls[0][0] as { start_time?: string; end_time?: string }
    expect(range.start_time).toEqual(expect.any(String))
    expect(range.end_time).toEqual(expect.any(String))
    expect(new Date(range.end_time!).getTime() - new Date(range.start_time!).getTime()).toBe(24 * 60 * 60 * 1000)

    const chart = wrapper.findComponent(ModelDistributionChart)
    expect(chart.props('filters')).toEqual({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      start_time: range.start_time,
      end_time: range.end_time,
      exclude_user_ids: '101,202'
    })
    expect(chart.props('startDate')).toBe(formatLocalDate(yesterday))
    expect(chart.props('endDate')).toBe(formatLocalDate(now))
  })

  it('refreshes summary cards and charts together and uses the returned snapshot bounds', async () => {
    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          ActiveUsersTrend: true,
          UserGrowthRetention: true
        }
      }
    })

    await flushPromises()
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2.mock.calls[0][0]).toEqual(expect.objectContaining({
      include_stats: true
    }))

    // A cached snapshot can have bounds different from the refresh request.
    getSnapshotV2.mockResolvedValueOnce({
      stats: createDashboardStats(), trend: [], models: [],
      start_time: '2026-06-01T11:00:05.123Z',
      end_time: '2026-06-02T11:00:05.123Z'
    })
    await wrapper.get('[data-testid="admin-dashboard-chart-refresh"]').trigger('click')
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(2)
    expect(getSnapshotV2.mock.calls[1][0]).toEqual(expect.objectContaining({
      include_stats: true
    }))
    expect(wrapper.findComponent(ModelDistributionChart).props('filters')).toEqual(expect.objectContaining({
      start_time: '2026-06-01T11:00:05.123Z',
      end_time: '2026-06-02T11:00:05.123Z'
    }))
  })

  it('retains the displayed query on failure and uses server bounds for a successful date range change', async () => {
    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          ActiveUsersTrend: true,
          UserGrowthRetention: true
        }
      }
    })
    await flushPromises()
    const chart = wrapper.findComponent(ModelDistributionChart)
    const originalFilters = chart.props('filters')
    const picker = wrapper.findComponent({ name: 'DateRangePicker' })
    let rejectSnapshot!: (reason: Error) => void
    getSnapshotV2.mockReturnValueOnce(new Promise((_, reject) => { rejectSnapshot = reject }))

    picker.vm.$emit('update:startDate', '2026-06-01')
    picker.vm.$emit('update:endDate', '2026-06-03')
    picker.vm.$emit('change', { startDate: '2026-06-01', endDate: '2026-06-03', preset: null })
    await flushPromises()
    expect(chart.props('filters')).toEqual(originalFilters)

    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    rejectSnapshot(new Error('Snapshot failed'))
    await flushPromises()
    consoleError.mockRestore()
    expect(chart.props('filters')).toEqual(originalFilters)
    expect(chart.props('startDate')).toBe(originalFilters.start_date)

    await wrapper.get('[data-testid="admin-dashboard-chart-refresh"]').trigger('click')
    await flushPromises()
    expect(getSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({
      start_date: '2026-06-01', end_date: '2026-06-03', exclude_user_ids: '101,202'
    }))
    expect(chart.props('filters')).toEqual({
      start_date: '2026-06-01', end_date: '2026-06-03', exclude_user_ids: '101,202',
      start_time: '2026-06-01T00:00:00.000Z', end_time: '2026-06-04T00:00:00.000Z'
    })
    expect(chart.props('startDate')).toBe('2026-06-01')
    expect(chart.props('endDate')).toBe('2026-06-03')
  })

  it('does not load recent usage trend and enlarges token usage trend', async () => {
    const TokenUsageTrendStub = {
      props: ['trendData', 'loading', 'showCost', 'chartHeightClass'],
      template: '<section data-testid="token-usage-trend" :data-height="chartHeightClass" />'
    }

    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: TokenUsageTrendStub,
          ActiveUsersTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    expect(getUserUsageTrend).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('admin.dashboard.recentUsage')
    expect(wrapper.find('[data-testid="token-usage-trend"]').attributes('data-height')).toBe('h-80')
    const recentActiveUsers = wrapper.find('[data-testid="recent-1m-active-users"]')
    expect(recentActiveUsers.text()).toContain('3')
    expect(recentActiveUsers.text()).toContain('admin.dashboard.recent1mActiveUsersUnit')
    expect(recentActiveUsers.find('.text-xl.font-bold').text()).toBe('3')
    expect(recentActiveUsers.element.parentElement?.textContent).toContain('RPM')
  })
})
