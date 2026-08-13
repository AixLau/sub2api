import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ClientSetupView from '../ClientSetupView.vue'

const routeQuery = vi.hoisted(() => ({
  setup_id: 'setup-123',
  device_code: 'ABCD-1234',
  client: 'codex'
}))

const getSession = vi.hoisted(() => vi.fn())
const approveSession = vi.hoisted(() => vi.fn())

vi.mock('vue-router', () => ({
  useRoute: () => ({
    query: routeQuery
  })
}))

vi.mock('@/api', () => ({
  clientSetupAPI: {
    getSession,
    approveSession
  }
}))

describe('ClientSetupView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    routeQuery.setup_id = 'setup-123'
    routeQuery.device_code = 'ABCD-1234'
    routeQuery.client = 'codex'
    getSession.mockResolvedValue({
      setup_id: 'setup-123',
      device_code: 'ABCD-1234',
      client: 'codex',
      status: 'pending'
    })
    approveSession.mockResolvedValue({
      setup_id: 'setup-123',
      client: 'codex',
      status: 'approved',
      setup_token: 'setup-token-123',
      redirect_uri: 'http://127.0.0.1:38173/callback?setup_token=setup-token-123'
    })

    Object.defineProperty(window, 'location', {
      value: { href: '' },
      writable: true,
      configurable: true
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('confirms the setup session without requiring a click', async () => {
    mount(ClientSetupView)

    await flushPromises()

    expect(getSession).toHaveBeenCalledWith('setup-123')
    expect(approveSession).toHaveBeenCalledWith('setup-123', {
      device_code: 'ABCD-1234',
      client: 'codex'
    })
    expect(window.location.href).toBe('')

    await vi.advanceTimersByTimeAsync(9_999)
    expect(window.location.href).toBe('')

    await vi.advanceTimersByTimeAsync(1)
    expect(window.location.href).toBe('https://aixlau.me/dashboard')
  })

  it('shows a neutral notification instead of an approval button while confirming', async () => {
    let resolveApprove: (value: unknown) => void = () => {}
    approveSession.mockReturnValue(new Promise((resolve) => {
      resolveApprove = resolve
    }))

    const wrapper = mount(ClientSetupView)

    await flushPromises()

    expect(wrapper.text()).toContain('正在处理本次配置')
    expect(wrapper.text()).not.toContain('API Key')
    expect(wrapper.text()).not.toContain('无需点击同意')
    expect(wrapper.text()).not.toContain('浏览器登录状态')
    expect(wrapper.text()).not.toContain('本机脚本')
    expect(wrapper.text()).not.toContain('配置会话')
    expect(wrapper.find('button').exists()).toBe(false)

    resolveApprove({
      setup_id: 'setup-123',
      client: 'codex',
      status: 'approved',
      setup_token: 'setup-token-123',
      redirect_uri: 'http://127.0.0.1:38173/callback?setup_token=setup-token-123'
    })
    await flushPromises()

    expect(wrapper.text()).toContain('配置确认完成')
    expect(window.location.href).toBe('')
  })
})
