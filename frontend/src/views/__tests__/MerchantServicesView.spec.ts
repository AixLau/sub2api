import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { listMerchantIntegrations, listMerchantBindings, launchMerchantIntegration, syncMerchantBinding, bindMerchantBinding, refreshMerchantBindingStatus } = vi.hoisted(() => ({
  listMerchantIntegrations: vi.fn(),
  listMerchantBindings: vi.fn(),
  launchMerchantIntegration: vi.fn(),
  syncMerchantBinding: vi.fn(),
  bindMerchantBinding: vi.fn(),
  refreshMerchantBindingStatus: vi.fn(),
}))

const { showError } = vi.hoisted(() => ({ showError: vi.fn() }))

vi.mock('@/api/user', () => ({
  userAPI: {
    listMerchantIntegrations,
    listMerchantBindings,
    launchMerchantIntegration,
    syncMerchantBinding,
    bindMerchantBinding,
    refreshMerchantBindingStatus,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'common.loading': 'Loading',
    'merchant.title': 'Merchant Services',
    'merchant.description': 'Description',
    'merchant.empty': 'Empty',
    'merchant.openInNewWindow': 'New window',
    'merchant.open': 'Open',
    'merchant.opening': 'Opening',
    'merchant.bindingsTitle': 'Connected accounts',
    'merchant.account': 'Account',
    'merchant.externalUserId': 'External user ID',
    'merchant.lastLogin': 'Last login',
    'merchant.never': 'Never',
    'merchant.noBinding': 'No binding',
    'merchant.sync': 'Sync',
    'merchant.syncing': 'Syncing',
    'merchant.bind': 'Bind',
    'merchant.binding': 'Binding',
    'merchant.checkStatus': 'Check status',
    'merchant.checking': 'Checking',
    'merchant.syncSuccess': 'Synced',
    'merchant.bindSuccess': 'Bound',
    'merchant.statusSuccess': 'Status refreshed',
    'merchant.syncFailed': 'Sync failed',
    'merchant.bindFailed': 'Bind failed',
    'merchant.statusFailed': 'Status failed',
    'merchant.launchFailed': 'Launch failed',
  }
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
  }
})

import MerchantServicesView from '../user/MerchantServicesView.vue'

const binding = {
  id: 7,
  integration_id: 3,
  integration_name: 'Merchant A',
  integration_code: 'merchant_a',
  external_user_id: 'external-7',
  external_account: 'user@example.com',
  status: 'active',
}

describe('MerchantServicesView binding actions', () => {
  beforeEach(() => {
    listMerchantIntegrations.mockResolvedValue([{ id: 3, name: 'Merchant A', code: 'merchant_a', description: '' }])
    listMerchantBindings.mockResolvedValue([binding])
    syncMerchantBinding.mockResolvedValue({ binding })
    bindMerchantBinding.mockResolvedValue({ binding })
    refreshMerchantBindingStatus.mockResolvedValue({ binding })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('runs sync, shows success, and refreshes bindings', async () => {
    const wrapper = mount(MerchantServicesView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } },
    })
    await flushPromises()

    const syncButton = wrapper.findAll('button').find((button) => button.text() === 'Sync')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(syncMerchantBinding).toHaveBeenCalledWith(7)
    expect(listMerchantBindings).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Synced')
  })

  it('shows an action error and leaves the binding list intact', async () => {
    syncMerchantBinding.mockRejectedValueOnce(new Error('merchant unavailable'))
    const wrapper = mount(MerchantServicesView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true } },
    })
    await flushPromises()

    const syncButton = wrapper.findAll('button').find((button) => button.text() === 'Sync')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('merchant unavailable')
    expect(listMerchantBindings).toHaveBeenCalledTimes(1)
  })
})
