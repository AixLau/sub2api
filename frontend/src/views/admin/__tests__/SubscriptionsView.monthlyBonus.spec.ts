import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'

import SubscriptionsView from '../SubscriptionsView.vue'
import type { Group, User, UserSubscription } from '@/types'

const {
  listSubscriptions,
  extendSubscription,
  addMonthlyBonus,
  getAllGroups,
  searchUsers,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  extendSubscription: vi.fn(),
  addMonthlyBonus: vi.fn(),
  getAllGroups: vi.fn(),
  searchUsers: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: {
      list: listSubscriptions,
      assign: vi.fn(),
      extend: extendSubscription,
      revoke: vi.fn(),
      resetQuota: vi.fn(),
      addMonthlyBonus
    },
    groups: {
      getAll: getAllGroups
    },
    usage: {
      searchUsers
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo: vi.fn()
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

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: { type: [String, Number, null], default: '' },
    options: { type: Array, default: () => [] }
  },
  emits: ['update:modelValue', 'change'],
  template: '<select :value="modelValue"><slot /></select>'
})

const DataTableStub = defineComponent({
  name: 'DataTable',
  props: {
    columns: { type: Array, required: true },
    data: { type: Array, required: true }
  },
  template: `
    <div data-test="data-table">
      <div v-for="row in data" :key="row.id" data-test="subscription-row">
        <slot name="cell-usage" :row="row" />
        <slot name="cell-expires_at" :value="row.expires_at" :row="row" />
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `
})

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: { type: Boolean, default: false },
    title: { type: String, default: '' }
  },
  emits: ['close'],
  template: `
    <div v-if="show" data-test="dialog">
      <h2>{{ title }}</h2>
      <slot />
      <slot name="footer" />
    </div>
  `
})

function makeUser(overrides: Partial<User> = {}): User {
  const now = new Date().toISOString()
  return {
    id: 20,
    username: 'user',
    email: 'user@example.com',
    role: 'user',
    balance: 0,
    concurrency: 1,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: false,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: now,
    updated_at: now,
    ...overrides
  }
}

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
    user: makeUser(),
    group: makeGroup(),
    ...overrides
  }
}

function mountView() {
  return mount(SubscriptionsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        GroupBadge: { template: '<span>{{ name }}</span>', props: ['name'] },
        GroupOptionItem: true,
        Icon: { template: '<span data-test="icon"></span>' },
        teleport: true,
        transition: false,
        'router-link': { template: '<a><slot /></a>' }
      }
    }
  })
}

describe('admin SubscriptionsView monthly bonus quota', () => {
  beforeEach(() => {
    localStorage.clear()
    listSubscriptions.mockReset()
    extendSubscription.mockReset()
    addMonthlyBonus.mockReset()
    getAllGroups.mockReset()
    searchUsers.mockReset()
    showError.mockReset()
    showSuccess.mockReset()

    listSubscriptions.mockResolvedValue({
      items: [makeSubscription()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    extendSubscription.mockResolvedValue(makeSubscription())
    addMonthlyBonus.mockResolvedValue(makeSubscription({ monthly_bonus_usd: 200 }))
    getAllGroups.mockResolvedValue([])
    searchUsers.mockResolvedValue([])
  })

  it('shows current-month bonus in the effective monthly limit', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('$50.00')
    expect(wrapper.text()).toContain('$200.00')
    expect(wrapper.text()).toContain('admin.subscriptions.monthlyBonusApplied')
  })

  it('shows queued renewal details alongside the current expiry', async () => {
    listSubscriptions.mockResolvedValue({
      items: [
        makeSubscription({
          pending_renewal_count: 1,
          pending_renewals: [{
            id: 1,
            position: 1,
            target_group_id: 30,
            target_group_name: 'Pro',
            plan_id: 1,
            plan_name: 'Pro Monthly',
            validity_days: 30,
            monthly_limit_usd: 540,
            purchased_at: '2026-07-05T13:44:41Z'
          }]
        })
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.subscriptions.pendingRenewals:{"count":1,"days":30}')
    expect(wrapper.text()).toContain('#1 Pro Monthly · $540.00 · 30')
    expect(wrapper.text()).toContain('admin.subscriptions.pendingRenewalRule')
  })

  it('can add bonus quota without extending the subscription time', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('[data-test="subscription-row"] button').trigger('click')
    await nextTick()

    const inputs = wrapper.findAll('[data-test="dialog"] input')
    expect(inputs).toHaveLength(2)
    expect((inputs[0].element as HTMLInputElement).value).toBe('0')
    await inputs[1].setValue('100')
    await wrapper.find('#extend-subscription-form').trigger('submit')
    await flushPromises()

    expect(extendSubscription).not.toHaveBeenCalled()
    expect(addMonthlyBonus).toHaveBeenCalledWith(10, 100)
    expect(showSuccess).toHaveBeenCalledWith('admin.subscriptions.subscriptionAdjusted')
  })

  it('requires either day adjustment or monthly bonus amount', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('[data-test="subscription-row"] button').trigger('click')
    await nextTick()
    await wrapper.find('#extend-subscription-form').trigger('submit')
    await flushPromises()

    expect(extendSubscription).not.toHaveBeenCalled()
    expect(addMonthlyBonus).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.subscriptions.adjustNoop')
  })
})
