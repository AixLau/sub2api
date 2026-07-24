import { shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AppHeader from '../AppHeader.vue'

const mocks = vi.hoisted(() => ({
  appStore: {
    contactInfo: 'youngPupss',
    supportQQGroupQRCode: 'data:image/png;base64,qq-code',
    supportWeChatGroupQRCode: 'data:image/png;base64,wechat-code',
    docUrl: '',
    cachedPublicSettings: null,
    toggleMobileSidebar: vi.fn()
  },
  authStore: {
    user: null,
    isAdmin: false,
    isSimpleMode: false,
    logout: vi.fn()
  },
  onboardingStore: {
    replay: vi.fn()
  },
  routerPush: vi.fn()
}))

vi.mock('@/stores', () => ({
  useAppStore: () => mocks.appStore,
  useAuthStore: () => mocks.authStore,
  useOnboardingStore: () => mocks.onboardingStore
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.routerPush }),
  useRoute: () => ({ name: 'Profile', meta: {}, params: {} })
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

describe('AppHeader contact support entry', () => {
  beforeEach(() => {
    mocks.appStore.contactInfo = 'youngPupss'
    mocks.appStore.supportQQGroupQRCode = 'data:image/png;base64,qq-code'
    mocks.appStore.supportWeChatGroupQRCode = 'data:image/png;base64,wechat-code'
    mocks.authStore.user = null
    mocks.routerPush.mockReset()
  })

  it('opens the wallet panel on hover and routes the primary recharge action', async () => {
    mocks.authStore.user = {
      username: 'demo',
      email: 'demo@example.com',
      role: 'user',
      balance: 2,
      frozen_balance: 0
    }

    const wrapper = shallowMount(AppHeader, {
      global: {
        stubs: {
          RouterLink: true,
          BaseDialog: true,
          Transition: false
        }
      }
    })

    expect(wrapper.find('[data-testid="wallet-panel"]').exists()).toBe(false)
    await wrapper.get('[data-testid="wallet-recharge-top"]').trigger('click')
    expect(mocks.routerPush).toHaveBeenCalledWith('/purchase')

    mocks.routerPush.mockReset()
    await wrapper.get('[data-testid="wallet-control"]').trigger('mouseenter')
    expect(wrapper.get('[data-testid="wallet-panel"]').text()).toContain('$2.00')
    expect(wrapper.get('[data-testid="wallet-artwork"]').attributes('src')).toContain('wallet-fluid-blue-violet.png')
    expect(wrapper.get('[data-testid="wallet-panel"]').text()).not.toContain('总余额')

    const walletControl = wrapper.get('[data-testid="wallet-control"]')
    const walletTrigger = wrapper.get('[data-testid="wallet-trigger"]')
    await walletTrigger.trigger('click')
    await walletControl.trigger('mouseleave')
    expect(walletTrigger.attributes('aria-expanded')).toBe('true')

    await wrapper.get('[aria-label="common.close"]').trigger('click')
    expect(walletTrigger.attributes('aria-expanded')).toBe('false')

    await walletControl.trigger('mouseenter')

    await wrapper.get('[data-testid="wallet-recharge"]').trigger('click')
    expect(mocks.routerPush).toHaveBeenCalledWith('/purchase')
  })

  it('opens the QQ community QR code by default and switches to WeChat', async () => {
    const wrapper = shallowMount(AppHeader, {
      global: {
        stubs: {
          RouterLink: true,
          Teleport: true,
          Transition: true
        }
      }
    })

    const supportButton = wrapper.get('[data-testid="header-contact-support"]')
    expect(supportButton.text()).toContain('common.contactSupport')
    expect(supportButton.classes()).toContain('md:flex')
    expect(wrapper.find('[data-testid="support-dialog"]').exists()).toBe(false)

    await supportButton.trigger('click')

    expect(wrapper.get('[data-testid="support-dialog"]').text()).toContain('common.supportCommunityTitle')
    expect(wrapper.get('[data-testid="support-tab-qq"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="support-qr-image"]').attributes('src')).toBe('data:image/png;base64,qq-code')

    await wrapper.get('[data-testid="support-tab-wechat"]').trigger('click')

    expect(wrapper.get('[data-testid="support-tab-wechat"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="support-qr-image"]').attributes('src')).toBe('data:image/png;base64,wechat-code')
    await wrapper.get('[data-testid="support-close"]').trigger('click')
    expect(wrapper.find('[data-testid="support-dialog"]').exists()).toBe(false)
  })

  it('shows an empty state for an unconfigured tab', async () => {
    mocks.appStore.supportWeChatGroupQRCode = ''

    const wrapper = shallowMount(AppHeader, {
      global: {
        stubs: {
          RouterLink: true,
          Teleport: true,
          Transition: true
        }
      }
    })

    await wrapper.get('[data-testid="header-contact-support"]').trigger('click')
    await wrapper.get('[data-testid="support-tab-wechat"]').trigger('click')

    expect(wrapper.find('[data-testid="support-qr-image"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="support-qr-empty"]').text()).toContain('common.supportQRCodeEmptyTitle')
  })

  it('does not render the entry when group QR codes are not configured', () => {
    mocks.appStore.supportQQGroupQRCode = ''
    mocks.appStore.supportWeChatGroupQRCode = ''

    const wrapper = shallowMount(AppHeader, {
      global: {
        stubs: { RouterLink: true }
      }
    })

    expect(wrapper.find('[data-testid="header-contact-support"]').exists()).toBe(false)
  })
})
