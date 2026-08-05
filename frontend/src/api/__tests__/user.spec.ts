import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

describe('user api oauth binding urls', () => {
  beforeEach(() => {
    vi.resetModules()
    get.mockReset()
    post.mockReset()
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/api/v1')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('builds third-party bind urls against the bind start endpoint', async () => {
    const { buildOAuthBindingStartURL } = await import('@/api/user')

    expect(buildOAuthBindingStartURL('linuxdo', { redirectTo: '/settings/profile' })).toBe(
      'https://api.example.com/api/v1/auth/oauth/linuxdo/bind/start?redirect=%2Fsettings%2Fprofile&intent=bind_current_user'
    )
    expect(
      buildOAuthBindingStartURL('wechat', {
        redirectTo: '/settings/profile',
        wechatOAuthSettings: {
          wechat_oauth_open_enabled: true,
          wechat_oauth_mp_enabled: false,
          wechat_oauth_mobile_enabled: false
        }
      })
    ).toBe(
      'https://api.example.com/api/v1/auth/oauth/wechat/bind/start?redirect=%2Fsettings%2Fprofile&intent=bind_current_user&mode=open'
    )
  })

  it('calls the user binding action endpoints', async () => {
    post.mockResolvedValue({ data: { binding: { id: 7 } } })
    const {
      syncMerchantBinding,
      bindMerchantBinding,
      refreshMerchantBindingStatus,
    } = await import('@/api/user')

    await syncMerchantBinding(7)
    await bindMerchantBinding(7)
    await refreshMerchantBindingStatus(7)

    expect(post.mock.calls).toEqual([
      ['/merchant-integrations/bindings/7/sync'],
      ['/merchant-integrations/bindings/7/bind'],
      ['/merchant-integrations/bindings/7/status'],
    ])
  })
})
