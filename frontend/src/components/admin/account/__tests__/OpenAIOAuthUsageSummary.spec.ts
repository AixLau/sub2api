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
  included_account_count: 3,
  excluded_account_count: 1,
  generated_at: '2026-08-16T12:00:00Z',
  five_hour: {
    used: 1200,
    estimated_remaining: 1022.22,
    estimated_capacity: 2222.22,
    usage_percent: 54,
    remaining_percent: 46,
    reference_capacity: 2222.22,
    reference_source: 'current',
    current_sample_account_count: 2,
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
    current_sample_account_count: 1,
    estimated_account_count: 3,
    unestimated_account_count: 1,
    pending_sync_account_count: 2
  }
})

describe('OpenAIOAuthUsageSummary', () => {
  it('renders toolbar, medium, and collapsed quota summaries without capacity', () => {
    const wrapper = mount(OpenAIOAuthUsageSummary, {
      props: { summary: summary(), loading: false, error: null },
      global: { stubs: { Teleport: true } }
    })

    expect(wrapper.get('[data-testid="openai-oauth-usage-summary"]').attributes('data-layout')).toBe('toolbar-compact')
    expect(wrapper.get('[data-testid="usage-summary-full"]').text()).not.toContain('OpenAI OAuth')
    expect(wrapper.get('[data-testid="usage-summary-full"]').text()).toContain('54.0%')
    expect(wrapper.findAll('[data-testid^="usage-window-full-"]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="usage-summary-medium"]').classes()).toContain('xl:flex')
    expect(wrapper.findAll('[data-testid^="usage-window-medium-"]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="usage-summary-collapsed"]').classes()).toContain('xl:hidden')
    expect(wrapper.findAll('[data-testid^="usage-window-collapsed-"]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="usage-summary-collapsed"]').text()).toContain('5h')
    expect(wrapper.get('[data-testid="usage-summary-collapsed"]').text()).toContain('7d')
    expect(wrapper.find('[data-testid="openai-oauth-usage-details"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="usage-summary-full"]').text())
      .not.toContain('admin.accounts.openaiUsageSummary.capacity')
    expect(wrapper.get('[data-testid="usage-summary-full"]').text()).not.toContain('~')

    const progress = wrapper.get('[data-testid="usage-window-full-five-hour"] [data-testid="usage-progress"]')
    expect(progress.attributes('style')).toContain('width: 54%')
    expect(progress.classes()).toContain('bg-primary-500')
    expect(wrapper.find('.bg-gradient-to-r').exists()).toBe(false)
  })

  it('opens complete details with counts, capacity, source, and synchronization hints', async () => {
    const wrapper = mount(OpenAIOAuthUsageSummary, {
      props: { summary: summary(), loading: false, error: null },
      global: { stubs: { Teleport: true } }
    })

    await wrapper.get('[data-testid="usage-window-full-five-hour"]').trigger('click')
    const details = wrapper.get('[data-testid="openai-oauth-usage-details"]')

    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.totalAccounts')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.includedAccounts')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.excludedAccounts')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.capacity')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.sourceHistorical')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.historicalHint')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.unestimatedHint:1')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.pendingSyncHint:2')
    expect(details.text()).toContain('2 / 3')
  })

  it('shows zero usage separately from unavailable capacity', async () => {
    const value = summary()
    value.five_hour = {
      ...value.five_hour,
      used: 0,
      estimated_remaining: null,
      estimated_capacity: null,
      usage_percent: 0,
      remaining_percent: 100,
      reference_capacity: null,
      reference_source: 'unavailable',
      current_sample_account_count: 0,
      estimated_account_count: 0,
      unestimated_account_count: 3
    }
    const wrapper = mount(OpenAIOAuthUsageSummary, {
      props: { summary: value, loading: false, error: null },
      global: { stubs: { Teleport: true } }
    })

    const fullWindow = wrapper.get('[data-testid="usage-window-full-five-hour"]')
    expect(fullWindow.text()).toContain('0.0%')
    expect(fullWindow.text()).toContain('admin.accounts.openaiUsageSummary.pendingEstimateShort')

    await wrapper.get('[data-testid="usage-window-full-five-hour"]').trigger('click')
    const details = wrapper.get('[data-testid="usage-details-five-hour"]')
    expect(details.text()).toContain('admin.accounts.openaiUsageSummary.pendingEstimate')
    expect(details.text()).toContain('100.0%')
  })

  it('renders compact loading and retryable terminal error states', async () => {
    const wrapper = mount(OpenAIOAuthUsageSummary, {
      props: { summary: null, loading: true, error: null }
    })
    const skeleton = wrapper.get('[data-testid="usage-summary-skeleton"]')
    expect(skeleton.classes()).toContain('h-11')
    expect(wrapper.get('[data-testid="usage-summary-skeleton-shimmer"]').classes()).toContain('motion-safe:animate-shimmer')

    await wrapper.setProps({ loading: false, error: 'network error' })
    expect(wrapper.text()).toContain('admin.accounts.openaiUsageSummary.loadFailedCompact')
    await wrapper.get('[data-testid="usage-summary-error"]').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })
})
