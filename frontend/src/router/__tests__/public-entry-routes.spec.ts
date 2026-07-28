import { describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    siteName: 'Sub2API',
    backendModeEnabled: false,
    cachedPublicSettings: null,
  }),
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: () => ({
    initialized: true,
    fetchStatus: vi.fn(),
    requireAcknowledgement: vi.fn(),
  }),
}))

vi.mock('@/api/setup', () => ({
  getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

describe('Vue public entry routes', () => {
  it('always registers the landing and authentication entry points', async () => {
    const { default: router } = await import('@/router')
    const routeNames = router.getRoutes().map((route) => route.name)
    const routePaths = router.getRoutes().map((route) => route.path)

    expect(routeNames).toContain('Home')
    expect(routeNames).toContain('Login')
    expect(routeNames).toContain('Register')
    expect(routeNames).toContain('ForgotPassword')
    expect(routeNames).toContain('ResetPassword')
    expect(routePaths).toContain('/')
    expect(routePaths).toContain('/home')
    expect(routePaths).toContain('/login')
    expect(routePaths).toContain('/register')
    expect(routePaths).toContain('/forgot-password')
    expect(routePaths).toContain('/reset-password')
    expect(routePaths).toContain('/model-market')
    expect(routePaths).toContain('/services')
    expect(routePaths).toContain('/service-status')
    expect(routePaths).toContain('/faq')
  })
})
