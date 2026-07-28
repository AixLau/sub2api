import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RegisterView from '@/views/auth/RegisterView.vue'

const {
  getPublicSettingsMock,
  showErrorMock,
  showWarningMock,
  showSuccessMock,
} = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
  showSuccessMock: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn(),
  }),
  useRoute: () => ({
    query: {},
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: any) => {
        const messages: Record<string, string> = {
          'auth.createAccount': '创建账户',
          'auth.signUpToStart': `注册 ${params?.siteName ?? 'Sub2API'} 以开始使用`,
          'auth.registerHeroKicker': '开发者控制台入口',
          'auth.registerHeroTitle': '统一 API 网关，连接所有 AI 模型',
          'auth.registerHeroDescription': '创建账户后即可进入控制台，管理密钥、上游模型、额度与调用记录。',
          'auth.registerPanelKicker': '开始配置您的网关',
          'auth.registerFeatureKeysTitle': '统一密钥',
          'auth.registerFeatureKeysDesc': '一个账户集中管理 API Key、访问权限与调用入口。',
          'auth.registerFeatureRoutingTitle': '多模型路由',
          'auth.registerFeatureRoutingDesc': '接入 GPT、Claude、Gemini 等模型。',
          'auth.registerFeatureControlTitle': '额度可控',
          'auth.registerFeatureControlDesc': '注册后查看余额、订阅配额和团队使用边界。',
          'auth.registerSignalGateway': '统一 API 网关',
          'auth.registerSignalRouting': '多模型路由',
          'auth.registerSignalUsage': '用量与额度管理',
          'auth.emailLabel': '邮箱',
          'auth.passwordLabel': '密码',
          'auth.showPassword': '显示密码',
          'auth.hidePassword': '隐藏密码',
          'auth.signIn': '登录',
          'auth.alreadyHaveAccount': '已经有账户？',
        }
        return messages[key] ?? (typeof params === 'string' ? params : key)
      },
      locale: { value: 'zh' },
    }),
  }
})

vi.mock('@/api/auth', () => ({
  getPublicSettings: (...args: any[]) => getPublicSettingsMock(...args),
  isWeChatWebOAuthEnabled: () => false,
  validatePromoCode: vi.fn(),
  validateInvitationCode: vi.fn(),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    register: vi.fn(),
  }),
  useAppStore: () => ({
    showError: (...args: any[]) => showErrorMock(...args),
    showWarning: (...args: any[]) => showWarningMock(...args),
    showSuccess: (...args: any[]) => showSuccessMock(...args),
  }),
}))

vi.mock('@/utils/oauthAffiliate', () => ({
  clearAffiliateReferralCode: vi.fn(),
  loadAffiliateReferralCode: vi.fn(),
  resolveAffiliateReferralCode: vi.fn(() => ''),
}))

describe('RegisterView visual baseline', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    showErrorMock.mockReset()
    showWarningMock.mockReset()
    showSuccessMock.mockReset()

    getPublicSettingsMock.mockResolvedValue({
      registration_enabled: true,
      email_verify_enabled: false,
      promo_code_enabled: false,
      invitation_code_enabled: false,
      turnstile_enabled: false,
      turnstile_site_key: '',
      site_name: 'Sub2API',
      site_logo: '',
      doc_url: '',
      linuxdo_oauth_enabled: false,
      wechat_oauth_enabled: false,
      oidc_oauth_enabled: false,
      oidc_oauth_provider_name: 'OIDC',
      github_oauth_enabled: false,
      google_oauth_enabled: false,
      registration_email_suffix_whitelist: [],
      login_agreement_enabled: false,
      login_agreement_documents: [],
    })

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
  })

  it('renders the Starlink registration card', async () => {
    const wrapper = mount(RegisterView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          AuthLayout: {
            props: ['eyebrow', 'title', 'subtitle'],
            template:
              '<div><p>{{ eyebrow }}</p><h2>{{ title }}</h2><span>{{ subtitle }}</span><slot /><slot name="footer" /></div>',
          },
          Icon: true,
          TurnstileWidget: true,
          EmailOAuthButtons: true,
          LinuxDoOAuthSection: true,
          WechatOAuthSection: true,
          OidcOAuthSection: true,
          LoginAgreementPrompt: true,
        },
      },
    })

    await flushPromises()

    const html = wrapper.html()
    const text = wrapper.text()

    expect(text).toContain('创建账号')
    expect(text).toContain('填写基础信息，开始接入稳定的模型 API 服务层。')
    expect(html).toContain('auth-form')
    expect(html).toContain('auth-submit')
    expect(text).toContain('邮箱')
    expect(text).toContain('已经有账户？')
  })
})

const invitationPublicSettings = {
  registration_enabled: true,
  email_verify_enabled: false,
  promo_code_enabled: false,
  invitation_code_enabled: false,
  affiliate_enabled: true,
  turnstile_enabled: true,
  turnstile_site_key: 'site-key',
  site_name: 'Sub2API',
  registration_email_suffix_whitelist: [],
  linuxdo_oauth_enabled: false,
  wechat_oauth_enabled: false,
  oidc_oauth_enabled: false,
  github_oauth_enabled: false,
  google_oauth_enabled: false
}

function mountRegister() {
  return mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: { template: '<div data-testid="turnstile-widget" />' },
        LoginAgreementPrompt: true,
        EmailOAuthButtons: true,
        LinuxDoOAuthSection: true,
        WechatOAuthSection: true,
        OidcOAuthSection: true,
        RouterLink: true,
        transition: false
      }
    }
  })
}

describe('RegisterView invitation layout', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    getPublicSettingsMock.mockResolvedValue(invitationPublicSettings)
  })

  it('keeps the optional affiliate invitation field before Turnstile', async () => {
    const wrapper = mountRegister()
    await flushPromises()

    const invitationField = wrapper.get('[data-testid="affiliate-invitation-field"]')
    const turnstile = wrapper.get('[data-testid="registration-turnstile"]')

    expect(invitationField.get('input').attributes('id')).toBe('affiliate_code')
    expect(invitationField.text()).toContain('common.optional')
    expect(
      invitationField.element.compareDocumentPosition(turnstile.element) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('uses the mandatory invitation field without duplicating the affiliate field', async () => {
    getPublicSettingsMock.mockResolvedValueOnce({
      ...invitationPublicSettings,
      invitation_code_enabled: true
    })

    const wrapper = mountRegister()
    await flushPromises()

    expect(wrapper.find('[data-testid="affiliate-invitation-field"]').exists()).toBe(false)
    expect(wrapper.get('#invitation_code').exists()).toBe(true)
  })
})

describe('RegisterView promo code visibility', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
  })

  it('does not show the promo code field before settings explicitly enable it', async () => {
    let resolveSettings!: (settings: typeof invitationPublicSettings) => void
    getPublicSettingsMock.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSettings = resolve
        })
    )

    const wrapper = mountRegister()

    expect(wrapper.find('[data-testid="promo-code-field"]').exists()).toBe(false)

    resolveSettings({
      ...invitationPublicSettings,
      promo_code_enabled: true
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="promo-code-field"]').exists()).toBe(true)
  })
})
