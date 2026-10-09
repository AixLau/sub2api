import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import PluginsView from '../PluginsView.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { PluginUISession } from '@/api/admin'

enableAutoUnmount(afterEach)

const {
  listPlugins,
  uploadPlugin,
  enablePlugin,
  disablePlugin,
  showError,
  showSuccess,
  savePluginConfig,
  createUISession,
  stepUpRun,
} = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  uploadPlugin: vi.fn(),
  enablePlugin: vi.fn(),
  disablePlugin: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  savePluginConfig: vi.fn(),
  createUISession: vi.fn(),
  stepUpRun: vi.fn((action: () => Promise<unknown>) => action()),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    plugins: {
      list: listPlugins,
      upload: uploadPlugin,
      enable: enablePlugin,
      disable: disablePlugin,
      remove: vi.fn(),
      getConfig: vi.fn().mockResolvedValue({}),
      saveConfig: savePluginConfig,
      test: vi.fn().mockResolvedValue({ success: true, message: 'ok', latency_ms: 1 }),
      createUISession,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const plugin = {
  id: 7,
  plugin_key: 'local.test.transport',
  name: 'Test Transport',
  version: '1.0.0',
  description: '',
  author: 'test',
  manifest: {
    schema_version: 1,
    id: 'local.test.transport',
    name: 'Test Transport',
    version: '1.0.0',
    requires: {
      sub2api: '>=0.1.0',
      plugin_protocol: 1,
      transport_api: 1,
      ui_bridge: 1,
    },
    capabilities: [],
    ui: { entrypoint: 'ui/index.html' },
  },
  binary_sha256: 'a'.repeat(64),
  signature_status: 'trusted' as const,
  state: 'disabled' as const,
  last_error: '',
  installed_at: '2026-08-22T00:00:00Z',
  updated_at: '2026-08-22T00:00:00Z',
  bindings: [
    {
      id: 1,
      plugin_id: 7,
      capability: 'openai.oauth.outbound_transport.v1',
      platform: 'openai',
      account_type: 'oauth',
      enabled: false,
      account_ids: [],
    },
  ],
  compatibility: {
    compatible: true,
    tested: true,
    status: 'compatible' as const,
    message: '',
    current_sub2api_version: '0.1.0',
    required_sub2api_version: '>=0.1.0',
    recommended_sub2api_version: '0.1.0',
    plugin_protocol: 1,
    transport_api: 1,
    ui_bridge: 1,
  },
  runtime_healthy: false,
  runtime_message: '',
}

function mountView() {
  return mount(PluginsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { template: '<div><slot /></div>' },
        PluginAccountScopeDialog: {
          name: 'PluginAccountScopeDialog',
          props: ['show', 'editing', 'initialAccountIds'],
          emits: ['confirm', 'close'],
          template:
            '<button v-if="show" data-test="confirm-account-scope" @click="$emit(\'confirm\', [11, 17])">confirm</button>',
        },
        Icon: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('管理员插件页二次验证', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    listPlugins.mockResolvedValue([plugin])
    uploadPlugin.mockResolvedValue(plugin)
    enablePlugin.mockResolvedValue(plugin)
    savePluginConfig.mockResolvedValue({ enabled: true })
    createUISession.mockResolvedValue({
      url: '/api/v1/plugin-ui/token/index.html#bridge_token=bridge',
      bridge_token: 'bridge',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z',
    })
  })

  it('启用插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.enable'))
    expect(button).toBeDefined()
    await button!.trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="confirm-account-scope"]').trigger('click')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(enablePlugin).toHaveBeenCalledWith(7, [11, 17], false)
  })

  it('运行中通过二次验证更新账号绑定，不停用插件', async () => {
    const running = {
      ...plugin,
      state: 'enabled',
      runtime_healthy: true,
      compatibility: { ...plugin.compatibility, tested: false },
      bindings: [{ ...plugin.bindings[0], enabled: true, account_ids: [11] }],
    }
    listPlugins.mockResolvedValue([running])
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = mountView()
    try {
      await flushPromises()
      const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.manageAccounts'))
      expect(button).toBeDefined()
      await button!.trigger('click')
      await flushPromises()
      const dialog = wrapper.findComponent({ name: 'PluginAccountScopeDialog' })
      expect(dialog.props('editing')).toBe(true)
      expect(dialog.props('initialAccountIds')).toEqual([11])
      await wrapper.get('[data-test="confirm-account-scope"]').trigger('click')
      await flushPromises()
      expect(stepUpRun).toHaveBeenCalledTimes(1)
      expect(enablePlugin).toHaveBeenCalledWith(7, [11, 17], false)
      expect(disablePlugin).not.toHaveBeenCalled()
      expect(confirm).not.toHaveBeenCalled()
      expect(showSuccess).toHaveBeenCalledWith('admin.plugins.updateAccountsSuccess')
      expect(dialog.props('show')).toBe(false)
    } finally {
      wrapper.unmount()
      confirm.mockRestore()
    }
  })

  it('独立 Codex ticket 插件读取自身的账号绑定', async () => {
    const ticketPlugin = {
      ...plugin,
      plugin_key: 'local.test.codex-ticket',
      name: 'Codex Ticket Hook',
      bindings: [{
        ...plugin.bindings[0],
        capability: 'openai.oauth.codex_ticket_hook.v1',
        enabled: true,
        account_ids: [17],
      }],
      state: 'enabled' as const,
      runtime_healthy: true,
    }
    listPlugins.mockResolvedValue([ticketPlugin])
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.manageAccounts'))!.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'PluginAccountScopeDialog' }).props('initialAccountIds')).toEqual([17])
    wrapper.unmount()
  })

  it('绑定保存失败保留账号选择窗口', async () => {
    listPlugins.mockResolvedValue([{
      ...plugin, state: 'enabled', runtime_healthy: true,
      bindings: [{ ...plugin.bindings[0], enabled: true, account_ids: [11] }],
    }])
    enablePlugin.mockRejectedValueOnce(new Error('binding save failed'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.manageAccounts'))!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="confirm-account-scope"]').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'PluginAccountScopeDialog' }).props('show')).toBe(true)
    expect(showError).toHaveBeenCalledWith('binding save failed')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(disablePlugin).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([7, 8])('keeps the latest session when reopening configuration for plugin %s', async (id) => {
    let resolveOld!: (session: PluginUISession) => void
    createUISession.mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveOld = resolve }))
    listPlugins.mockResolvedValue([plugin, { ...plugin, id: 8, name: 'Other Transport' }])
    const wrapper = mountView()
    await flushPromises()
    const buttons = wrapper.findAll('button').filter(item => item.text() === 'admin.plugins.configure')
    await buttons[0].trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await buttons[id === 7 ? 0 : 1].trigger('click')
    await flushPromises()
    const currentURL = wrapper.get('iframe').attributes('src')

    resolveOld({ url: '/obsolete-session', bridge_token: 'old', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toBe(currentURL)
  })

  it('ignores an old session error while the reopened configuration is loading', async () => {
    let rejectOld!: (error: Error) => void
    let resolveCurrent!: (session: PluginUISession) => void
    createUISession
      .mockReturnValueOnce(new Promise((_, reject) => { rejectOld = reject }))
      .mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveCurrent = resolve }))
    const wrapper = mountView()
    await flushPromises()
    const button = wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!
    await button.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    await button.trigger('click')
    rejectOld(new Error('obsolete failure'))
    await flushPromises()

    expect(wrapper.text()).not.toContain('obsolete failure')
    expect(wrapper.text()).toContain('admin.plugins.loadingUI')
    resolveCurrent({ url: '/current-session', bridge_token: 'current', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    await wrapper.get('iframe').trigger('load')
    expect(wrapper.get('iframe').attributes('src')).toBe('/current-session')
    expect(wrapper.text()).not.toContain('admin.plugins.loadingUI')
  })

  it('does not restore a session after the configuration dialog closes', async () => {
    let resolveSession!: (session: PluginUISession) => void
    createUISession.mockReturnValueOnce(new Promise<PluginUISession>((resolve) => { resolveSession = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text() === 'admin.plugins.configure')!.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    resolveSession({ url: '/closed-session', bridge_token: 'old', ui_bridge_version: 1, expires_at: '' })
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
  })

  it('上传插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: [new File(['plugin'], 'transport.s2plugin', { type: 'application/zip' })],
    })

    await input.trigger('change')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(uploadPlugin).toHaveBeenCalledTimes(1)
  })
})
