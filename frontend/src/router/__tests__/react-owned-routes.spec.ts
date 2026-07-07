import { afterEach, describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  cachedPublicSettings: null as null | Record<string, unknown>,
}))

const originalLocation = window.location

afterEach(() => {
  vi.unstubAllEnvs()
  vi.resetModules()
  authStore.checkAuth.mockClear()
  authStore.isAuthenticated = false
  authStore.isAdmin = false
  authStore.isSimpleMode = false
  authStore.hasPendingAuthSession = false
  appStore.backendModeEnabled = false
  appStore.cachedPublicSettings = null
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: originalLocation,
  })
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    customMenuItems: [],
  }),
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

describe('React-owned public routes', () => {
  it('keeps Vue auth entry routes by default for single-service deployments', async () => {
    vi.resetModules()
    const { default: router, reactLandingRoutesEnabled } = await import('@/router')
    const routeNames = router.getRoutes().map((route) => route.name)
    const routePaths = router.getRoutes().map((route) => route.path)

    expect(reactLandingRoutesEnabled).toBe(false)
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
  })

  it('leaves the branded landing and auth entry paths unregistered in the Vue router', async () => {
    vi.stubEnv('VITE_REACT_LANDING_ROUTES', 'true')
    vi.resetModules()
    const { default: router, reactLandingRoutesEnabled } = await import('@/router')
    const routeNames = router.getRoutes().map((route) => route.name)
    const routePaths = router.getRoutes().map((route) => route.path)

    expect(reactLandingRoutesEnabled).toBe(true)
    expect(routeNames).not.toContain('Home')
    expect(routeNames).not.toContain('Login')
    expect(routeNames).not.toContain('Register')
    expect(routeNames).not.toContain('ForgotPassword')
    expect(routeNames).not.toContain('ResetPassword')
    expect(routePaths).not.toContain('/')
    expect(routePaths).not.toContain('/home')
    expect(routePaths).not.toContain('/login')
    expect(routePaths).not.toContain('/register')
    expect(routePaths).not.toContain('/forgot-password')
    expect(routePaths).not.toContain('/reset-password')
  })

  it('keeps the Sub2API console routes on their original paths', async () => {
    vi.stubEnv('VITE_REACT_LANDING_ROUTES', 'true')
    vi.resetModules()
    const { default: router } = await import('@/router')
    const routePaths = router.getRoutes().map((route) => route.path)

    expect(routePaths).toContain('/dashboard')
    expect(routePaths).toContain('/keys')
    expect(routePaths).toContain('/admin/dashboard')
    expect(routePaths).toContain('/auth/callback')
    expect(routePaths).toContain('/email-verify')
  })

  it('builds React login URLs with the intended console redirect target', async () => {
    vi.stubEnv('VITE_REACT_LANDING_ROUTES', 'true')
    vi.resetModules()
    const routerModule = await import('@/router')
    const buildReactLoginRedirectUrl = (
      routerModule as {
        buildReactLoginRedirectUrl?: (redirectTarget: string) => string
      }
    ).buildReactLoginRedirectUrl

    expect(buildReactLoginRedirectUrl?.('/dashboard')).toBe('/login?redirect=%2Fdashboard')
    expect(buildReactLoginRedirectUrl?.('/admin/users?tab=active')).toBe(
      '/login?redirect=%2Fadmin%2Fusers%3Ftab%3Dactive',
    )
  })
})
