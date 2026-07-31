import { describe, expect, it } from 'vitest'

import { paymentMethodColorClass } from '../paymentMethodColors'

describe('payment method color helper', () => {
  it('maps external payment methods to provider tokens', () => {
    expect(paymentMethodColorClass('alipay')).toBe('bg-provider-alipay')
    expect(paymentMethodColorClass('wxpay')).toBe('bg-provider-wechat')
    expect(paymentMethodColorClass('stripe')).toBe('bg-provider-stripe')
    expect(paymentMethodColorClass('airwallex')).toBe('bg-provider-airwallex-selection')
  })

  it('uses a semantic neutral for unknown methods', () => {
    expect(paymentMethodColorClass('cash')).toBe('bg-content-disabled')
  })
})
