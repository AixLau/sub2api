import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import OpenAIOAuthUsageDetailsDialog from '../OpenAIOAuthUsageDetailsDialog.vue'
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

const makeSummary = (): Summary => ({
  account_count: 9,
  included_account_count: 8,
  excluded_account_count: 1,
  generated_at: '2026-08-19T15:46:21Z',
  five_hour: {
    used: 1200,
    estimated_remaining: 1022.22,
    estimated_capacity: 2222.22,
    usage_percent: 54,
    remaining_percent: 46,
    reference_capacity: 277.78,
    reference_source: 'current',
    current_sample_account_count: 8,
    estimated_account_count: 8,
    unestimated_account_count: 0,
    pending_sync_account_count: 0
  },
  seven_day: {
    used: 2500,
    estimated_remaining: 7500,
    estimated_capacity: 10000,
    usage_percent: 25,
    remaining_percent: 75,
    reference_capacity: 1111.11,
    reference_source: 'historical',
    current_sample_account_count: 6,
    estimated_account_count: 7,
    unestimated_account_count: 1,
    pending_sync_account_count: 1
  }
})

const mountDrawer = (props: Partial<{ show: boolean; summary: Summary | null; error: string | null }> = {}) => mount(OpenAIOAuthUsageDetailsDialog, {
  props: {
    show: true,
    summary: makeSummary(),
    error: null,
    ...props
  },
  global: {
    stubs: {
      Teleport: true,
      Icon: true
    }
  }
})

describe('OpenAIOAuthUsageDetailsDialog', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    document.body.classList.remove('modal-open')
  })

  it('renders a dense drawer with account totals and both quota windows', () => {
    const wrapper = mountDrawer()

    expect(wrapper.get('[data-testid="openai-oauth-usage-drawer"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="openai-usage-drawer-panel"]').classes()).toContain('openai-usage-drawer')
    expect(wrapper.get('[data-testid="openai-usage-drawer-panel"]').get('h2').text()).toContain('detailsTitle')
    expect(wrapper.get('[data-testid="openai-usage-account-stats"]').findAll('dd').map((node) => node.text())).toEqual(['9', '8', '1'])
    expect(wrapper.findAll('[data-testid^="usage-details-"]').filter((node) => node.attributes('data-testid')?.includes('hour') || node.attributes('data-testid')?.includes('day'))).toHaveLength(2)
    expect(wrapper.get('[data-testid="usage-details-five-hour"]').text()).toContain('54.0%')
    expect(wrapper.get('[data-testid="usage-details-seven-day"]').text()).toContain('25.0%')
    expect(wrapper.findAll('[data-testid="usage-details-progress"]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="view-openai-oauth-accounts"]').text()).toContain('viewAccounts:9')
  })

  it('maps current, historical, mixed, and unavailable data sources and preserves null estimates', async () => {
    const value = makeSummary()
    value.five_hour = {
      ...value.five_hour,
      estimated_remaining: null,
      estimated_capacity: null,
      reference_capacity: null,
      reference_source: 'unavailable'
    }
    value.seven_day = {
      ...value.seven_day,
      reference_source: 'mixed'
    }
    const wrapper = mountDrawer({ summary: value })

    expect(wrapper.get('[data-testid="usage-details-five-hour"]').text()).toContain('sourceUnavailable')
    expect(wrapper.get('[data-testid="usage-details-five-hour"]').text()).toContain('pendingEstimate')
    expect(wrapper.get('[data-testid="usage-details-seven-day"]').text()).toContain('sourceMixed')

    await wrapper.setProps({
      summary: {
        ...value,
        five_hour: { ...value.five_hour, reference_source: 'current' },
        seven_day: { ...value.seven_day, reference_source: 'historical' }
      }
    })
    expect(wrapper.get('[data-testid="usage-details-five-hour"]').text()).toContain('sourceCurrent')
    expect(wrapper.get('[data-testid="usage-details-seven-day"]').text()).toContain('sourceHistorical')
  })

  it('keeps stale data visible with retryable error, and renders a loading skeleton without data', async () => {
    const wrapper = mountDrawer({ error: 'network error' })
    expect(wrapper.get('[data-testid="openai-usage-inline-error"]').exists()).toBe(true)
    await wrapper.get('[data-testid="openai-usage-inline-error"] button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)

    const loading = mountDrawer({ summary: null, error: null })
    expect(loading.get('[data-testid="openai-usage-details-skeleton"]').exists()).toBe(true)
  })

  it('closes on close button, backdrop, and Escape, and emits view-accounts before close', async () => {
    const wrapper = mountDrawer()
    await wrapper.get('[data-testid="openai-usage-drawer-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)

    await wrapper.get('[data-testid="openai-usage-drawer-backdrop"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(2)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(3)

    await wrapper.get('[data-testid="view-openai-oauth-accounts"]').trigger('click')
    expect(wrapper.emitted('view-accounts')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(4)
  })

  it('keeps the same structure when dark mode is active', () => {
    document.documentElement.classList.add('dark')
    const wrapper = mountDrawer()
    expect(wrapper.get('[data-testid="openai-usage-drawer-panel"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="openai-usage-account-stats"]').findAll('dd')).toHaveLength(3)
    expect(wrapper.findAll('[data-testid="usage-details-progress"]')).toHaveLength(2)
  })
})
