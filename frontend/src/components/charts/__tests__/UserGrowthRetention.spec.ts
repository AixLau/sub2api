import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserGrowthRetention from '../UserGrowthRetention.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en-US' },
      t: (key: string, params?: Record<string, unknown>) => params
        ? `${key}:${Object.values(params).join(',')}`
        : key
    })
  }
})

vi.mock('vue-chartjs', () => ({
  Chart: {
    name: 'Chart',
    props: ['data', 'options', 'type'],
    template: '<div data-testid="conversion-chart" />'
  }
}))

const createCohort = (
  date: string,
  registrations: number,
  paidUsers: number,
  repeatBuyers: number,
  activeUsers: number,
  rechargeAmount: number
) => ({
  date,
  registrations,
  active_users: activeUsers,
  d1_retained: 0,
  d7_retained: 0,
  d30_retained: 0,
  paid_users: paidUsers,
  repeat_buyers: repeatBuyers,
  recharge_amount: rechargeAmount,
  d1_rate: 0,
  d7_rate: 0,
  d30_rate: 0,
  paid_rate: registrations > 0 ? paidUsers * 100 / registrations : null,
  repeat_buy_rate: paidUsers > 0 ? repeatBuyers * 100 / paidUsers : null
})

const cohorts = Array.from({ length: 16 }, (_, index) => createCohort(
  `2026-06-${String(index + 1).padStart(2, '0')}`,
  100,
  index < 7 ? 10 : 20,
  index < 7 ? 2 : 8,
  index < 7 ? 5 : 12,
  index < 7 ? 50 : 120
))

const mountComponent = (items = cohorts) => mount(UserGrowthRetention, {
  props: {
    cohorts: items,
    summary: {
      d1_rate: null,
      d7_rate: null,
      d30_rate: null,
      paid_rate: 15,
      repeat_buy_rate: 33.3
    }
  },
  global: {
    stubs: {
      LoadingSpinner: true,
      Select: {
        name: 'Select',
        props: ['modelValue', 'options'],
        template: '<div />'
      }
    }
  }
})

describe('UserGrowthRetention', () => {
  it('defaults to a 7-day range and offers it as the minimum option', () => {
    const wrapper = mountComponent()
    const select = wrapper.findComponent({ name: 'Select' })

    expect(select.props('modelValue')).toBe(7)
    expect(select.props('options').map((option: { value: number }) => option.value)).toEqual([7, 30, 60, 90, 180])
  })

  it('presents the registration, recharge, and repeat-purchase funnel first', () => {
    const wrapper = mountComponent()

    expect(wrapper.find('[data-testid="registration-stage"]').text()).toContain('1,600')
    expect(wrapper.find('[data-testid="recharge-stage"]').text()).toContain('250')
    expect(wrapper.find('[data-testid="repeat-stage"]').text()).toContain('86')
    expect(wrapper.find('[data-testid="registration-loss"]').text()).toContain('1,350')
    expect(wrapper.find('[data-testid="primary-loss-alert"]').exists()).toBe(false)
    expect(wrapper.find('table').exists()).toBe(false)
  })

  it('shows registrations, recharge users, recharge amount, and API users', () => {
    const wrapper = mountComponent()
    const chart = wrapper.findComponent({ name: 'Chart' })

    expect(chart.props('data').datasets).toHaveLength(4)
    expect(chart.props('data').datasets.map((dataset: { label: string }) => dataset.label)).toEqual([
      'admin.dashboard.registrations',
      'admin.dashboard.rechargedUsers',
      'admin.dashboard.rechargeAmount',
      'admin.dashboard.apiActiveUsers'
    ])
  })

  it('removes the needs-attention panel and keeps recent cohorts in the trend', () => {
    const immature = cohorts.map((cohort) => ({ ...cohort, paid_rate: null, repeat_buy_rate: null }))
    const wrapper = mountComponent(immature)

    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.dashboard.needsAttention')
    expect(wrapper.find('[data-testid="conversion-chart"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('admin.dashboard.noMatureCohorts')
  })

  it('emits a selected time range without exposing calculation details', async () => {
    const wrapper = mountComponent()
    const select = wrapper.findComponent({ name: 'Select' })

    expect(wrapper.text()).not.toContain('30-day observation')
    await select.vm.$emit('update:modelValue', 90)
    expect(wrapper.emitted('range-change')).toEqual([[90]])
  })
})
