import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RechargeMethodSelector from '../recharge/RechargeMethodSelector.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const mountSelector = (overrides: Record<string, unknown> = {}) =>
  mount(RechargeMethodSelector, {
    props: {
      selected: 'alipay',
      methods: [
        { type: 'alipay', display_name: 'Configured Alipay', fee_rate: 0, available: true },
        { type: 'wxpay', fee_rate: 0, available: false },
      ],
      ...overrides,
    },
  })

describe('RechargeMethodSelector', () => {
  it('uses the reference heading without numbering or supporting description', () => {
    const wrapper = mountSelector()

    expect(wrapper.get('#recharge-method-title').text())
      .toContain('payment.rechargeUi.selectPaymentMethod')
    expect(wrapper.get('#recharge-method-title').text()).not.toMatch(/^2\./)
    expect(wrapper.text()).not.toContain('payment.rechargeUi.paymentMethodHint')
  })

  it('uses checkout labels while showing the payment brands and assurance copy', () => {
    const wrapper = mountSelector()

    const alipay = wrapper.find('[data-testid="payment-method-alipay"]')
    const wxpay = wrapper.find('[data-testid="payment-method-wxpay"]')

    expect(alipay.text()).toContain('Configured Alipay')
    expect(alipay.text()).toContain('payment.rechargeUi.alipayBrand')
    expect(alipay.text()).toContain('payment.rechargeUi.alipayAssurance')
    expect(alipay.text()).not.toContain('payment.rechargeUi.recommended')
    expect(wxpay.text()).toContain('payment.rechargeUi.wechatPayBrand')
    expect(wxpay.text()).toContain('payment.rechargeUi.wechatPayAssurance')
    expect(wxpay.text()).toContain('payment.rechargeUi.methodUnavailable')
  })

  it('does not invent recommendations and only renders an explicit recommendation', async () => {
    const wrapper = mountSelector()

    expect(wrapper.text()).not.toContain('payment.rechargeUi.recommended')

    await wrapper.setProps({ recommendedMethod: 'alipay' })

    expect(wrapper.find('[data-testid="payment-method-alipay"]').text())
      .toContain('payment.rechargeUi.recommended')
    expect(wrapper.find('[data-testid="payment-method-wxpay"]').text())
      .not.toContain('payment.rechargeUi.recommended')
  })

  it('uses a two-column grid with selected and unselected radio indicators', () => {
    const wrapper = mountSelector()
    const grid = wrapper.get('.recharge-method-grid')
    const alipay = wrapper.find('[data-testid="payment-method-alipay"]')
    const wxpay = wrapper.find('[data-testid="payment-method-wxpay"]')

    expect(grid.classes()).toContain('sm:grid-cols-2')
    expect(grid.classes()).not.toContain('xl:grid-cols-3')
    expect(alipay.classes()).toContain('recharge-method-selected-glow')
    expect(alipay.classes()).toContain('recharge-method-card-selected')
    expect(alipay.classes()).toContain('recharge-method-selected-alipay')
    expect(wxpay.classes()).not.toContain('recharge-method-selected-glow')
    expect(alipay.find('.recharge-method-selection-indicator').classes())
      .toContain('recharge-method-selection-indicator-selected')
    expect(wxpay.find('.recharge-method-selection-indicator').classes())
      .not.toContain('recharge-method-selection-indicator-selected')
  })

  it('preserves fee display, disabled state, and selection events', async () => {
    const wrapper = mountSelector({
      selected: 'alipay_direct',
      methods: [
        { type: 'alipay_direct', fee_rate: 1.25, available: true },
        { type: 'wxpay_direct', fee_rate: 0, available: false },
      ],
    })

    expect(wrapper.get('[data-testid="payment-method-alipay_direct"]').text())
      .toContain('payment.fee 1.25%')
    expect(wrapper.get('[data-testid="payment-method-wxpay_direct"]').attributes('disabled'))
      .toBeDefined()

    await wrapper.get('[data-testid="payment-method-alipay_direct"]').trigger('click')
    await wrapper.get('[data-testid="payment-method-wxpay_direct"]').trigger('click')

    expect(wrapper.emitted('select')).toEqual([['alipay_direct']])
  })

  it('does not mislabel non-core methods when reused outside the recharge panel', () => {
    const wrapper = mountSelector({
      selected: 'stripe',
      methods: [
        { type: 'stripe', display_name: 'Configured Stripe', fee_rate: 1.2, available: true },
      ],
    })

    expect(wrapper.text()).toContain('Configured Stripe')
    expect(wrapper.text()).not.toContain('payment.rechargeUi.alipayBrand')
    expect(wrapper.text()).not.toContain('payment.rechargeUi.wechatPayBrand')
    expect(wrapper.text()).not.toContain('payment.rechargeUi.alipayAssurance')
    expect(wrapper.text()).not.toContain('payment.rechargeUi.wechatPayAssurance')
  })
})
