import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import type { UserSubscription } from '@/types'
import type { NinePlusProduct, SubscriptionPlan } from '@/types/payment'
import SubscriptionCheckoutPanel from '../SubscriptionCheckoutPanel.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      payment: {
        selectPlan: 'Select a subscription plan',
        noPlans: 'No subscription plans available',
        fee: 'Fee',
        actualPay: 'Amount due',
        packagePrice: 'Plan price',
        subscribeNow: 'Subscribe now',
        renewNow: 'Renew now',
        paymentMethod: 'Payment method',
        days: 'days',
        weeks: 'weeks',
        months: 'months',
        years: 'years',
        perMonth: 'month',
        methods: {
          alipay: 'Alipay',
          nineplus: 'NinePlus',
        },
        planCard: {
          quotaUnit: 'quota',
          validitySuffix: ' valid',
        },
        rechargeUi: {
          orderSummary: 'Order summary',
          subscriptionHint: 'Select a plan; the list remains available.',
          selectPaymentMethod: 'Select payment method',
          paymentMethodHint: 'Use an available checkout method.',
          recommended: 'Recommended',
          methodUnavailable: 'Unavailable',
        },
        subscriptionUi: {
          selectedPlan: 'Subscription plan',
          validity: 'Service validity',
          originalPrice: 'Original price',
          planDiscount: 'Plan discount',
          activationRule: 'Activation',
          immediateActivation: 'Activates immediately after payment',
          deferredActivation: 'Starts after the current plan ends',
          oneTimePurchase: 'One-time purchase',
          noAutomaticRenewal: 'No automatic charges',
          renewalMethod: 'Renewal',
          manualRenewal: 'Renew manually when needed',
          selectPlanFirst: 'Select a plan first',
          renewalTarget: 'Renewal group',
        },
      },
    },
  },
})

function plan(id: number, overrides: Partial<SubscriptionPlan> = {}): SubscriptionPlan {
  return {
    id,
    group_id: id * 10,
    group_platform: 'openai',
    name: `Plan ${id}`,
    description: `Plan ${id} description`,
    price: id * 10,
    currency: 'CNY',
    validity_days: 30,
    validity_unit: 'day',
    features: [`Feature ${id}`],
    monthly_limit_usd: id * 100,
    for_sale: true,
    sort_order: id,
    ...overrides,
  }
}

