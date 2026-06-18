import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import HomeView from '@/views/HomeView.vue'

const { checkAuthMock, fetchPublicSettingsMock } = vi.hoisted(() => ({
  checkAuthMock: vi.fn(),
  fetchPublicSettingsMock: vi.fn(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const messages: Record<string, string> = {
          'home.footer.allRightsReserved': 'All rights reserved.',
        }
        return messages[key] ?? key
      },
    }),
  }
})

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    isAuthenticated: false,
    isAdmin: false,
    user: null,
    checkAuth: (...args: any[]) => checkAuthMock(...args),
  }),
  useAppStore: () => ({
    cachedPublicSettings: {
      site_name: 'Sub2API',
      site_logo: '',
      site_subtitle: 'AI API Gateway Platform',
      doc_url: '',
      home_content: '',
    },
    siteName: 'Sub2API',
    siteLogo: '',
    docUrl: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: (...args: any[]) => fetchPublicSettingsMock(...args),
  }),
}))

describe('HomeView visual baseline', () => {
  beforeEach(() => {
    checkAuthMock.mockReset()
    fetchPublicSettingsMock.mockReset()
    localStorage.clear()

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
  })

  it('renders the restored white hero with blue CTA in default mode', async () => {
    const wrapper = mount(HomeView, {
      global: {
        stubs: {
          RouterLink: { template: '<a><slot /></a>' },
          LocaleSwitcher: true,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const html = wrapper.html()
    const text = wrapper.text()

    expect(html).toContain('bg-white')
    expect(text).toContain('统一接入')
    expect(text).toContain('所有 AI 模型')
    expect(text).toContain('免费开始')
  })
})
