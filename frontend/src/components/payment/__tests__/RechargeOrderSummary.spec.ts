import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RechargeOrderSummary from '../recharge/RechargeOrderSummary.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

function mountSummary(overrides: Record<string, unknown> = {}) {
  return mount(RechargeOrderSummary, {
    props: {
      formattedAmount: '¥100.00',
      formattedFee: '¥2.00',
      formattedTotal: '¥102.00',
      formattedEstimatedCreditedAmount: '$100.00',
      disabled: false,
      submitting: false,
      ...overrides,
    },
  })
}

describe('RechargeOrderSummary', () => {
  it.each([
    ['¥10.00', '$10.00'],
    ['¥100.00', '$100.00'],
  ])('shows the credited balance for %s without an extra exchange-rate row', (formattedAmount, formattedCredited) => {
    const wrapper = mountSummary({
      formattedAmount,
      formattedTotal: formattedAmount,
      formattedEstimatedCreditedAmount: formattedCredited,
      formattedFee: '¥0.00',
    })

    expect(wrapper.find('[data-testid="estimated-credited-highlight"]').text())
      .toContain(formattedCredited)
    expect(wrapper.find('[data-testid="recharge-one-to-one-agreement"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('¥1 = $1')
  })

  it('keeps the credited balance independent from the gateway fee', () => {
    const wrapper = mountSummary()
    const text = wrapper.find('[data-testid="order-summary"]').text()

    expect(text).toContain('¥100.00')
    expect(text).toContain('¥2.00')
    expect(text).toContain('¥102.00')
    expect(wrapper.find('[data-testid="estimated-credited-highlight"]').text()).toContain('$100.00')
    expect(text).not.toContain('≈')
  })

  it('never claims 1:1 when configuration is not confirmed and exposes the blocking warning', () => {
    const wrapper = mountSummary({
      configurationWarning: 'Recharge multiplier must be 1.',
      disabled: true,
    })

    expect(wrapper.find('[data-testid="recharge-one-to-one-agreement"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="recharge-configuration-warning"]').text())
      .toContain('Recharge multiplier must be 1.')
    expect(wrapper.find('[data-testid="submit-recharge"]').attributes('disabled')).toBeDefined()
  })

  it('contains neither a coupon entry nor automatic-renewal content', () => {
    const text = mountSummary().text()

    expect(text).not.toMatch(/优惠券|coupon/i)
    expect(text).not.toMatch(/自动续费|automatic renewal/i)
  })

  it('emits submit and changes to retry copy after a failed attempt', async () => {
    const wrapper = mountSummary({ hasSubmitted: true, errorMessage: 'failed' })

    expect(wrapper.find('[data-testid="submit-recharge"]').text())
      .toContain('payment.rechargeUi.retryRecharge')

    await wrapper.find('[data-testid="submit-recharge"]').trigger('click')

    expect(wrapper.emitted('submit')).toHaveLength(1)
  })
})
