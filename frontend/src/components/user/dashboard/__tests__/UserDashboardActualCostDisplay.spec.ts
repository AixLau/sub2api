import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserDashboardCharts from '../UserDashboardCharts.vue'
import UserDashboardRecentUsage from '../UserDashboardRecentUsage.vue'
import UserDashboardStats from '../UserDashboardStats.vue'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'
import type { ModelStat, UsageLog } from '@/types'

const messages: Record<string, string> = {
  'common.active': 'active',
  'common.available': 'available',
  'common.refresh': 'Refresh',
  'common.total': 'Total',
  'dashboard.actual': 'Actual',
  'dashboard.apiKeys': 'API Keys',
  'dashboard.averageTime': 'Average time',
  'dashboard.activity.currentStreak': 'Current Streak',
  'dashboard.activity.days': '{count} days',
  'dashboard.activity.longestStreak': 'Longest Streak',
  'dashboard.activity.peakDailyTokens': 'Peak Daily Tokens',
  'dashboard.avgResponse': 'Avg Response',
  'dashboard.balance': 'Balance',
  'dashboard.day': 'Day',
  'dashboard.granularity': 'Granularity',
  'dashboard.hour': 'Hour',
  'dashboard.input': 'Input',
  'dashboard.last7Days': 'Last 7 Days',
  'dashboard.model': 'Model',
  'dashboard.modelDistribution': 'Model Distribution',
  'dashboard.noDataAvailable': 'No data available',
  'dashboard.output': 'Output',
  'dashboard.performance': 'Performance',
  'dashboard.recentUsage': 'Recent Usage',
  'dashboard.requests': 'Requests',
  'dashboard.standard': 'Standard',
  'dashboard.timeRange': 'Time Range',
  'dashboard.todayCost': 'Today Cost',
  'dashboard.todayRequests': 'Today Requests',
  'dashboard.todayTokens': 'Today Tokens',
  'dashboard.tokens': 'Tokens',
  'dashboard.totalTokens': 'Total Tokens',
  'dashboard.viewAllUsage': 'View All Usage',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

const IconStub = { template: '<span />' }
const LoadingSpinnerStub = { template: '<span />' }
const DateRangePickerStub = { template: '<span />' }
const SelectStub = { template: '<span />' }
const EmptyStateStub = { template: '<div />' }
const RouterLinkStub = { template: '<a><slot /></a>' }
const TokenUsageTrendStub = {
  props: ['showCost'],
  template: '<div class="token-usage-trend" :data-show-cost="String(showCost)" />',
}

const dashboardStats = (): UserStatsType => ({
  total_api_keys: 2,
  active_api_keys: 1,
  total_requests: 100,
  total_input_tokens: 1000,
  total_output_tokens: 2000,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 3000,
  total_cost: 7746.7298,
  total_actual_cost: 7796.5673,
  today_requests: 10,
  today_input_tokens: 100,
  today_output_tokens: 200,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 300,
  today_cost: 19.1348,
  today_actual_cost: 24.8752,
  average_duration_ms: 123,
  rpm: 1,
  tpm: 300,
  by_platform: [
    {
      platform: 'openai',
      total_requests: 8,
      total_tokens: 240,
      total_actual_cost: 20,
      today_requests: 2,
      today_tokens: 120,
      today_actual_cost: 5,
    },
  ],
})

const modelStats = (): ModelStat[] => [
  {
    model: 'gpt-5.4',
    requests: 10,
    input_tokens: 100,
    output_tokens: 200,
    cache_creation_tokens: 0,
    cache_read_tokens: 0,
    total_tokens: 300,
    cost: 19.1348,
    actual_cost: 24.8752,
    account_cost: 12,
  },
]

const recentUsage = (): UsageLog[] => [
  {
    id: 1,
    user_id: 1,
    api_key_id: 1,
    account_id: null,
    request_id: 'req-actual-only',
    model: 'gpt-5.4',
    group_id: null,
    subscription_id: null,
    input_tokens: 100,
    output_tokens: 200,
    cache_creation_tokens: 0,
    cache_read_tokens: 0,
    cache_creation_5m_tokens: 0,
    cache_creation_1h_tokens: 0,
    input_cost: 10,
    output_cost: 9.1348,
    cache_creation_cost: 0,
    cache_read_cost: 0,
    total_cost: 19.1348,
    actual_cost: 24.8752,
    rate_multiplier: 1,
    billing_type: 1,
    stream: false,
    duration_ms: 100,
    first_token_ms: null,
    image_count: 0,
    image_size: null,
    image_input_size: null,
    image_output_size: null,
    image_size_source: null,
    image_size_breakdown: null,
    image_output_tokens: 0,
    image_output_cost: 0,
    user_agent: null,
    cache_ttl_overridden: false,
    created_at: '2026-06-19T12:00:00Z',
  },
]

describe('user dashboard cost visibility', () => {
  it('formats accumulated tokens in billions', () => {
    const stats = dashboardStats()
    stats.total_tokens = 6_750_000_000

    const wrapper = mount(UserDashboardStats, {
      props: {
        stats,
        balance: 0,
        isSimple: false,
      },
      global: {
        stubs: {
          Icon: IconStub,
        },
      },
    })

    expect(wrapper.text()).toContain('6.75B')
  })

  it('shows activity metrics in place of the performance card', () => {
    const wrapper = mount(UserDashboardStats, {
      props: {
        stats: dashboardStats(),
        balance: 0,
        isSimple: false,
        activity: {
          window_start: '2025-09-01',
          window_end: '2026-08-30',
          current_date: '2026-08-18',
          total_tokens: 1_000_000,
          peak_daily_tokens: 850_000,
          current_streak_days: 3,
          longest_streak_days: 9,
          cumulative_tokens_before_window: 0,
          days: [],
        },
      },
      global: { stubs: { Icon: IconStub } },
    })

    const text = wrapper.text()
    expect(text).toContain('Peak Daily Tokens')
    expect(text).toContain('850.0K')
    expect(text).toContain('Current Streak')
    expect(text).toContain('Longest Streak')
    expect(text).not.toContain('RPM')
    expect(text).not.toContain('TPM')
  })

  it('shows user actual consumption in the summary cards without standard cost', () => {
    const wrapper = mount(UserDashboardStats, {
      props: {
        stats: dashboardStats(),
        balance: 0,
        isSimple: false,
      },
      global: {
        stubs: {
          Icon: IconStub,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Today Cost')
    expect(text).toContain('$24.8752')
    expect(text).toContain('$7796.5673')
    expect(text).not.toContain('$20.0000')
    expect(text).not.toContain('$5.0000')
    expect(text).not.toContain('dashboard.platformBreakdown')
    expect(text).not.toContain('$19.1348')
    expect(text).not.toContain('$7746.7298')
  })

  it('shows user actual consumption in charts without standard cost', () => {
    const wrapper = mount(UserDashboardCharts, {
      props: {
        loading: false,
        startDate: '2026-06-13',
        endDate: '2026-06-19',
        granularity: 'day',
        trend: [],
        models: modelStats(),
      },
      global: {
        stubs: {
          LoadingSpinner: LoadingSpinnerStub,
          DateRangePicker: DateRangePickerStub,
          Select: SelectStub,
          TokenUsageTrend: TokenUsageTrendStub,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Actual')
    expect(text).toContain('$24.8752')
    expect(text).not.toContain('$19.1348')
    expect(text).not.toContain('Standard')
    expect(wrapper.find('.token-usage-trend').attributes('data-show-cost')).toBe('true')

    const chartData = JSON.parse(wrapper.get('.chart-data').text())
    expect(chartData.datasets[0]).toMatchObject({
      borderRadius: 8,
      spacing: 2,
      hoverOffset: 4,
    })
    expect(chartData.datasets[0].backgroundColor[0]).toBe('#3B82F6')
    expect(chartData.datasets[0].backgroundColor[1]).toBe('#20D9A0')
    expect(wrapper.get('[data-testid="user-model-ring-center"]').text()).toContain('300')
  })

  it('shows user actual consumption in recent usage records without standard cost', () => {
    const wrapper = mount(UserDashboardRecentUsage, {
      props: {
        data: recentUsage(),
        loading: false,
      },
      global: {
        stubs: {
          LoadingSpinner: LoadingSpinnerStub,
          EmptyState: EmptyStateStub,
          Icon: IconStub,
          RouterLink: RouterLinkStub,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('$24.8752')
    expect(text).not.toContain('$19.1348')
  })
})
