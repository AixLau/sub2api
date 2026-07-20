import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import SubscriptionsView from '../SubscriptionsView.vue'
import type { Group, UserSubscription } from '@/types'

const getMySubscriptions = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())

vi.mock('@/api/subscriptions', () => ({
  default: {
    getMySubscriptions
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: routerPush
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

function makeGroup(overrides: Partial<Group> = {}): Group {
  const now = new Date().toISOString()
  return {
    id: 30,
    name: 'Pro',
    description: null,
    platform: 'anthropic',
    rate_multiplier: 1,
    is_exclusive: false,
    status: 'active',
    subscription_type: 'subscription',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: 100,
    allow_image_generation: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    claude_code_only: false,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    require_oauth_only: false,
    require_privacy_set: false,
    created_at: now,
    updated_at: now,
    ...overrides
  }
}

function makeSubscription(overrides: Partial<UserSubscription> = {}): UserSubscription {
  const now = new Date()
  return {
    id: 10,
    user_id: 20,
    group_id: 30,
    status: 'active',
    starts_at: now.toISOString(),
    expires_at: new Date(now.getTime() + 10 * 24 * 60 * 60 * 1000).toISOString(),
    daily_usage_usd: 0,
    weekly_usage_usd: 0,
    monthly_usage_usd: 50,
    monthly_bonus_usd: 100,
    pending_renewal_count: 0,
    pending_renewals: [],
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: now.toISOString(),
    created_at: now.toISOString(),
    updated_at: now.toISOString(),
    group: makeGroup(),
    ...overrides
  }
}

describe('user SubscriptionsView monthly bonus quota', () => {
  beforeEach(() => {
    getMySubscriptions.mockReset()
    showError.mockReset()
    routerPush.mockReset()
    getMySubscriptions.mockResolvedValue([makeSubscription()])
  })

  it('includes current-month bonus in the displayed monthly limit', async () => {
    const wrapper = mount(SubscriptionsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: { template: '<span />' }
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('$50.00 / $200.00')
  })

  it('shows queued renewals in payment order', async () => {
    getMySubscriptions.mockResolvedValue([
      makeSubscription({
        pending_renewal_count: 2,
        pending_renewals: [
          {
            id: 1,
            position: 1,
            target_group_id: 30,
            target_group_name: 'Pro',
            plan_id: 1,
            plan_name: 'Pro Monthly',
            validity_days: 30,
            monthly_limit_usd: 540,
            purchased_at: '2026-07-05T13:44:41Z'
          },
          {
            id: 2,
            position: 2,
            target_group_id: 30,
            target_group_name: 'Pro',
            plan_id: 1,
            plan_name: 'Pro Monthly',
            validity_days: 30,
            monthly_limit_usd: 540,
            purchased_at: '2026-07-12T10:41:49Z'
          }
        ]
      })
    ])

    const wrapper = mount(SubscriptionsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: { template: '<span />' }
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('userSubscriptions.pendingRenewals:{"count":2}')
    expect(wrapper.text()).toContain('userSubscriptions.pendingRenewalTotalDays:{"days":60}')
    expect(wrapper.text()).toContain('#1 Pro Monthly')
    expect(wrapper.text()).toContain('#2 Pro Monthly')
  })
})
