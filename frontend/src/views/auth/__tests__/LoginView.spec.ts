import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import LoginView from '@/views/auth/LoginView.vue'

const {
  getPublicSettingsMock,
  pushMock,
  loginMock,
  showErrorMock,
  showWarningMock,
  showSuccessMock,
} = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  pushMock: vi.fn(),
  loginMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
  showSuccessMock: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: pushMock,
    currentRoute: {
      value: {
        query: {},
      },
    },
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, fallback?: string) => {
        const messages: Record<string, string> = {
          'auth.welcomeBack': '欢迎回来',
          'auth.signInToAccount': '登录您的账户以继续',
          'auth.emailLabel': '邮箱',
          'auth.passwordLabel': '密码',
          'auth.signIn': '登录',
          'auth.signingIn': '登录中...',
          'auth.forgotPassword': '忘记密码？',
          'auth.dontHaveAccount': '还没有账户？',
          'auth.signUp': '注册',
        }
        return messages[key] ?? (typeof fallback === 'string' ? fallback : key)
      },
    }),
  }
})

vi.mock('@/api/auth', () => ({
  getPublicSettings: (...args: any[]) => getPublicSettingsMock(...args),
  isTotp2FARequired: () => false,
  isWeChatWebOAuthEnabled: () => false,
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    login: (...args: any[]) => loginMock(...args),
  }),
  useAppStore: () => ({
    showError: (...args: any[]) => showErrorMock(...args),
    showWarning: (...args: any[]) => showWarningMock(...args),
    showSuccess: (...args: any[]) => showSuccessMock(...args),
  }),
}))

vi.mock('@/utils/oauthAffiliate', () => ({
  clearAllAffiliateReferralCodes: vi.fn(),
}))

describe('LoginView visual baseline', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    pushMock.mockReset()
    loginMock.mockReset()
    showErrorMock.mockReset()
    showWarningMock.mockReset()
    showSuccessMock.mockReset()

    getPublicSettingsMock.mockResolvedValue({
      turnstile_enabled: false,
      turnstile_site_key: '',
      linuxdo_oauth_enabled: false,
      dingtalk_oauth_enabled: false,
      oidc_oauth_enabled: false,
      oidc_oauth_provider_name: 'OIDC',
      github_oauth_enabled: false,
      google_oauth_enabled: false,
      password_reset_enabled: true,
      backend_mode_enabled: false,
      login_agreement_enabled: false,
      login_agreement_mode: 'checkbox',
      login_agreement_updated_at: '2026-03-31',
      login_agreement_revision: 'revision-disabled',
      login_agreement_documents: [],
    })
  })

  function mountLoginView() {
    return mount(LoginView, {
      global: {
        stubs: {
          AuthLayout: {
            props: ['eyebrow', 'title', 'subtitle'],
            template:
              '<div><p>{{ eyebrow }}</p><h2>{{ title }}</h2><span>{{ subtitle }}</span><slot /><slot name="footer" /></div>',
          },
          RouterLink: {
            template: '<a><slot /></a>',
          },
          Icon: true,
          TurnstileWidget: true,
          EmailOAuthButtons: true,
          LinuxDoOAuthSection: true,
          DingTalkOAuthSection: true,
          WechatOAuthSection: true,
          OidcOAuthSection: true,
          LoginAgreementPrompt: {
            name: 'LoginAgreementPrompt',
            props: ['accepted', 'documents', 'mode'],
            emits: ['accept', 'reject', 'open'],
            template:
              '<div data-test="login-agreement"><button type="button" data-test="accept-agreement" @click="$emit(\'accept\')">accept</button></div>',
          },
          TotpLoginModal: true,
        },
      },
    })
  }

  it('renders the Starlink auth card body with the black-pill submit', async () => {
    const wrapper = mountLoginView()

    await flushPromises()

    const html = wrapper.html()
    const text = wrapper.text()

    expect(text).toContain('登录星链')
    expect(html).toContain('auth-form')
    expect(html).toContain('auth-submit')
    expect(html).toContain('auth-secondary-link')
    expect(text).toContain('邮箱')
    expect(text).toContain('还没有账户？')
  })

  it('blocks backend login until the current login agreement revision is accepted', async () => {
    localStorage.removeItem('sub2api_login_agreement_consent')
    getPublicSettingsMock.mockResolvedValueOnce({
      turnstile_enabled: false,
      turnstile_site_key: '',
      linuxdo_oauth_enabled: false,
      dingtalk_oauth_enabled: false,
      oidc_oauth_enabled: false,
      oidc_oauth_provider_name: 'OIDC',
      github_oauth_enabled: false,
      google_oauth_enabled: false,
      password_reset_enabled: true,
      backend_mode_enabled: false,
      login_agreement_enabled: true,
      login_agreement_mode: 'checkbox',
      login_agreement_updated_at: '2026-03-31',
      login_agreement_revision: 'revision-2026-03-31',
      login_agreement_documents: [
        { id: 'terms', title: '服务条款', content_md: '' },
        { id: 'usage-policy', title: '使用政策', content_md: '' },
        { id: 'supported-regions', title: '支持的国家和地区', content_md: '' },
        { id: 'service-specific-terms', title: '服务特定条款', content_md: '' },
      ],
    })
    loginMock.mockResolvedValue({
      access_token: 'token',
      refresh_token: 'refresh-token',
      expires_in: 3600,
      user: { id: 1, email: 'user@example.com', role: 'user' },
    })

    const wrapper = mountLoginView()
    await flushPromises()

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(loginMock).not.toHaveBeenCalled()
    expect(showWarningMock).toHaveBeenCalledWith('legal.loginAgreementPrompt.loginRequiredWarning')

    wrapper.getComponent({ name: 'LoginAgreementPrompt' }).vm.$emit('accept')
    await wrapper.vm.$nextTick()
    expect(JSON.parse(localStorage.getItem('sub2api_login_agreement_consent') || '{}')).toMatchObject({
      revision: 'revision-2026-03-31',
    })
    await wrapper.get('#email').setValue('user@example.com')
    await wrapper.get('#password').setValue('password123')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(loginMock).toHaveBeenCalledWith({
      email: 'user@example.com',
      password: 'password123',
      turnstile_token: undefined,
    })
  })
})
