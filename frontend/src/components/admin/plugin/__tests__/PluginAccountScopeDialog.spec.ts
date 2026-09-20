import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PluginAccountScopeDialog from '../PluginAccountScopeDialog.vue'

const { listAccounts } = vi.hoisted(() => ({ listAccounts: vi.fn() }))

vi.mock('@/api/admin', () => ({
  adminAPI: { accounts: { list: listAccounts } },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('PluginAccountScopeDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listAccounts.mockResolvedValue({
      items: [
        { id: 5, name: 'OAuth Five', status: 'active', extra: { email_address: 'five@example.com' } },
        { id: 9, name: 'OAuth Nine', status: 'inactive', extra: { email_address: 'nine@example.com' } },
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1,
    })
  })

  it('loads only OpenAI OAuth accounts and confirms the exact selection', async () => {
    const wrapper = mount(PluginAccountScopeDialog, {
      props: {
        show: true,
        pluginName: 'Transport',
        initialAccountIds: [9],
        submitting: false,
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          Pagination: true,
          Icon: true,
        },
      },
    })
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(
      1,
      20,
      expect.objectContaining({ platform: 'openai', type: 'oauth', lite: '1' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(3)
    expect((checkboxes[2].element as HTMLInputElement).checked).toBe(true)
    await checkboxes[1].trigger('change')
    await wrapper.get('[data-test="confirm-plugin-accounts"]').trigger('click')

    expect(wrapper.emitted('confirm')).toEqual([[[5, 9]]])
  })
})
