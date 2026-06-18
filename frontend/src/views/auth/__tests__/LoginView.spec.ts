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
      login_agreement_documents: [],
    })
  })

  it('renders the restored white-and-blue split login layout', async () => {
    const wrapper = mount(LoginView, {
      global: {
        stubs: {
          AuthLayout: {
            template: '<div><slot /><slot name="footer" /></div>',
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
          LoginAgreementPrompt: true,
          TotpLoginModal: true,
        },
      },
    })

    await flushPromises()

    const html = wrapper.html()
    const text = wrapper.text()

    expect(html).toContain('bg-gradient-to-br from-blue-600 via-cyan-600 to-blue-700')
    expect(html).toContain('lg:w-1/2')
    expect(text).toContain('统一 API 密钥管理')
    expect(text).toContain('返回首页')
  })
})
