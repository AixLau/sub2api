import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserDashboardActivity from '../UserDashboardActivity.vue'

const labels: Record<string, string> = {
  'dashboard.activity.title': 'Token Activity',
  'dashboard.activity.lastYear': 'Past 12 months',
  'dashboard.activity.viewMode': 'Activity view',
  'dashboard.activity.daily': 'Daily',
  'dashboard.activity.weekly': 'Weekly',
  'dashboard.activity.cumulative': 'Cumulative',
  'dashboard.activity.totalTokens': 'Total Tokens',
  'dashboard.activity.peakDailyTokens': 'Peak Daily Tokens',
  'dashboard.activity.currentStreak': 'Current Streak',
  'dashboard.activity.longestStreak': 'Longest Streak',
  'dashboard.activity.tokenActivity': 'Token Activity',
  'dashboard.activity.less': 'Less',
  'dashboard.activity.more': 'More',
  'dashboard.activity.days': '{count} days',
  'dashboard.activity.tooltip': '{date} used {tokens} tokens ({mode})',
  'dashboard.activity.futureDate': '{date} has not arrived',
}

vi.mock('vue-i18n', async importOriginal => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en-US' },
      t: (key: string, values: Record<string, string | number> = {}) => Object.entries(values).reduce((text, [name, value]) => text.replace(`{${name}}`, String(value)), labels[key] ?? key),
    }),
  }
})

describe('UserDashboardActivity', () => {
  it('renders summary data, a fixed 52-week grid, and local view switches', async () => {
    const wrapper = mount(UserDashboardActivity, {
      props: {
        loading: false,
        activity: {
          window_start: '2025-08-25',
          window_end: '2026-08-23',
          total_tokens: 1_200_000,
          peak_daily_tokens: 800_000,
          current_streak_days: 2,
          longest_streak_days: 9,
          cumulative_tokens_before_window: 100,
          days: [
            { date: '2025-08-25', total_tokens: 10 },
            { date: '2025-08-26', total_tokens: 30 },
          ],
        },
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.get('[data-testid="user-dashboard-activity"]').text()).toContain('1.2M')
    expect(wrapper.findAll('[role="gridcell"]')).toHaveLength(364)
    expect(wrapper.get('[role="gridcell"]').attributes('title')).toContain('10')

    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[role="gridcell"]').attributes('title')).toContain('40')
  })
})
