import { describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  cachedPublicSettings: null as null | Record<string, unknown>,
}))

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

describe('router development preview routes', () => {
  it('registers a development-only liquid glass preview route as public', async () => {
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find((record) => record.name === 'LiquidGlassPreview')

    expect(import.meta.env.DEV).toBe(true)
    expect(route?.path).toBe('/liquid-glass-preview')
    expect(route?.meta.requiresAuth).toBe(false)
  })

  it('registers a separate readability experiment without replacing the baseline preview', async () => {
    const { default: router } = await import('@/router')
    const baselineRoute = router.getRoutes().find((record) => record.name === 'LiquidGlassPreview')
    const readabilityRoute = router
      .getRoutes()
      .find((record) => record.name === 'LiquidGlassReadabilityPreview')

    expect(import.meta.env.DEV).toBe(true)
    expect(baselineRoute?.path).toBe('/liquid-glass-preview')
    expect(readabilityRoute?.path).toBe('/liquid-glass-readability-preview')
    expect(readabilityRoute?.meta.requiresAuth).toBe(false)
  })

  it('registers a development-only purchase preview route as public', async () => {
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find((record) => record.name === 'PurchasePreview')

    expect(import.meta.env.DEV).toBe(true)
    expect(route?.path).toBe('/purchase-preview')
    expect(route?.meta.requiresAuth).toBe(false)
    expect(route?.meta.requiresPayment).toBe(false)
  })

  it('keeps the real purchase route protected', async () => {
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find((record) => record.name === 'PurchaseSubscription')

    expect(route?.path).toBe('/purchase')
    expect(route?.meta.requiresAuth).toBe(true)
    expect(route?.meta.requiresPayment).toBe(true)
    expect(route?.meta.titleKey).toBe('payment.rechargeUi.title')
  })
})
