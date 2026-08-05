import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MerchantIntegrationsView from '../MerchantIntegrationsView.vue'

const {
  listIntegrations,
  getIntegrationById,
  listUsers,
  testMerchantEndpoint,
  removeEndpoint,
} = vi.hoisted(() => ({
  listIntegrations: vi.fn(),
  getIntegrationById: vi.fn(),
  listUsers: vi.fn(),
  testMerchantEndpoint: vi.fn(),
  removeEndpoint: vi.fn(),
}))

const showError = vi.fn()
const showSuccess = vi.fn()

vi.mock('@/api/admin', () => ({
  adminAPI: {
    merchantIntegrations: {
      list: (...args: unknown[]) => listIntegrations(...args),
      getById: (...args: unknown[]) => getIntegrationById(...args),
      testEndpoint: (...args: unknown[]) => testMerchantEndpoint(...args),
      removeEndpoint: (...args: unknown[]) => removeEndpoint(...args),
    },
    users: {
      list: (...args: unknown[]) => listUsers(...args),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (error: unknown, fallback: string) =>
    error instanceof Error ? error.message : fallback,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (params && 'count' in params) return `${key}:${params.count}`
        return key
      },
    }),
  }
})

const integration = {
  id: 7,
  name: 'ACME SSO',
  code: 'acme',
  mode: 'dynamic_api',
  merchant_code: 'ACME',
  description: 'demo',
  status: 'active',
  enabled: true,
  redirect_hosts: ['sso.acme.test'],
  endpoints: [
    {
      id: 31,
      integration_id: 7,
      type: 'login',
      url: 'https://merchant.example/login',
      method: 'POST',
      content_type: 'application/json',
      query_template: {},
      header_template: {},
      body_template: {
        user_id: '{{user_id}}',
        start_time: '{{start_time}}',
        end_time: '{{end_time}}',
      },
      auth_type: 'none',
      secret_ref: '',
      response_mapping: {},
      success_rule: {},
      retry_policy: {},
      timeout_ms: 10000,
      status: 'active',
      enabled: true,
      created_at: '2026-08-05T00:00:00Z',
      updated_at: '2026-08-05T00:00:00Z',
    },
  ],
  created_at: '2026-08-05T00:00:00Z',
  updated_at: '2026-08-05T00:00:00Z',
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function mountView() {
  return mount(MerchantIntegrationsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: { template: '<span />' },
        Toggle: { template: '<button type="button" />' },
        BaseDialog: {
          props: ['show', 'title'],
          template: '<section v-if="show" :data-title="title"><slot /><div data-test="footer"><slot name="footer" /></div></section>',
        },
        ConfirmDialog: {
          props: ['show', 'title', 'message', 'confirmText', 'cancelText'],
          emits: ['confirm', 'cancel'],
          template: `
            <section v-if="show" :data-title="title">
              <p data-test="message">{{ message }}</p>
              <slot />
              <button type="button" data-test="cancel" @click="$emit('cancel')">{{ cancelText }}</button>
              <button type="button" data-test="confirm" @click="$emit('confirm')">{{ confirmText }}</button>
            </section>
          `,
        },
      },
    },
  })
}

describe('MerchantIntegrationsView', () => {
  beforeEach(() => {
    showError.mockReset()
    showSuccess.mockReset()
    listIntegrations.mockReset()
    getIntegrationById.mockReset()
    listUsers.mockReset()
    testMerchantEndpoint.mockReset()
    removeEndpoint.mockReset()
    listIntegrations.mockResolvedValue([integration])
    getIntegrationById.mockResolvedValue(integration)
    listUsers.mockResolvedValue({ items: [{ id: 42, email: 'tester@example.com', username: 'tester' }] })
    testMerchantEndpoint.mockResolvedValue({
      endpoint_id: 31,
      http_status: 200,
      successful: true,
      message: 'ok',
    })
    removeEndpoint.mockResolvedValue(undefined)
  })

  it('passes user_id and time bounds from the test dialog to the endpoint test request', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[title="admin.merchant.test"]').trigger('click')
    await flushPromises()

    const dialog = wrapper.get('[data-title="admin.merchant.testParameters"]')
    await dialog.get('input[type="number"]').setValue('42')
    const timeInputs = dialog.findAll('input[type="datetime-local"]')
    await timeInputs[0].setValue('2026-08-05T10:00')
    await timeInputs[1].setValue('2026-08-05T11:30')
    await dialog.findAll('button').at(-1)!.trigger('click')
    await flushPromises()

    expect(testMerchantEndpoint).toHaveBeenCalledWith(7, 31, {
      user_id: 42,
      start_time: new Date('2026-08-05T10:00').toISOString(),
      end_time: new Date('2026-08-05T11:30').toISOString(),
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.merchant.testPassed')
    expect(wrapper.get('[data-title="admin.merchant.testResult"]').text()).toContain('admin.merchant.httpStatus')
  })

  it('shows delete loading state before removing an endpoint and refreshes the list after success', async () => {
    const pending = deferred<void>()
    removeEndpoint.mockReturnValueOnce(pending.promise)
    getIntegrationById.mockResolvedValueOnce(integration)
    getIntegrationById.mockResolvedValueOnce({ ...integration, endpoints: [] })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button[title="admin.merchant.deleteEndpoint"]').trigger('click')
    await flushPromises()

    const confirmDialog = wrapper.get('[data-title="admin.merchant.deleteEndpoint"]')
    await confirmDialog.get('[data-test="confirm"]').trigger('click')
    await nextTick()

    expect(confirmDialog.text()).toContain('admin.merchant.deleting')

    pending.resolve()
    await flushPromises()

    expect(removeEndpoint).toHaveBeenCalledWith(7, 31)
    expect(showSuccess).toHaveBeenCalledWith('admin.merchant.endpointDeleted')
    expect(wrapper.text()).not.toContain('admin.merchant.deleteEndpointConfirm')
  })
})
