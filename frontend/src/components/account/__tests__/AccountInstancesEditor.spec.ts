import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountInstancesEditor from '../AccountInstancesEditor.vue'
import OpenAIInstanceForm from '../OpenAIInstanceForm.vue'
import * as api from '@/api/admin/credentialPrincipals'

vi.mock('vue-i18n', async () => ({ ...(await vi.importActual('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/credentialPrincipals', () => ({
  getCredentialPrincipal: vi.fn(), getCredentialRuntime: vi.fn(), saveCredentialConfiguration: vi.fn(),
  controlCredentialInstance: vi.fn(), refreshCredentialInstance: vi.fn(), resolveCredentialLease: vi.fn(),
  credentialErrorReason: (error: { reason?: string }) => error.reason || 'UNAVAILABLE'
}))

const principal = {
  id: 9, account_id: 42, name: 'Account', group_ids: [7], proxy_id: null,
  config_version: 2, account_max_concurrency: 20, occupied: 0, admin_state: 'ACTIVE', routing_mode: 'GROUPED',
  instances: [{ id: 5, name: 'Instance', max_concurrency: 10, occupied: 0, active_bindings: 0, admin_state: 'ACTIVE', state: 'AVAILABLE' }]
} as api.CredentialPrincipal

const make = () => mount(AccountInstancesEditor, {
  props: { principalId: 9 },
  global: { stubs: { TotpStepUpDialog: true, InstanceCapacitySummary: true, OpenAIInstanceForm: true } }
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getCredentialPrincipal).mockResolvedValue(structuredClone(principal))
  vi.mocked(api.getCredentialRuntime).mockResolvedValue({ wait_reasons: [], unresolved_leases: [] } as unknown as api.CredentialRuntime)
  vi.mocked(api.saveCredentialConfiguration).mockResolvedValue({ ...principal, config_version: 3 })
})

describe('inline account instances', () => {
  it('saves instance configuration without overwriting account settings or opening a dialog', async () => {
    const wrapper = make()
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await wrapper.get('input[type="number"]').setValue(12)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(api.saveCredentialConfiguration).toHaveBeenCalledWith(principal, {
      instances: [{ id: 5, name: 'Instance', max_concurrency: 12 }]
    })
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ config_version: 3 })
    wrapper.unmount()
  })

  it('holds the account editor busy throughout adding an authorization', async () => {
    const wrapper = make()
    await flushPromises()
    const add = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.instances.add')!
    await add.trigger('click')
    expect(wrapper.findComponent(OpenAIInstanceForm).exists()).toBe(true)
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([true])
    await add.trigger('click')
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
    wrapper.unmount()
  })

  it('refreshes the version after a conflict and does not report a successful update', async () => {
    const wrapper = make()
    await flushPromises()
    vi.mocked(api.saveCredentialConfiguration).mockRejectedValueOnce({ reason: 'CONFIG_VERSION_CONFLICT' })
    vi.mocked(api.getCredentialPrincipal).mockResolvedValueOnce({ ...principal, config_version: 4 })
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('CONFIG_VERSION_CONFLICT')
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.emitted('loaded')?.at(-1)?.[0]).toMatchObject({ config_version: 4 })
    wrapper.unmount()
  })
})
