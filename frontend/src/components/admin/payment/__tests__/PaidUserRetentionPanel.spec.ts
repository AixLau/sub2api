import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PaidUserRetentionPanel from '../PaidUserRetentionPanel.vue'

const routerPush = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

describe('PaidUserRetentionPanel', () => {
  beforeEach(() => routerPush.mockReset())

  it('renders the total and three mutually exclusive churn ranges', () => {
    const wrapper = mount(PaidUserRetentionPanel, {
      props: {
        stats: {
          total_paid_users: 263,
          days_7_to_14: 10,
          days_15_to_29: 11,
          days_30_plus: 1,
        },
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.get('[data-testid="total-paid-users"]').text()).toBe('263')
    expect(wrapper.get('[data-testid="paid-churn-7_14"]').text()).toContain('10')
    expect(wrapper.get('[data-testid="paid-churn-15_29"]').text()).toContain('11')
    expect(wrapper.get('[data-testid="paid-churn-30_plus"]').text()).toContain('1')
    expect(wrapper.text()).toContain('3.8%')
    expect(wrapper.text()).toContain('4.2%')
    expect(wrapper.text()).toContain('0.4%')
  })

  it('opens the matching filtered user list', async () => {
    const wrapper = mount(PaidUserRetentionPanel, {
      props: {
        stats: {
          total_paid_users: 263,
          days_7_to_14: 10,
          days_15_to_29: 11,
          days_30_plus: 1,
        },
      },
      global: { stubs: { Icon: true } },
    })

    await wrapper.get('[data-testid="paid-churn-15_29"]').trigger('click')

    expect(routerPush).toHaveBeenCalledWith({
      name: 'AdminUsers',
      query: { paid_churn: '15_29' },
    })
  })
})
