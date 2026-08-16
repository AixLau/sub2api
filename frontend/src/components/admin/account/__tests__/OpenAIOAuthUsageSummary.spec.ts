import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import OpenAIOAuthUsageSummary from '../OpenAIOAuthUsageSummary.vue'
import type { OpenAIOAuthUsageSummary as Summary } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => params?.count == null ? key : `${key}:${params.count}`
    })
  }
})

const summary = (): Summary => ({
  account_count: 4,
  generated_at: '2026-08-16T12:00:00Z',
  five_hour: {
    used: 1200,
    estimated_remaining: 1022.22,
    estimated_capacity: 2222.22,
    usage_percent: 54,
    remaining_percent: 46,
    reference_capacity: 2222.22,
    reference_source: 'current',
    estimated_account_count: 4,
    unestimated_account_count: 0,
    pending_sync_account_count: 0
  },
  seven_day: {
    used: 500,
    estimated_remaining: 1500,
    estimated_capacity: 2000,
    usage_percent: 25,
    remaining_percent: 75,
    reference_capacity: 2000,
    reference_source: 'historical',
    estimated_account_count: 3,
    unestimated_account_count: 1,
    pending_sync_account_count: 2
  }
})

describe('OpenAIOAuthUsageSummary', () => {
  it('renders independent weighted windows and estimated USD values', () => {
    const wrapper = mount(OpenAIOAuthUsageSummary, {
      props: { summary: summary(), loading: false, error: null }
    })

    expect(wrapper.findAll('article')).toHaveLength(2)
    expect(wrapper.get('[data-testid="usage-window-five_hour"]').text()).toContain('54.0% / 46.0%')
    expect(wrapper.get('[data-testid="usage-window-five_hour"]').text()).toContain('~$1,022.22')
    expect(wrapper.get('[data-testid="usage-window-seven_day"]').text()).toContain('25.0% / 75.0%')

    const fiveHourProgress = wrapper.get('[data-testid="usage-window-five_hour"] [role="progressbar"]')
    expect(fiveHourProgress.attributes('aria-valuenow')).toBe('54')
    expect(fiveHourProgress.get('[data-testid="usage-progress-mask"]').attributes('style')).toContain('width: 46%')
    expect(fiveHourProgress.get('.bg-gradient-to-r').classes()).toEqual(expect.arrayContaining([
      'from-emerald-500',
      'via-amber-400',
      'to-red-500'
    ]))
  })

  it('shows historical, unestimated, and pending-sync states', () => {
    const wrapper = mount(OpenAIOAuthUsageSummary, {
      props: { summary: summary(), loading: false, error: null }
    })
    const sevenDay = wrapper.get('[data-testid="usage-window-seven_day"]').text()

    expect(sevenDay).toContain('admin.accounts.openaiUsageSummary.historicalHint')
    expect(sevenDay).toContain('admin.accounts.openaiUsageSummary.unestimatedHint:1')
    expect(sevenDay).toContain('admin.accounts.openaiUsageSummary.pendingSyncHint:2')
  })

  it('renders loading and terminal error states', async () => {
    const wrapper = mount(OpenAIOAuthUsageSummary, {
      props: { summary: null, loading: true, error: null }
    })
    expect(wrapper.findAll('.animate-pulse')).toHaveLength(2)

    await wrapper.setProps({ loading: false, error: 'network error' })
    expect(wrapper.text()).toContain('admin.accounts.openaiUsageSummary.loadFailed')
    expect(wrapper.findAll('article')).toHaveLength(0)
  })
})
