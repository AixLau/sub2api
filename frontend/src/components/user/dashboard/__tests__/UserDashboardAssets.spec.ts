import { mount, type DOMWrapper } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import UserDashboardDecorations from '../UserDashboardDecorations.vue'
import UserDashboardHero from '../UserDashboardHero.vue'
import UserDashboardQuickActions from '../UserDashboardQuickActions.vue'
import '@/styles/user-dashboard.css'

const mocks = vi.hoisted(() => ({
  refreshBatchImageAccess: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ locale: { value: 'en-US' }, t: (key: string) => key }),
}))

vi.mock('@/utils/featureFlags', () => ({
  FeatureFlags: { modelPlaza: { key: 'model_plaza_enabled' } },
  isFeatureFlagEnabled: () => false,
}))

vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({
    canUseBatchImage: { value: false },
    refreshBatchImageAccess: mocks.refreshBatchImageAccess,
  }),
}))

const RouterLinkStub = defineComponent({
  name: 'RouterLinkStub',
  props: ['to'],
  template: '<a><slot /></a>',
})

const HeroDecorationsHarness = defineComponent({
  components: { UserDashboardHero, UserDashboardDecorations },
  template: `
    <div class="user-dashboard">
      <UserDashboardHero display-name="demo" />
      <UserDashboardDecorations />
    </div>
  `,
})

function expectDecorativeImage(
  image: DOMWrapper<HTMLImageElement>,
  expectedSource: string,
  stylingHook: string,
) {
  expect(image.attributes('src')).toBe(expectedSource)
  expect(image.attributes('alt')).toBe('')
  expect(image.attributes('aria-hidden')).toBe('true')
  expect(image.attributes('draggable')).toBe('false')
  expect(Number(image.attributes('width'))).toBeGreaterThan(0)
  expect(Number(image.attributes('height'))).toBeGreaterThan(0)
  expect(image.element.matches(stylingHook) || image.element.closest(stylingHook)).toBeTruthy()
}

describe('dashboard decorative assets', () => {
  it('keeps all four transparent images out of the accessibility and pointer trees', () => {
    const heroWrapper = mount(HeroDecorationsHarness, { attachTo: document.body })
    const camera = heroWrapper.get<HTMLImageElement>('.user-dashboard__camera-image')
    const astronaut = heroWrapper.get<HTMLImageElement>('.user-dashboard__astronaut')

    const trendWrapper = mount(TokenUsageTrend, {
      attachTo: document.body,
      props: {
        trendData: [],
        loading: false,
        surface: 'playfulDashboard',
      },
      global: {
        stubs: {
          VariableWidthLineChart: true,
          LoadingSpinner: true,
        },
      },
    })
    const badge = trendWrapper.get<HTMLImageElement>('.token-trend__badge')

    const quickWrapper = mount(UserDashboardQuickActions, {
      attachTo: document.body,
      global: {
        stubs: {
          RouterLink: RouterLinkStub,
          Icon: true,
        },
      },
    })
    const bunny = quickWrapper.get<HTMLImageElement>('[data-testid="dashboard-quick-actions-bunny"] img')

    expectDecorativeImage(camera, '/assets/dashboard/camera-fun.png', '.user-dashboard__camera-image')
    expectDecorativeImage(astronaut, '/assets/dashboard/mascot-ai-astronaut.png', '.user-dashboard__astronaut')
    expectDecorativeImage(badge, '/assets/dashboard/badge-good-job.png', '.token-trend__badge')
    expectDecorativeImage(bunny, '/assets/dashboard/mascot-game-bunny.png', '.quick-actions-panel__mascot')

    heroWrapper.unmount()
    trendWrapper.unmount()
    quickWrapper.unmount()
  })
})
