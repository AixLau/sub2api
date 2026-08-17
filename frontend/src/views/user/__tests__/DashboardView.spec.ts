import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DashboardView from '../DashboardView.vue'

const mocks = vi.hoisted(() => ({
  refreshUser: vi.fn(),
  getDashboardStats: vi.fn(),
  getDashboardActivity: vi.fn(),
  getDashboardTrend: vi.fn(),
  getDashboardModels: vi.fn(),
  getByDateRange: vi.fn()
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { balance: 42 },
    isSimpleMode: false,
    refreshUser: mocks.refreshUser
  })
}))

vi.mock('@/api/usage', () => ({
  usageAPI: {
    getDashboardStats: mocks.getDashboardStats,
    getDashboardActivity: mocks.getDashboardActivity,
    getDashboardTrend: mocks.getDashboardTrend,
    getDashboardModels: mocks.getDashboardModels,
    getByDateRange: mocks.getByDateRange
  }
}))

const stats = {
  total_api_keys: 1,
  active_api_keys: 1,
  today_requests: 2,
  total_requests: 3,
  today_actual_cost: 4,
  total_actual_cost: 5,
  today_tokens: 6,
  total_tokens: 7,
  today_input_tokens: 8,
  today_output_tokens: 9,
  total_input_tokens: 10,
  total_output_tokens: 11,
  rpm: 12,
  tpm: 13,
  average_duration_ms: 14
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function mountDashboard() {
  return mount(DashboardView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        UserDashboardStats: { template: '<div data-testid="dashboard-stats" />' },
        UserDashboardActivity: { template: '<div data-testid="dashboard-activity" />' },
        UserDashboardCharts: {
          emits: ['refresh'],
          template: '<button data-testid="dashboard-refresh" @click="$emit(\'refresh\')" />'
        },
        UserDashboardRecentUsage: true,
        UserDashboardQuickActions: true
      }
    }
  })
}

describe('user DashboardView loading lifecycle', () => {
  it('renders the layout-matched skeleton in the first frame', () => {
    mocks.refreshUser.mockResolvedValue(undefined)
    mocks.getDashboardStats.mockReturnValue(new Promise(() => {}))
    mocks.getDashboardActivity.mockResolvedValue({ days: [] })
    mocks.getDashboardTrend.mockResolvedValue({ trend: [] })
    mocks.getDashboardModels.mockResolvedValue({ models: [] })
    mocks.getByDateRange.mockResolvedValue({ items: [] })

    const wrapper = mountDashboard()

    expect(wrapper.find('[data-testid="user-dashboard-skeleton"]').exists()).toBe(true)
  })

  it('keeps rendered dashboard content mounted during refresh', async () => {
    mocks.refreshUser.mockResolvedValue(undefined)
    mocks.getDashboardStats.mockResolvedValueOnce(stats)
    mocks.getDashboardActivity.mockResolvedValue({ days: [] })
    mocks.getDashboardTrend.mockResolvedValue({ trend: [] })
    mocks.getDashboardModels.mockResolvedValue({ models: [] })
    mocks.getByDateRange.mockResolvedValue({ items: [] })

    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-stats"]').exists()).toBe(true)

    const refreshStats = deferred<typeof stats>()
    mocks.getDashboardStats.mockReturnValueOnce(refreshStats.promise)
    await wrapper.get('[data-testid="dashboard-refresh"]').trigger('click')

    expect(wrapper.find('[data-testid="dashboard-stats"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="user-dashboard-skeleton"]').exists()).toBe(false)

    refreshStats.resolve(stats)
    await flushPromises()
  })
})
