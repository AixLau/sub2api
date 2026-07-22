import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import SubscriptionProgressMini from '../SubscriptionProgressMini.vue'
import { useSubscriptionStore } from '@/stores/subscriptions'
import type { Group, UserSubscription } from '@/types'

const getActiveSubscriptions = vi.hoisted(() => vi.fn())

vi.mock('@/api/subscriptions', () => ({
  default: {
    getActiveSubscriptions
  }
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
    id: 4,
    name: 'Pro 套餐',
    description: null,
    platform: 'openai',
    rate_multiplier: 1,
    is_exclusive: false,
    status: 'active',
    subscription_type: 'subscription',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: 1,
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
    id: 6,
    user_id: 34,
    group_id: 4,
    status: 'active',
    starts_at: now.toISOString(),
    expires_at: new Date(now.getTime() + 25 * 24 * 60 * 60 * 1000).toISOString(),
    daily_usage_usd: 0,
    weekly_usage_usd: 0,
    monthly_usage_usd: 243,
    monthly_bonus_usd: 2399,
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: now.toISOString(),
    created_at: now.toISOString(),
    updated_at: now.toISOString(),
    group: makeGroup(),
    ...overrides
  }
}

describe('SubscriptionProgressMini monthly bonus quota', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getActiveSubscriptions.mockReset()
    getActiveSubscriptions.mockResolvedValue([])
  })

  it('uses the effective monthly limit including bonus quota', async () => {
    const subscription = makeSubscription()
    getActiveSubscriptions.mockResolvedValue([subscription])
    const store = useSubscriptionStore()
    await store.fetchActiveSubscriptions()

    const wrapper = mount(SubscriptionProgressMini, {
      global: {
        stubs: {
          Icon: { template: '<span />' },
          RouterLink: { template: '<a><slot /></a>' }
        }
      }
    })

    expect(wrapper.text()).toContain('subscriptionProgress.title')
    await wrapper.get('[data-testid="subscription-control"]').trigger('mouseenter')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)

    expect(wrapper.text()).toContain('$243.00/$2400.00')
    expect(wrapper.text()).not.toContain('$243.00/$1.00')

    const control = wrapper.get('[data-testid="subscription-control"]')
    const trigger = control.get('button')
    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('true')

    await control.trigger('mouseleave')
    expect(trigger.attributes('aria-expanded')).toBe('true')

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')

    await control.trigger('mouseenter')
    await wrapper.get('[data-testid="subscription-close"]').trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('false')
  })
})
