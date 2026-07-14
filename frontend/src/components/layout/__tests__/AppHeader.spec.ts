import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AppHeader from '../AppHeader.vue'

const mocks = vi.hoisted(() => ({
  appStore: {
    contactInfo: 'youngPupss',
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
  copyToClipboard: vi.fn().mockResolvedValue(true)
}))

vi.mock('@/stores', () => ({
  useAppStore: () => mocks.appStore,
  useAuthStore: () => mocks.authStore,
  useOnboardingStore: () => mocks.onboardingStore
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: false,
    copyToClipboard: mocks.copyToClipboard
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ name: 'Profile', meta: {}, params: {} })
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

describe('AppHeader contact support entry', () => {
  beforeEach(() => {
    mocks.appStore.contactInfo = 'youngPupss'
    mocks.copyToClipboard.mockClear()
  })

  it('opens a beginner-friendly guide before copying contact info', async () => {
    const wrapper = shallowMount(AppHeader, {
      global: {
        stubs: {
          RouterLink: true,
          BaseDialog: {
            props: ['show', 'title'],
            template: '<div v-if="show" data-testid="support-dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>'
          }
        }
      }
    })

    const supportButton = wrapper.get('[data-testid="header-contact-support"]')
    expect(supportButton.text()).toContain('common.getSupport')
    expect(wrapper.find('[data-testid="support-dialog"]').exists()).toBe(false)

    await supportButton.trigger('click')

    expect(mocks.copyToClipboard).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="support-dialog"]').text()).toContain('common.supportDialogTitle')
    expect(wrapper.get('[data-testid="support-contact-info"]').text()).toBe('youngPupss')

    await wrapper.get('[data-testid="copy-support-contact"]').trigger('click')
    await flushPromises()

    expect(mocks.copyToClipboard).toHaveBeenCalledWith(
      'youngPupss',
      'common.supportCopiedNextStep'
    )
    expect(wrapper.get('[data-testid="support-copy-next-step"]').text()).toContain(
      'common.supportCopiedNextStep'
    )
  })

  it('does not render the entry when contact info is not configured', () => {
    mocks.appStore.contactInfo = ''

    const wrapper = shallowMount(AppHeader, {
      global: {
        stubs: { RouterLink: true }
      }
    })

    expect(wrapper.find('[data-testid="header-contact-support"]').exists()).toBe(false)
  })
})
