import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  getOpenAIOAuthUsageSummary,
  getBatchTodayStats,
  getUpstreamBillingProbeSettings,
  getAllGroups,
  getAllProxies,
  listAccounts,
  listWithEtag,
  setSchedulable
} = vi.hoisted(() => ({
  getOpenAIOAuthUsageSummary: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllGroups: vi.fn(),
  getAllProxies: vi.fn(),
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  setSchedulable: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      getOpenAIOAuthUsageSummary,
      getUpstreamBillingProbeSettings,
      setSchedulable
    },
    proxies: {
      getAll: getAllProxies,
      getAllWithCount: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token' })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const summary = {
  account_count: 2,
  included_account_count: 1,
  excluded_account_count: 1,
  generated_at: '2026-08-16T12:00:00Z',
  five_hour: {
    used: 10,
    estimated_remaining: 90,
    estimated_capacity: 100,
    usage_percent: 10,
    remaining_percent: 90,
    reference_capacity: 100,
    reference_source: 'current',
    current_sample_account_count: 1,
    estimated_account_count: 1,
    unestimated_account_count: 0,
    pending_sync_account_count: 0
  },
  seven_day: {
    used: 20,
    estimated_remaining: 80,
    estimated_capacity: 100,
    usage_percent: 20,
    remaining_percent: 80,
    reference_capacity: 100,
    reference_source: 'current',
    current_sample_account_count: 1,
    estimated_account_count: 1,
    unestimated_account_count: 0,
    pending_sync_account_count: 0
  }
}

const account = {
  id: 80,
  name: 'openai-oauth',
  platform: 'openai',
  type: 'oauth',
  status: 'active',
  schedulable: true,
  quota_dimension: 'global',
  created_at: '2026-08-16T00:00:00Z',
  updated_at: '2026-08-16T00:00:00Z'
}

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-schedulable" :row="row" />
      </div>
    </div>
  `
}

const OpenAIOAuthUsageSummaryStub = {
  props: ['summary', 'loading', 'error'],
  emits: ['retry'],
  template: '<div data-testid="openai-summary-stub">{{ summary?.included_account_count }}</div>'
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        AccountTableActions: { template: '<div data-testid="account-actions"><slot name="after" /></div>' },
        AccountTableFilters: { template: '<div data-testid="account-filters-stub" />' },
        OpenAIOAuthUsageSummary: OpenAIOAuthUsageSummaryStub,
        DataTable: DataTableStub,
        Pagination: true,
        ConfirmDialog: true,
        AccountBulkActionsBar: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        HelpTooltip: true,
        Icon: true
      }
    }
  })
}

describe('admin AccountsView OpenAI OAuth pool quota', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.useRealTimers()
    listAccounts.mockReset().mockResolvedValue({ items: [account], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockReset().mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockReset().mockResolvedValue({ stats: {} })
    getOpenAIOAuthUsageSummary.mockReset().mockResolvedValue(summary)
    getUpstreamBillingProbeSettings.mockReset().mockResolvedValue({ enabled: true, interval_minutes: 30 })
    getAllGroups.mockReset().mockResolvedValue([])
    getAllProxies.mockReset().mockResolvedValue([])
    setSchedulable.mockReset().mockResolvedValue({ ...account, schedulable: false })
  })

  it('keeps the quota summary in the action toolbar and independent of list filters', async () => {
    const wrapper = mountView()
    await flushPromises()

    const toolbar = wrapper.get('[data-testid="account-management-toolbar"]')
    expect(toolbar.find('[data-testid="account-actions"]').exists()).toBe(true)
    expect(toolbar.find('[data-testid="openai-summary-stub"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="account-management-filters"]').find('[data-testid="openai-summary-stub"]').exists()).toBe(false)
    expect(getOpenAIOAuthUsageSummary).toHaveBeenCalledTimes(1)
    expect(getOpenAIOAuthUsageSummary).toHaveBeenCalledWith()
  })

  it('refreshes the pool summary after changing account schedulability', async () => {
    const wrapper = mountView()
    await flushPromises()
    getOpenAIOAuthUsageSummary.mockClear()

    await wrapper.get('[data-testid="account-schedulable-toggle"]').trigger('click')
    await new Promise(resolve => setTimeout(resolve, 300))
    await flushPromises()

    expect(setSchedulable).toHaveBeenCalledWith(80, false)
    expect(getOpenAIOAuthUsageSummary).toHaveBeenCalledTimes(1)
  })
})
