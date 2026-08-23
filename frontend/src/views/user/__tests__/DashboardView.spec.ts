import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import DashboardView from '../DashboardView.vue'

const mocks = vi.hoisted(() => ({
  authStore: {
    user: {
      username: 'dashboard-user',
      email: 'dashboard-user@example.com',
      balance: 42,
    },
    isSimpleMode: false,
    refreshUser: vi.fn(),
  },
  getDashboardStats: vi.fn(),
  getDashboardActivity: vi.fn(),
  getDashboardTrend: vi.fn(),
  getDashboardModels: vi.fn(),
  getByDateRange: vi.fn()
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => mocks.authStore
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
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

const HeroStub = defineComponent({
  name: 'UserDashboardHero',
  props: { displayName: { type: String, required: true } },
  template: '<section data-testid="dashboard-hero" :data-display-name="displayName" />',
})

const StatsStub = defineComponent({
  name: 'UserDashboardStats',
  props: {
    stats: { type: Object, required: true },
    balance: { type: Number, required: true },
    isSimple: { type: Boolean, required: true },
    activity: { type: Object, default: null },
  },
  template: '<section data-testid="dashboard-stats" />',
})

const ChartsStub = defineComponent({
  name: 'UserDashboardCharts',
  props: ['startDate', 'endDate', 'granularity', 'loading', 'trend', 'models'],
  emits: [
    'update:startDate',
    'update:endDate',
    'update:granularity',
    'dateRangeChange',
    'granularityChange',
    'refresh',
  ],
  template: '<section data-testid="dashboard-charts"><button data-testid="dashboard-refresh" @click="$emit(\'refresh\')" /></section>',
})

const RecentUsageStub = defineComponent({
  name: 'UserDashboardRecentUsage',
  props: ['data', 'loading'],
  template: '<section data-testid="dashboard-recent-usage" />',
})

const ActivityStub = defineComponent({
  name: 'UserDashboardActivity',
  props: ['activity', 'loading'],
  template: '<section data-testid="dashboard-activity" />',
})

function mountDashboard() {
  return mount(DashboardView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        UserDashboardHero: HeroStub,
        UserDashboardDecorations: { template: '<div data-testid="dashboard-decorations" />' },
        UserDashboardStats: StatsStub,
        UserDashboardActivity: ActivityStub,
        UserDashboardCharts: ChartsStub,
        UserDashboardRecentUsage: RecentUsageStub,
        UserDashboardQuickActions: { template: '<section data-testid="dashboard-quick-actions" />' },
      }
    }
  })
}

describe('user DashboardView loading lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the layout-matched skeleton in the first frame', () => {
    mocks.authStore.refreshUser.mockResolvedValue(undefined)
    mocks.getDashboardStats.mockReturnValue(new Promise(() => {}))
    mocks.getDashboardActivity.mockResolvedValue({ days: [] })
    mocks.getDashboardTrend.mockResolvedValue({ trend: [] })
    mocks.getDashboardModels.mockResolvedValue({ models: [] })
    mocks.getByDateRange.mockResolvedValue({ items: [] })

    const wrapper = mountDashboard()

    expect(wrapper.get('[data-testid="dashboard-hero"]').attributes('data-display-name')).toBe('dashboard-user')
    expect(wrapper.find('[data-testid="dashboard-decorations"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="user-dashboard-skeleton"]').exists()).toBe(true)
  })

  it('renders the ordinary-user modules with API data and keeps them mounted during refresh', async () => {
    const activity = {
      peak_daily_tokens: 800_000,
      current_streak_days: 7,
      longest_streak_days: 16,
      days: [],
    }
    mocks.authStore.refreshUser.mockResolvedValue(undefined)
    mocks.getDashboardStats.mockResolvedValueOnce(stats)
    mocks.getDashboardActivity.mockResolvedValue(activity)
    mocks.getDashboardTrend.mockResolvedValue({ trend: [] })
    mocks.getDashboardModels.mockResolvedValue({ models: [] })
    mocks.getByDateRange.mockResolvedValue({ items: [] })

    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.find('[data-testid="user-dashboard"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-hero"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-stats"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-charts"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-recent-usage"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-quick-actions"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dashboard-activity"]').exists()).toBe(true)

    const renderedModules = wrapper
      .get('.user-dashboard__content')
      .findAll('[data-testid]')
      .map(module => module.attributes('data-testid'))
    expect(renderedModules.indexOf('dashboard-stats')).toBeLessThan(
      renderedModules.indexOf('dashboard-activity'),
    )
    expect(renderedModules.indexOf('dashboard-activity')).toBeLessThan(
      renderedModules.indexOf('dashboard-charts'),
    )

    const statsComponent = wrapper.getComponent(StatsStub)
    expect(statsComponent.props('stats')).toEqual(stats)
    expect(statsComponent.props('balance')).toBe(42)
    expect(statsComponent.props('isSimple')).toBe(false)
    expect(statsComponent.props('activity')).toEqual(activity)

    expect(wrapper.text()).not.toContain('用户管理')
    expect(wrapper.text()).not.toContain('分组管理')
    expect(wrapper.text()).not.toContain('安全审计')

    const refreshStats = deferred<typeof stats>()
    mocks.getDashboardStats.mockReturnValueOnce(refreshStats.promise)
    await wrapper.get('[data-testid="dashboard-refresh"]').trigger('click')

    expect(wrapper.find('[data-testid="dashboard-stats"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="user-dashboard-skeleton"]').exists()).toBe(false)

    refreshStats.resolve(stats)
    await flushPromises()
  })

  it('shows a recoverable error state when the initial stats request fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    mocks.authStore.refreshUser.mockResolvedValue(undefined)
    mocks.getDashboardStats.mockRejectedValueOnce(new Error('offline'))
    mocks.getDashboardActivity.mockResolvedValue({ days: [] })
    mocks.getDashboardTrend.mockResolvedValue({ trend: [] })
    mocks.getDashboardModels.mockResolvedValue({ models: [] })
    mocks.getByDateRange.mockResolvedValue({ items: [] })

    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.get('[data-testid="dashboard-load-error"]').text()).toContain('dashboard.loadFailed')

    mocks.getDashboardStats.mockResolvedValueOnce(stats)
    await wrapper.get('.user-dashboard__retry').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="dashboard-load-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="dashboard-stats"]').exists()).toBe(true)
    consoleError.mockRestore()
  })
})
