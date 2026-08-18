import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserDashboardActivity from '../UserDashboardActivity.vue'

const labels: Record<string, string> = {
  'dashboard.activity.title': 'Token Activity',
  'dashboard.activity.viewMode': 'Activity view',
  'dashboard.activity.daily': 'Daily',
  'dashboard.activity.weekly': 'Weekly',
  'dashboard.activity.cumulative': 'Cumulative',
  'dashboard.activity.peakDailyTokens': 'Peak Daily Tokens',
  'dashboard.activity.currentStreak': 'Current Streak',
  'dashboard.activity.longestStreak': 'Longest Streak',
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
  it('renders a fixed 52-week grid and local view switches', async () => {
    const wrapper = mount(UserDashboardActivity, {
      props: {
        loading: false,
        activity: {
          window_start: '2025-09-01',
          window_end: '2026-08-30',
          current_date: '2026-08-18',
          total_tokens: 1_200_000,
          peak_daily_tokens: 800_000,
          current_streak_days: 2,
          longest_streak_days: 9,
          cumulative_tokens_before_window: 100,
          days: [
            { date: '2025-09-01', total_tokens: 10 },
            { date: '2025-09-02', total_tokens: 30 },
          ],
        },
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.findAll('[role="gridcell"]')).toHaveLength(364)
    expect(wrapper.find('[data-testid="activity-tooltip"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="activity-month-label"]').map(label => label.text())).toEqual([
      'Sep', 'Oct', 'Nov', 'Dec', 'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug',
    ])
    const gridCells = wrapper.findAll('[role="gridcell"]')
    expect(gridCells[gridCells.length - 1].attributes('disabled')).toBeDefined()

    await gridCells[0].trigger('mouseenter', { clientX: 120, clientY: 80 })
    expect(wrapper.get('[data-testid="activity-tooltip"]').text()).toContain('10')
    await gridCells[0].trigger('mouseleave')
    expect(wrapper.find('[data-testid="activity-tooltip"]').exists()).toBe(false)
    await gridCells[0].trigger('mouseenter', { clientX: 120, clientY: 80 })
    await wrapper.get('.activity-graph').trigger('mouseleave')
    expect(wrapper.find('[data-testid="activity-tooltip"]').exists()).toBe(false)

    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="activity-tooltip"]').exists()).toBe(false)
    await gridCells[0].trigger('mouseenter', { clientX: 120, clientY: 80 })
    expect(wrapper.get('[data-testid="activity-tooltip"]').text()).toContain('40')
  })
})
