import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RechargeCheckoutPanel from '../purchase/RechargeCheckoutPanel.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

function mountPanel(overrides: Record<string, unknown> = {}) {
  return mount(RechargeCheckoutPanel, {
    props: {
      modelValue: 10,
      amounts: [10, 20, 50, 100, 200, 500, 1000],
      min: 10,
      max: 500000,
      currency: 'CNY',
      amountError: '',
      formatAmount: (value: number) => `¥${value.toFixed(2)}`,
      methods: [
        { type: 'alipay', display_name: 'Alipay', fee_rate: 0, available: true },
        { type: 'wxpay', display_name: 'WeChat Pay', fee_rate: 2, available: true },
      ],
      selectedMethod: 'alipay',
      formattedAmount: '¥10.00',
      formattedFee: '¥0.00',
      formattedTotal: '¥10.00',
      formattedEstimatedCreditedAmount: '$10.00',
      disabled: false,
      submitting: false,
      oneToOneConfigured: true,
      ...overrides,
    },
  })
}

describe('RechargeCheckoutPanel', () => {
  it('preserves the checkout test contract and forwards selection events', async () => {
    const wrapper = mountPanel()

    expect(wrapper.find('[data-testid="recharge-layout"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="recharge-controls"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="order-summary"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="purchase-energy-orb"]').attributes('src'))
      .toBe('/assets/purchase/energy-orb.webp')
    expect(wrapper.text()).not.toContain('payment.rechargeUi.amountHint')
    expect(wrapper.text()).not.toContain('payment.rechargeUi.paymentMethodHint')

    await wrapper.find('[data-testid="quick-amount-100"]').trigger('click')
    await wrapper.find('[data-testid="payment-method-wxpay"]').trigger('click')
    await wrapper.find('[data-testid="submit-recharge"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[100]])
    expect(wrapper.emitted('select-method')).toEqual([['wxpay']])
    expect(wrapper.emitted('submit')).toHaveLength(1)
  })

  it('renders only real built-in Alipay and WeChat configurations', () => {
    const wrapper = mountPanel({
      methods: [
        { type: 'alipay', display_name: 'Primary Alipay', fee_rate: 0, available: true },
        { type: 'alipay_direct', display_name: 'Direct Alipay', fee_rate: 0, available: true },
        { type: 'wxpay', display_name: 'Primary WeChat', fee_rate: 0, available: true },
        { type: 'wxpay_direct', display_name: 'Direct WeChat', fee_rate: 0.6, available: true },
        { type: 'airwallex', display_name: 'Configured Airwallex', fee_rate: 1.5, available: true },
        { type: 'stripe', display_name: 'Configured Stripe', fee_rate: 1.2, available: true },
      ],
      selectedMethod: 'alipay',
    })

    expect(wrapper.find('[data-testid="payment-method-alipay"]').text())
      .toContain('Primary Alipay')
    expect(wrapper.find('[data-testid="payment-method-wxpay"]').text())
      .toContain('Primary WeChat')
    expect(wrapper.find('[data-testid="payment-method-alipay_direct"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="payment-method-wxpay_direct"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="payment-method-airwallex"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="payment-method-stripe"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid^="payment-method-"]')).toHaveLength(2)
  })

  it('recommends the first available Alipay configuration by default', () => {
    const wrapper = mountPanel()

    expect(wrapper.find('[data-testid="payment-method-alipay"]').text())
      .toContain('payment.rechargeUi.recommended')
    expect(wrapper.find('[data-testid="payment-method-wxpay"]').text())
      .not.toContain('payment.rechargeUi.recommended')
  })

  it('blocks submission itself when the backend multiplier is not confirmed as 1', async () => {
    const wrapper = mountPanel({
      oneToOneConfigured: false,
      configurationWarning: 'Recharge multiplier must be 1.',
    })
    const submit = wrapper.find('[data-testid="submit-recharge"]')

    expect(submit.attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="recharge-one-to-one-agreement"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="recharge-configuration-warning"]').exists()).toBe(true)

    await submit.trigger('click')

    expect(wrapper.emitted('submit')).toBeUndefined()
  })

  it('shows a semantic empty state and disables checkout when no core method exists', () => {
    const wrapper = mountPanel({
      methods: [
        { type: 'stripe', display_name: 'Configured Stripe', fee_rate: 1.2, available: true },
      ],
      selectedMethod: 'stripe',
    })

    expect(wrapper.find('[data-testid="recharge-method-empty"]').text()).toContain('payment.notAvailable')
    expect(wrapper.findAll('[data-testid^="payment-method-"]')).toHaveLength(0)
    expect(wrapper.find('[data-testid="submit-recharge"]').attributes('disabled')).toBeDefined()
  })
})
