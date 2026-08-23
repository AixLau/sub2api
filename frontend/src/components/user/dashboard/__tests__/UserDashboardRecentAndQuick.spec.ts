import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UserDashboardQuickActions from '../UserDashboardQuickActions.vue'
import UserDashboardRecentUsage from '../UserDashboardRecentUsage.vue'

const mocks = vi.hoisted(() => ({
  modelPlazaEnabled: true,
  canUseBatchImage: true,
  refreshBatchImageAccess: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/featureFlags', () => ({
  FeatureFlags: { modelPlaza: { key: 'model_plaza_enabled' } },
  isFeatureFlagEnabled: () => mocks.modelPlazaEnabled,
}))

vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({
    canUseBatchImage: {
      get value() {
        return mocks.canUseBatchImage
      },
    },
    refreshBatchImageAccess: mocks.refreshBatchImageAccess,
  }),
}))

const RouterLinkStub = defineComponent({
  name: 'RouterLinkStub',
  props: ['to'],
  template: '<a><slot /></a>',
})

const EmptyStateStub = defineComponent({
  name: 'EmptyStateStub',
  props: ['title', 'description'],
  template: '<div data-testid="empty-state-stub">{{ title }} {{ description }}</div>',
})

function mountQuickActions() {
  return mount(UserDashboardQuickActions, {
    global: {
      stubs: {
        RouterLink: RouterLinkStub,
        Icon: true,
      },
    },
  })
}

describe('UserDashboardRecentUsage', () => {
  it('renders the explicit empty state without a misleading usage route', () => {
    const wrapper = mount(UserDashboardRecentUsage, {
      props: { data: [], loading: false },
      global: {
        stubs: {
          EmptyState: EmptyStateStub,
          LoadingSpinner: true,
          AnimatedList: true,
          RouterLink: RouterLinkStub,
          Icon: true,
        },
      },
    })

    expect(wrapper.get('[data-testid="dashboard-recent-usage"]').attributes('aria-busy')).toBe('false')
    expect(wrapper.get('[data-testid="recent-usage-empty"]').text()).toContain('dashboard.noUsageRecords')
    expect(wrapper.get('[data-testid="recent-usage-empty"]').text()).toContain('dashboard.startUsingApi')
    expect(wrapper.find('[data-testid="recent-usage-view-all"]').exists()).toBe(false)
  })
})

describe('UserDashboardQuickActions', () => {
  beforeEach(() => {
    mocks.modelPlazaEnabled = true
    mocks.canUseBatchImage = true
    mocks.refreshBatchImageAccess.mockClear()
  })

  it('uses the real routes for primary and secondary actions', () => {
    const wrapper = mountQuickActions()
    const links = wrapper.findAllComponents(RouterLinkStub)
    const routes = links.map((link) => link.props('to'))

    expect(routes).toContain('/keys')
    expect(routes).toContain('/usage')
    expect(routes).toContain('/batch-image')
    expect(routes).toContain('/redeem')
    expect(routes).toContainEqual({ path: '/model-plaza', query: { embedded: '1' } })
    expect(wrapper.find('[data-testid="quick-action-model-plaza"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quick-action-batch-image"]').exists()).toBe(true)
    expect(mocks.refreshBatchImageAccess).toHaveBeenCalledTimes(1)
  })

  it('honors model-plaza and batch-image feature availability without hiding core actions', () => {
    mocks.modelPlazaEnabled = false
    mocks.canUseBatchImage = false

    const wrapper = mountQuickActions()

    expect(wrapper.find('[data-testid="quick-action-model-plaza"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quick-action-batch-image"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quick-action-keys"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quick-action-usage"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quick-action-redeem"]').exists()).toBe(true)
  })
})
