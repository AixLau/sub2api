import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UserDashboardSkeleton from '../UserDashboardSkeleton.vue'

describe('UserDashboardSkeleton', () => {
  it('mirrors the dashboard data regions while leaving the always-visible hero outside', () => {
    const wrapper = mount(UserDashboardSkeleton)
    const root = wrapper.get('[data-testid="user-dashboard-skeleton"]')

    expect(root.attributes('aria-busy')).toBe('true')
    expect(wrapper.findAll('.user-dashboard-skeleton__stat')).toHaveLength(8)
    expect(wrapper.find('.user-dashboard-skeleton__filters').exists()).toBe(true)
    expect(wrapper.find('.user-dashboard-skeleton__analysis').exists()).toBe(true)
    expect(wrapper.find('.user-dashboard-skeleton__bottom').exists()).toBe(true)
    expect(wrapper.find('[data-testid="user-dashboard-skeleton-activity"]').exists()).toBe(true)
    expect(wrapper.find('.user-dashboard__hero').exists()).toBe(false)

    const regions = Array.from(root.element.children)
    const activity = wrapper.get('[data-testid="user-dashboard-skeleton-activity"]').element
    const stats = wrapper.get('.user-dashboard-skeleton__stats').element
    const filters = wrapper.get('.user-dashboard-skeleton__filters').element
    expect(regions.indexOf(stats)).toBeLessThan(regions.indexOf(activity))
    expect(regions.indexOf(activity)).toBeLessThan(regions.indexOf(filters))
  })
})