function ninePlus(overrides: Partial<NinePlusProduct> = {}): NinePlusProduct {
  return {
    product_id: 'np-month',
    display_name: 'NinePlus Monthly Membership',
    description: 'External subscription product',
    category: 'subscription',
    currency: 'CNY',
    price: 88,
    fee: 2,
    payment_amount: 90,
    original_price: 100,
    quota: 500,
    quota_unit: 'USD',
    enabled: true,
    sort_order: 1,
    ...overrides,
  }
}

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(SubscriptionCheckoutPanel, {
    props: {
      plans: [plan(1), plan(2), plan(3)],
      methods: [
        { type: 'alipay', fee_rate: 0, available: true },
      ],
      selectedMethod: 'alipay',
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('SubscriptionCheckoutPanel', () => {
  it('uses a compact three-to-four-card grid and narrow desktop summary rail', () => {
    const wrapper = mountPanel({ selectedOptionKey: 'internal:1', canSubmit: true })
    const controls = wrapper.get('[data-testid="subscription-controls"]')
    const layoutGrid = controls.element.parentElement

    expect(layoutGrid?.classList).toContain('lg:grid-cols-[minmax(0,1fr)_300px]')
    expect(controls.classes()).toContain('space-y-3')
    expect(wrapper.get('[data-testid="subscription-plan-list"]').classes()).toEqual(expect.arrayContaining([
      'gap-3',
      'lg:grid-cols-3',
      '2xl:grid-cols-4',
    ]))
    expect(wrapper.get('[data-testid="subscription-plan-1"]').classes()).toContain('!min-h-[11rem]')
    expect(wrapper.find('[data-testid="subscription-plan-action"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Select a plan; the list remains available.')
    expect(wrapper.get('[data-testid="order-summary"]').classes()).toContain('p-4')
    expect(wrapper.get('[data-testid="subscription-summary-total"]').classes()).toContain('text-2xl')
  })

  it('keeps every checkout plan visible after selection and exposes an explicit radio state', async () => {
    const wrapper = mountPanel({
      selectedOptionKey: 'internal:2',
      paymentAmount: 20,
      totalAmount: 20,
      paymentCurrency: 'CNY',
      canSubmit: true,
    })

    expect(wrapper.get('[data-testid="subscription-plan-list"]').attributes('role')).toBe('radiogroup')
    expect(wrapper.findAll('[data-testid^="subscription-plan-"][role="radio"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="subscription-plan-2"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="subscription-plan-1"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[data-testid="subscription-summary-plan"]').text()).toBe('Plan 2')

    await wrapper.get('[data-testid="subscription-plan-1"]').trigger('click')

    expect(wrapper.findAll('[data-testid^="subscription-plan-"][role="radio"]')).toHaveLength(3)
    expect(wrapper.emitted('select-option')?.[0]?.[0]).toMatchObject({ source: 'internal', id: 1 })
  })

  it('highlights the renewal group without rendering an activation explanation block', () => {
    const activeSubscriptions = [{ group_id: 20, status: 'active' }] as UserSubscription[]
    const wrapper = mountPanel({
      selectedOptionKey: 'internal:2',
      renewalGroupId: 20,
      activeSubscriptions,
      canSubmit: true,
    })

    expect(wrapper.get('[data-testid="subscription-renewal-target"]').text()).toBe('payment.subscriptionUi.renewalTarget')
    expect(wrapper.find('[data-testid="subscription-activation-rule"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="submit-subscription"]').text()).toBe('payment.renewNow')
  })

  it('omits renewal explanations, coupons, and recurring-payment controls', () => {
    const wrapper = mountPanel({ selectedOptionKey: 'internal:1', canSubmit: true })
    const text = wrapper.text().toLowerCase()

    expect(wrapper.find('[data-testid="subscription-no-automatic-renewal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="subscription-manual-renewal"]').exists()).toBe(false)
    expect(text).not.toContain('coupon')
    expect(text).not.toContain('next charge')
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid*="coupon"]').exists()).toBe(false)
  })

  it('keeps original and payable prices without a separate discount row', () => {
    const wrapper = mountPanel({
      plans: [plan(1, { price: 80, original_price: 100 })],
      selectedOptionKey: 'internal:1',
      paymentAmount: 80,
      originalAmount: 100,
      discountAmount: 20,
      totalAmount: 80,
      canSubmit: true,
    })

    expect(wrapper.get('[data-testid="subscription-summary-price"]').text()).toContain('80')
    expect(wrapper.get('[data-testid="order-summary"]').text()).toContain('100')
    expect(wrapper.get('[data-testid="subscription-summary-total"]').text()).toContain('80')
    expect(wrapper.find('[data-testid="subscription-summary-discount"]').exists()).toBe(false)
  })

  it('keeps the current subscription and queued manual renewals in a compact status card', () => {
    const wrapper = mountPanel({
      currentSubscription: {
        planName: 'Current Pro',
        platform: 'OpenAI',
        remainingText: '12 days left',
        pendingCount: 2,
        pendingDays: 60,
      },
    })

    expect(wrapper.get('[data-testid="current-subscription-name"]').text()).toBe('Current Pro')
    expect(wrapper.get('[data-testid="current-subscription-platform"]').text()).toBe('OpenAI')
    expect(wrapper.get('[data-testid="current-subscription-remaining"]').text()).toBe('12 days left')
    expect(wrapper.find('[data-testid="current-subscription-pending-renewals"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="current-subscription-card"]').classes()).toEqual(expect.arrayContaining(['px-4', 'py-3']))
  })

  it('renders only the payment methods supplied by checkout and forwards selection', async () => {
    const wrapper = mountPanel({
      selectedOptionKey: 'internal:1',
      methods: [
        { type: 'alipay', fee_rate: 0, available: true },
        { type: 'stripe', display_name: 'Corporate Stripe', fee_rate: 1, available: true },
      ],
    })

    expect(wrapper.find('[data-testid="payment-method-alipay"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="payment-method-stripe"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="payment-method-wxpay"]').exists()).toBe(false)

    await wrapper.get('[data-testid="payment-method-stripe"]').trigger('click')
    expect(wrapper.emitted('select-method')).toEqual([['stripe']])
  })

  it('keeps the exact NinePlus product in the selection and submission payload', async () => {
    const product = ninePlus()
    const wrapper = mountPanel({
      plans: [],
      ninePlusProducts: [product],
      selectedOptionKey: 'nineplus:np-month',
      methods: [{ type: 'nineplus', fee_rate: 0, available: true }],
      selectedMethod: 'nineplus',
      canSubmit: true,
    })

    expect(wrapper.get('[data-testid="nineplus-subscription-product-np-month"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="nineplus-subscription-price"]').text()).toContain('88')
    expect(wrapper.get('[data-testid="subscription-summary-fee"]').text()).toContain('2')
    expect(wrapper.get('[data-testid="subscription-summary-total"]').text()).toContain('90')

    await wrapper.get('[data-testid="submit-subscription"]').trigger('click')
    const submitted = wrapper.emitted('submit')?.[0]?.[0]
    expect(submitted).toMatchObject({ source: 'nineplus', id: 'np-month' })
    expect(submitted && 'product' in submitted ? submitted.product : null).toEqual(product)
  })

  it('shows an empty state and disables submission until a plan is selected', () => {
    const wrapper = mountPanel({ plans: [], ninePlusProducts: [], methods: [] })

    expect(wrapper.get('[data-testid="subscription-empty-state"]').text()).toContain('payment.noPlans')
    expect(wrapper.get('[data-testid="subscription-select-plan-first"]').text()).toBe('payment.subscriptionUi.selectPlanFirst')
    expect(wrapper.get('[data-testid="submit-subscription"]').attributes()).toHaveProperty('disabled')
  })
})
