import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { listAccounts, bulkUpdate, showSuccess, showError } = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  bulkUpdate: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { accounts: { list: listAccounts, bulkUpdate } }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

import BindAccountsModal from '../BindAccountsModal.vue'

const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const proxy = {
  id: 7,
  name: 'Tokyo Proxy',
  protocol: 'http',
  host: 'proxy.example.com',
  port: 8080,
  status: 'active'
} as any

const account = (id: number, extra: Record<string, unknown> = {}) => ({
  id,
  name: `Account ${id}`,
  platform: 'openai',
  type: 'oauth',
  proxy_id: null,
  ...extra
}) as any

describe('BindAccountsModal', () => {
  beforeEach(() => {
    listAccounts.mockReset().mockResolvedValue({
      items: [account(1), account(2, { proxy_id: 7 }), account(3, { parent_account_id: 10 })],
      total: 3,
      page: 1,
      page_size: 20,
      pages: 1
    })
    bulkUpdate.mockReset().mockResolvedValue({ success: 1, failed: 0, results: [] })
    showSuccess.mockReset()
    showError.mockReset()
  })

  it('binds selected existing accounts to the current proxy', async () => {
    const wrapper = mount(BindAccountsModal, {
      props: { show: true, proxy },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Pagination: true,
          PlatformTypeBadge: true,
          Icon: true
        }
      }
    })
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(1, 20, undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.get('input[data-account-id="2"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('input[data-account-id="3"]').attributes('disabled')).toBeDefined()

    await wrapper.get('input[data-account-id="1"]').setValue(true)
    await wrapper.findAll('button').find((button) => button.text().includes('bindSelectedAccounts'))?.trigger('click')
    await flushPromises()

    expect(bulkUpdate).toHaveBeenCalledWith([1], { proxy_id: 7 })
    expect(wrapper.emitted('bound')).toHaveLength(1)
    expect(showSuccess).toHaveBeenCalled()
  })
})
