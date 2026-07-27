import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RechargeMethodSelector from '../recharge/RechargeMethodSelector.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const mountSelector = () =>
  mount(RechargeMethodSelector, {
    props: {
      selected: 'alipay',
      methods: [
        { type: 'alipay', fee_rate: 0, available: true },
        { type: 'wxpay', fee_rate: 0, available: false },
      ],
    },
  })

describe('RechargeMethodSelector', () => {
  it('keeps available method cards concise while preserving unavailable reasons', () => {
    const wrapper = mountSelector()

    const alipay = wrapper.find('[data-testid="payment-method-alipay"]')
    const wxpay = wrapper.find('[data-testid="payment-method-wxpay"]')

    expect(alipay.text()).toContain('payment.methods.alipay')
    expect(alipay.text()).toContain('payment.rechargeUi.recommended')
    expect(alipay.text()).not.toContain('payment.rechargeUi.instantArrival')
    expect(wxpay.text()).toContain('payment.rechargeUi.methodUnavailable')
  })

  it('adds the teal glow and payment brand border classes to the selected method', () => {
    const wrapper = mountSelector()
    const alipay = wrapper.find('[data-testid="payment-method-alipay"]')
    const wxpay = wrapper.find('[data-testid="payment-method-wxpay"]')

    expect(alipay.classes()).toContain('recharge-method-selected-glow')
    expect(alipay.classes()).toContain('recharge-method-selected-alipay')
    expect(wxpay.classes()).not.toContain('recharge-method-selected-glow')
  })
})
