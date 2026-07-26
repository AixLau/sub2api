import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import HomeView from '@/views/HomeView.vue'

const { checkAuthMock, fetchPublicSettingsMock, replaceMock, authState } = vi.hoisted(() => ({
  checkAuthMock: vi.fn(),
  fetchPublicSettingsMock: vi.fn(),
  replaceMock: vi.fn(),
  authState: { isAuthenticated: false, isAdmin: false },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    replace: replaceMock,
    push: vi.fn(),
  }),
  useRoute: () => ({
    path: '/home',
    query: {},
  }),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    get isAuthenticated() {
      return authState.isAuthenticated
    },
    get isAdmin() {
      return authState.isAdmin
    },
    user: null,
    checkAuth: (...args: unknown[]) => checkAuthMock(...args),
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
    publicSettingsLoaded: true,
    fetchPublicSettings: (...args: unknown[]) => fetchPublicSettingsMock(...args),
  }),
}))

function mountHomeView() {
  return mount(HomeView, {
    global: {
      stubs: {
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
}

describe('HomeView visual baseline', () => {
  beforeEach(() => {
    checkAuthMock.mockReset()
    fetchPublicSettingsMock.mockReset()
    replaceMock.mockReset()
    authState.isAuthenticated = false
    authState.isAdmin = false
    localStorage.clear()

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({ matches: false }),
    })
  })

  it('renders the Starlink landing hero in default mode', async () => {
    const wrapper = mountHomeView()

    await flushPromises()

    const html = wrapper.html()
    const text = wrapper.text()

    expect(html).toContain('landing-shell')
    expect(html).toContain('app-shell--home')
    expect(html).toContain('hero-section')
    expect(text).toContain('Codex 与模型 API 接入，像光一样自然。')
    expect(text).toContain('开始接入')
    expect(text).toContain('Partnered with top-tier companies globally')
  })

  it('redirects authenticated users to their dashboard like the live landing', async () => {
    authState.isAuthenticated = true
    authState.isAdmin = true

    mountHomeView()
    await flushPromises()

    expect(checkAuthMock).toHaveBeenCalled()
    expect(replaceMock).toHaveBeenCalledWith('/admin/dashboard')
  })
})
