import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const apiMocks = vi.hoisted(() => ({
  getUserBalanceHistory: vi.fn(),
  getUserUsageStats: vi.fn(),
  listByUser: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      getUserBalanceHistory: apiMocks.getUserBalanceHistory,
      getUserUsageStats: apiMocks.getUserUsageStats,
    },
  },
}))

vi.mock('@/api/admin/subscriptions', () => ({
  default: {
    listByUser: apiMocks.listByUser,
  },
}))

const messages: Record<string, string> = {
  'admin.users.balanceHistoryTitle': 'User Recharge & Concurrency History',
  'admin.users.createdAt': 'Created',
  'admin.users.currentBalance': 'Current Balance',
  'admin.users.totalRecharged': 'Total Recharged',
  'admin.users.balanceSourceTitle': 'Recharge balance',
  'admin.users.concurrencySourceTitle': 'Concurrency limit',
  'admin.users.concurrencyCurrentOfLimit': '{current} in use / {limit} max',
  'admin.users.concurrencyLimitOnly': '{limit} max',
  'admin.users.activeSubscriptionCount': '{count} active subscription(s)',
  'admin.users.subscriptionEntitlementTitle': 'Subscription benefits',
  'admin.users.subscriptionEntitlementSectionTitle': 'Remaining subscription benefits',
  'admin.users.noActiveSubscriptionEntitlements': 'No active subscription benefits',
  'admin.users.subscriptionEntitlementsLoadFailed': 'Subscription benefits could not be loaded. Please try again later.',
  'admin.users.subscriptionPeriod': 'Subscription period',
  'admin.users.subscriptionStartedAt': 'Started',
  'admin.users.subscriptionPlan': 'Subscription plan',
  'admin.users.subscriptionDailyQuota': 'Daily quota',
  'admin.users.subscriptionWeeklyQuota': 'Weekly quota',
  'admin.users.subscriptionMonthlyQuota': 'Monthly quota',
  'admin.users.subscriptionUnlimited': 'Unlimited',
  'admin.users.subscriptionUsedAmount': '{amount} used',
  'admin.users.subscriptionRemainingAmount': '{amount} remaining',
  'admin.users.subscriptionUsedOfAmount': '{used} used / {limit}',
  'common.loading': 'Loading',
  'userSubscriptions.noExpiration': 'No expiration',
  'admin.users.todayUsage': 'Today',
  'admin.users.sevenDayUsage': '7 Days',
  'admin.users.thirtyDayUsage': '30 Days',
  'admin.users.usageCost': 'Cost',
  'admin.users.usageRequests': 'Requests',
  'admin.users.usageTokens': 'Tokens',
  'admin.users.usageTokenBreakdown': 'In {input} / Out {output} / Cache {cache}',
  'admin.users.allTypes': 'All',
  'admin.users.typeBalance': 'Balance',
  'admin.users.typeAffiliateBalance': 'Affiliate Balance',
  'admin.users.typeAdminBalance': 'Admin Balance',
  'admin.users.typeScratch': 'Scratch Card',
  'admin.users.typeConcurrency': 'Concurrency',
  'admin.users.typeAdminConcurrency': 'Admin Concurrency',
  'admin.users.typeSubscription': 'Subscription',
  'admin.users.subscriptionDuration': '{days} days',
  'admin.users.subscriptionGroup': 'Group',
  'admin.users.subscriptionNotes': 'Notes',
  'admin.users.subscriptionNotesEmpty': '-',
  'admin.users.subscriptionExpiresAt': 'Expires: {date}',
  'admin.users.scratchRewardRecord': 'Granted after scratching',
  'admin.subscriptions.status.active': 'Active',
  'redeem.subscriptionAssigned': 'Subscription Assigned',
  'redeem.welcomeScratchReward': 'Welcome Gift Scratch Card',
  'redeem.surpriseScratchReward': 'Active-user Surprise Scratch Card',
  'redeem.campaignReward': 'Campaign Reward Credited',
  'admin.users.noBalanceHistory': 'No history',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        const template = messages[key] ?? key
        if (!params) return template
        return template.replace(/\{(\w+)\}/g, (_, name) => String(params[name] ?? ''))
      },
    }),
  }
})

