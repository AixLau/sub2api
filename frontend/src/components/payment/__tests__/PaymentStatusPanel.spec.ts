import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const pollOrderStatus = vi.hoisted(() => vi.fn())
const cancelOrder = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const toCanvas = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({
    pollOrderStatus,
  }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
  }),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    cancelOrder,
    verifyOrder,
  },
}))

vi.mock('qrcode', () => ({
  default: {
    toCanvas,
  },
}))

import PaymentStatusPanel from '../PaymentStatusPanel.vue'
import Lens from '@/components/inspira/Lens.vue'

const orderFactory = (status: string) => ({
  id: 42,
  user_id: 9,
  amount: 88,
  pay_amount: 88,
  fee_rate: 0,
  payment_type: 'alipay',
  out_trade_no: 'sub2_20260420abcd1234',
  status,
  order_type: 'balance',
  created_at: '2026-04-20T12:00:00Z',
  expires_at: '2099-01-01T12:30:00Z',
  refund_amount: 0,
})

describe('PaymentStatusPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset()
    cancelOrder.mockReset()
    verifyOrder.mockReset()
    showError.mockReset()
    toCanvas.mockReset().mockResolvedValue(undefined)
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue('data:image/png;base64,test-qr')
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('treats RECHARGING as a successful terminal state', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('RECHARGING'))

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  describe('official Alipay embedded checkout', () => {
    const props = {
      orderId: 42,
      qrCode: '',
      payUrl: 'https://openapi.alipay.com/gateway.do?qr_pay_mode=4',
      paymentMode: 'embedded',
      expiresAt: '2099-01-01T12:30:00Z',
      paymentType: 'alipay',
      orderType: 'balance',
    }

    beforeEach(() => {
      // The global mock disables Lens; exercise the real desktop duplication path.
      const media = window.matchMedia('(pointer: coarse)')
      vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ ...media, media: query, matches: false }))
    })

    afterEach(() => {
      vi.mocked(window.matchMedia).mockRestore()
    })

    it('loads one cashier on desktop and clears feedback after document load', async () => {
      const wrapper = mount(PaymentStatusPanel, { props })
      await flushPromises()
      expect(wrapper.findAll('iframe')).toHaveLength(1)
      expect(wrapper.findComponent(Lens).exists()).toBe(false)
      expect(wrapper.get('iframe').attributes('src')).toBe(props.payUrl)
      expect(wrapper.text()).toContain('payment.qr.cashierLoading')
      expect(toCanvas).not.toHaveBeenCalled()
      await wrapper.get('iframe').trigger('load')
      expect(wrapper.get('[data-test="payment-embedded-status"]').text()).toBe('')
      await vi.advanceTimersByTimeAsync(15000)
      expect(wrapper.text()).not.toContain('payment.qr.cashierSlow')
      expect(wrapper.findAll('iframe')).toHaveLength(1)
      wrapper.unmount()
    })

    it('shows slow-load guidance without hiding or reloading the cashier, then accepts a late load', async () => {
      const wrapper = mount(PaymentStatusPanel, { props })
      const frame = wrapper.get('iframe').element
      await vi.advanceTimersByTimeAsync(14999)
      expect(wrapper.text()).toContain('payment.qr.cashierLoading')
      await vi.advanceTimersByTimeAsync(1)
      expect(wrapper.text()).toContain('payment.qr.cashierSlow')
      expect(wrapper.get('iframe').element).toBe(frame)
      expect(wrapper.text()).toContain('payment.qr.openPayWindow')
      await wrapper.get('iframe').trigger('load')
      expect(wrapper.text()).not.toContain('payment.qr.cashierSlow')
      wrapper.unmount()
    })

    it('handles a frame error and resets loading for a different payment URL', async () => {
      const wrapper = mount(PaymentStatusPanel, { props })
      const frame = wrapper.get('iframe').element
      await wrapper.get('iframe').trigger('error')
      expect(wrapper.text()).toContain('payment.qr.cashierSlow')
      await wrapper.setProps({ payUrl: props.payUrl + '&order=next' })
      expect(wrapper.text()).toContain('payment.qr.cashierLoading')
      expect(wrapper.get('iframe').element).not.toBe(frame)
      expect(wrapper.findAll('iframe')).toHaveLength(1)
      wrapper.unmount()
      expect(vi.getTimerCount()).toBe(0)
    })

    it('clears the pending load timer when the order is cancelled', async () => {
      cancelOrder.mockResolvedValue(undefined)
      const wrapper = mount(PaymentStatusPanel, { props })
      const cancel = wrapper.findAll('button').find(b => b.text().includes('payment.qr.cancelOrder'))!
      await cancel.trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('payment.qr.cancelled')
      expect(wrapper.find('iframe').exists()).toBe(false)
      expect(vi.getTimerCount()).toBe(0)
      wrapper.unmount()
    })

    it('keeps HaozPay QR images local without mounting a cashier', async () => {
      const wrapper = mount(PaymentStatusPanel, {
        props: { ...props, paymentMode: 'qrcode', providerKey: 'haozpay', payUrl: '', qrCode: 'https://pay.example.com/qr/42' },
      })
      await flushPromises()
      expect(wrapper.find('iframe').exists()).toBe(false)
      expect(wrapper.find('[data-test="payment-embedded-status"]').exists()).toBe(false)
      expect(wrapper.findComponent(Lens).exists()).toBe(true)
      expect(wrapper.findAll('[data-test="payment-qr-image"]')).toHaveLength(2)
      expect(toCanvas).toHaveBeenCalled()
      wrapper.unmount()
      expect(vi.getTimerCount()).toBe(0)
    })
  })

  it('shows reopen button in QR mode when payUrl is also available', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as Window)

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        payUrl: 'https://pay.example.com/session/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('payment.qr.openPayWindow')
    expect(wrapper.findComponent(Lens).exists()).toBe(true)
    expect(wrapper.find('[data-test="payment-qr-image"]').attributes('src')).toBe('data:image/png;base64,test-qr')

    const reopenBtn = wrapper.findAll('button').find(b => b.text().includes('payment.qr.openPayWindow'))
    expect(reopenBtn).toBeTruthy()
    await reopenBtn!.trigger('click')
    expect(openSpy).toHaveBeenCalledWith(
      'https://pay.example.com/session/42',
      'paymentPopup',
      expect.any(String),
    )

    openSpy.mockRestore()
  })

  it('uses generic QR copy for custom methods that contain built-in names', async () => {
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'card_alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.qr.scanToPay')
    expect(wrapper.text()).not.toContain('payment.qr.scanAlipay')
  })

  it('actively verifies a stuck pending order and settles it when upstream confirms payment', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({
      data: orderFactory('COMPLETED'),
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'wxpay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('actively verifies a stuck nineplus order without waiting for the backend sweep', async () => {
    pollOrderStatus.mockResolvedValue({
      ...orderFactory('PENDING'),
      payment_type: 'nineplus',
    })
    verifyOrder.mockResolvedValue({
      data: {
        ...orderFactory('COMPLETED'),
        payment_type: 'nineplus',
      },
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://9.plus/payApi/Zhifutong/pay.html?trade_no=9PTEST',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'nineplus',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('actively verifies Alipay orders routed through HaozPay', async () => {
    pollOrderStatus.mockResolvedValue({
      ...orderFactory('PENDING'),
      payment_type: 'alipay',
    })
    verifyOrder.mockResolvedValue({
      data: {
        ...orderFactory('COMPLETED'),
        payment_type: 'alipay',
        provider_key: 'haozpay',
      },
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://cashier.haozpay.com/cashier-pc?orderNo=HZHT123',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        providerKey: 'haozpay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('recovers legacy checkout state from the provider returned by order polling', async () => {
    pollOrderStatus.mockResolvedValue({
      ...orderFactory('PENDING'),
      payment_type: 'alipay',
      provider_key: 'haozpay',
    })
    verifyOrder.mockResolvedValue({
      data: {
        ...orderFactory('COMPLETED'),
        payment_type: 'alipay',
        provider_key: 'haozpay',
      },
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://cashier.haozpay.com/cashier-pc?orderNo=HZHT123',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('keeps verifying nineplus orders after the initial retry window', async () => {
    const pendingOrder = {
      ...orderFactory('PENDING'),
      payment_type: 'nineplus',
    }
    const completedOrder = {
      ...orderFactory('COMPLETED'),
      payment_type: 'nineplus',
    }
    pollOrderStatus.mockResolvedValue(pendingOrder)
    for (let i = 0; i < 6; i++) {
      verifyOrder.mockResolvedValueOnce({ data: pendingOrder })
    }
    verifyOrder.mockResolvedValueOnce({ data: completedOrder })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://9.plus/payApi/Zhifutong/pay.html?trade_no=9PTEST',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'nineplus',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    for (let i = 0; i < 6; i++) {
      await vi.advanceTimersByTimeAsync(3000)
      await flushPromises()
    }

    expect(verifyOrder).toHaveBeenCalledTimes(6)
    expect(wrapper.emitted('success')).toBeUndefined()

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledTimes(7)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('actively verifies a pending mobile Alipay precreate order', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    Object.defineProperty(window, 'location', { configurable: true, value: { assign: vi.fn() } })
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('success')).toHaveLength(1)

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('actively verifies a pending desktop Alipay order', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/desktop-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('success')).toHaveLength(1)

    wrapper.unmount()
  })

  it('keeps the QR fallback hidden until the Alipay app launch times out', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    const assign = vi.fn()
    Object.defineProperty(window, 'location', { configurable: true, value: { assign } })
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    expect(assign).toHaveBeenCalledWith(expect.stringContaining('alipays://platformapi/startapp?saId=10000007&qrcode='))
    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('payment.qr.saveQRCode')
    expect(wrapper.text()).toContain('sub2_20260420abcd1234')
    expect(toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), 'https://qr.alipay.com/dynamic-order-42', expect.any(Object))

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('does not show the QR fallback after the page enters the background', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    let hidden = false
    Object.defineProperty(window, 'location', { configurable: true, value: { assign: vi.fn() } })
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.qr.alipayContinueInApp')

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })
})