vi.mock('@/components/common/BaseDialog.vue', () => ({
  default: {
    name: 'BaseDialog',
    props: ['show', 'title', 'width'],
    template: '<section v-if="show"><h2>{{ title }}</h2><slot /></section>',
  },
}))

vi.mock('@/components/common/Select.vue', () => ({
  default: {
    name: 'Select',
    props: ['modelValue', 'options'],
    emits: ['update:modelValue', 'change'],
    template: '<select :value="modelValue" @change="$emit(\'change\')"><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>',
  },
}))

vi.mock('@/components/icons/Icon.vue', () => ({
  default: {
    name: 'Icon',
    props: ['name'],
    template: '<span>{{ name }}</span>',
  },
}))

import UserBalanceHistoryModal from '../UserBalanceHistoryModal.vue'

const user = {
  id: 99,
  email: 'person@example.com',
  username: '',
  balance: 12.34,
  concurrency: 5,
  notes: '',
  created_at: '2026-06-21T15:19:17Z',
}

describe('UserBalanceHistoryModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.getUserBalanceHistory.mockResolvedValue({
      items: [],
      total: 0,
      total_recharged: 25,
    })
    apiMocks.getUserUsageStats.mockImplementation((_id: number, period: string) => Promise.resolve({
      period,
      total_requests: period === 'today' ? 2 : period === '7d' ? 7 : 30,
      total_input_tokens: 100,
      total_output_tokens: 50,
      total_cache_tokens: 10,
      total_cache_creation_tokens: 4,
      total_cache_read_tokens: 6,
      total_tokens: 13_353_354,
      total_cost: 1.23,
      total_actual_cost: period === 'today' ? 0.5 : period === '7d' ? 2.75 : 9.25,
      average_duration_ms: 250,
    }))
    apiMocks.listByUser.mockResolvedValue([])
  })

  it('loads and renders today, 7-day, and 30-day usage summaries when opened', async () => {
    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
        show: false,
        user: user as any,
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(apiMocks.getUserUsageStats).toHaveBeenCalledWith(99, 'today')
    expect(apiMocks.getUserUsageStats).toHaveBeenCalledWith(99, '7d')
    expect(apiMocks.getUserUsageStats).toHaveBeenCalledWith(99, '30d')

    const text = wrapper.text()
    expect(text).toContain('Today')
    expect(text).toContain('5 max')
    expect(text).toContain('7 Days')
    expect(text).toContain('30 Days')
    expect(text).toContain('$0.50')
    expect(text).toContain('$2.75')
    expect(text).toContain('$9.25')
    expect(text).toContain('13.4M')
    expect(text).not.toContain('13,353,354')
    expect(text).not.toContain('In 100 / Out 50 / Cache 10')
  })

  it('renders subscriptions from the unified history with status and expiration', async () => {
    apiMocks.getUserBalanceHistory.mockResolvedValue({
      items: [{
        id: 7,
        code: 'SUB-7',
        type: 'subscription',
        value: 30,
        status: 'active',
        used_by: 99,
        used_at: '2026-07-01T08:00:00Z',
        created_at: '2026-07-01T08:00:00Z',
        expires_at: '2026-07-31T08:00:00Z',
        starts_at: '2026-07-01T08:00:00Z',
        group_id: 3,
        validity_days: 30,
        notes: 'Paid plan',
        group: { id: 3, name: 'Pro' },
      }],
      total: 1,
      total_recharged: 25,
    })

    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
        show: false,
        user: user as any,
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Subscription Assigned')
    expect(text).toContain('30 days')
    expect(text).toContain('Subscription plan: Pro')
    expect(text).toContain('Notes: Paid plan')
    expect(text).toContain('Active')
    expect(text).toContain('Expires:')
    expect(text).toContain('Started')
    expect(text).not.toContain('SUB-7')
  })

  it('shows remaining subscription benefits separately from the recharge balance', async () => {
    apiMocks.listByUser.mockResolvedValue([{
        id: 15,
        user_id: 99,
        group_id: 3,
        status: 'active',
        starts_at: '2026-07-01T08:00:00Z',
        expires_at: '2099-07-31T08:00:00Z',
        daily_usage_usd: 2,
        weekly_usage_usd: 8,
        monthly_usage_usd: 25,
        monthly_bonus_usd: 5,
        pending_renewal_count: 0,
        pending_renewals: [],
        daily_window_start: null,
        weekly_window_start: null,
        monthly_window_start: null,
        created_at: '2026-07-01T08:00:00Z',
        updated_at: '2026-07-01T08:00:00Z',
        group: {
          id: 3,
          name: 'Pro',
          daily_limit_usd: 10,
          weekly_limit_usd: 20,
          monthly_limit_usd: 100,
        },
      }])

    const wrapper = mount(UserBalanceHistoryModal, {
      props: { show: false, user: user as any },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    const text = wrapper.text()
    expect(apiMocks.listByUser).toHaveBeenCalledWith(99)
    expect(text).toContain('Recharge balance')
    expect(text).toContain('Subscription benefits')
    expect(text).toContain('8.00 remaining')
    expect(text).toContain('12.00 remaining')
    expect(text).toContain('80.00 remaining')
    expect(text).toContain('Subscription period')
    expect(text).not.toContain('Subscription quota and recharge balance')
  })

  it('renders the scratch-card source type and credited amount', async () => {
    apiMocks.getUserBalanceHistory.mockResolvedValue({
      items: [
        {
          id: 10,
          code: 'WELCOME-REWARD-RECORD',
          type: 'welcome_scratch',
          value: 3,
          status: 'used',
          used_by: 99,
          used_at: '2026-07-28T08:00:00Z',
          created_at: '2026-07-28T08:00:00Z',
          group_id: null,
          validity_days: 30,
          notes: '',
        },
        {
          id: 11,
          code: 'SURPRISE-REWARD-RECORD',
          type: 'surprise_scratch',
          value: 5,
          status: 'used',
          used_by: 99,
          used_at: '2026-07-29T08:00:00Z',
          created_at: '2026-07-29T08:00:00Z',
          group_id: null,
          validity_days: 30,
          notes: '',
        },
      ],
      total: 2,
      total_recharged: 25,
    })

    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
        show: false,
        user: user as any,
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Welcome Gift Scratch Card')
    expect(text).toContain('Active-user Surprise Scratch Card')
    expect(text).toContain('+$3.00')
    expect(text).toContain('+$5.00')
    expect(text).toContain('Granted after scratching')
    expect(text).not.toContain('WELCOME')
    expect(text).not.toContain('SURPRISE-')
  })

  it('offers one scratch-card filter for every scratch reward source', async () => {
    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
        show: false,
        user: user as any,
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    const options = wrapper.getComponent({ name: 'Select' }).props('options') as Array<{
      value: string
      label: string
    }>
    const scratchOptions = options.filter(option => option.value.includes('scratch'))

    expect(scratchOptions).toEqual([{ value: 'scratch', label: 'Scratch Card' }])
  })

  it('renders campaign rewards as scratch cards without internal tracking notes', async () => {
    apiMocks.getUserBalanceHistory.mockResolvedValue({
      items: [{
        id: 12,
        code: 'CAMPAIGN-REWARD-RECORD',
        type: 'campaign_reward',
        value: 8,
        status: 'used',
        used_by: 99,
        used_at: '2026-08-01T05:27:12Z',
        created_at: '2026-08-01T05:27:12Z',
        group_id: null,
        validity_days: 0,
        notes: 'reward campaign grant #11',
      }],
      total: 1,
      total_recharged: 25,
    })

    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
        show: false,
        user: user as any,
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Campaign Reward Credited')
    expect(text).toContain('+$8.00')
    expect(text).toContain('Granted after scratching')
    expect(text).not.toContain('reward campaign grant #11')
    expect(text).not.toContain('common.unknown')
  })

  it('keeps subscription plan and notes visible when optional details are missing', async () => {
    apiMocks.getUserBalanceHistory.mockResolvedValue({
      items: [{
        id: 8,
        code: 'SUB-8',
        type: 'subscription',
        value: 14,
        status: 'active',
        used_by: 99,
        used_at: '2026-07-02T08:00:00Z',
        created_at: '2026-07-02T08:00:00Z',
        expires_at: '2026-07-16T08:00:00Z',
        group_id: 4,
        validity_days: 14,
        notes: '   ',
        group: null,
      }],
      total: 1,
      total_recharged: 25,
    })

    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
        show: false,
        user: user as any,
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Subscription plan: #4')
    expect(text).toContain('Notes: -')
    expect(text).toContain('14 days')
  })
})
