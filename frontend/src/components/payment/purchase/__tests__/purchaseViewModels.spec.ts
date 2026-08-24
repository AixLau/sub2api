import { describe, expect, it } from 'vitest'
import type { UserSubscription } from '@/types'
import type { NinePlusProduct, SubscriptionPlan } from '@/types/payment'
import {
  buildPurchaseSubscriptionOptions,
  isNinePlusSubscriptionProduct,
  isPurchaseSubscriptionRenewal,
  ninePlusPaymentAmounts,
  purchaseSubscriptionDiscountAmount,
  purchaseSubscriptionDiscountPercent,
} from '../purchaseViewModels'

function plan(overrides: Partial<SubscriptionPlan> = {}): SubscriptionPlan {
  return {
    id: 1,
    group_id: 10,
    name: 'Internal Pro',
    description: 'Internal plan description',
    price: 39,
    currency: 'CNY',
    validity_days: 30,
    validity_unit: 'day',
    features: ['Priority access'],
    for_sale: true,
    sort_order: 10,
    ...overrides,
  }
}

function ninePlus(overrides: Partial<NinePlusProduct> = {}): NinePlusProduct {
  return {
    product_id: 'np-sub',
    display_name: 'NinePlus 月度会员',
    description: '订阅产品',
    category: 'subscription',
    currency: 'CNY',
    price: 88,
    quota: 500,
    quota_unit: 'USD',
    enabled: true,
    sort_order: 10,
    ...overrides,
  }
}

describe('purchase subscription view models', () => {
  it('keeps real internal plans and available NinePlus subscriptions as tagged source payloads', () => {
    const internalPlan = plan()
    const externalProduct = ninePlus()
    const options = buildPurchaseSubscriptionOptions(
      [internalPlan],
      [
        externalProduct,
        ninePlus({ product_id: 'recharge', display_name: '余额充值 100', description: '余额商品', category: 'recharge' }),
        ninePlus({ product_id: 'sold-out', stock_count: 0 }),
        ninePlus({ product_id: 'disabled', enabled: false }),
      ],
    )

    expect(options).toHaveLength(2)
    expect(options[0]).toMatchObject({ source: 'internal', key: 'internal:1', id: 1 })
    expect(options[0].source === 'internal' && options[0].plan).toBe(internalPlan)
    expect(options[1]).toMatchObject({ source: 'nineplus', key: 'nineplus:np-sub', id: 'np-sub' })
    expect(options[1].source === 'nineplus' && options[1].product).toBe(externalProduct)
  })

  it('preserves NinePlus configured price, fee, and payment amount semantics', () => {
    expect(ninePlusPaymentAmounts(ninePlus({ price: 100, fee: 2, payment_amount: undefined }))).toEqual({
      price: 100,
      fee: 2,
      total: 102,
    })
    expect(ninePlusPaymentAmounts(ninePlus({ price: 100, fee: undefined, payment_amount: 103.25 }))).toEqual({
      price: 100,
      fee: 3.25,
      total: 103.25,
    })
  })

  it('preserves the existing lower-price-first NinePlus storefront ordering', () => {
    const options = buildPurchaseSubscriptionOptions([], [
      ninePlus({ product_id: 'standard', price: 49.9, sort_order: 2 }),
      ninePlus({ product_id: 'starter', price: 29.9, sort_order: 9 }),
    ])

    expect(options.map(option => option.id)).toEqual(['starter', 'standard'])
  })

  it('only derives discounts from a real original price above the current price', () => {
    const [discounted, notDiscounted] = buildPurchaseSubscriptionOptions([
      plan({ id: 1, price: 80, original_price: 100 }),
      plan({ id: 2, price: 100, original_price: 80 }),
    ], [])

    expect(discounted.originalPrice).toBe(100)
    expect(purchaseSubscriptionDiscountAmount(discounted)).toBe(20)
    expect(purchaseSubscriptionDiscountPercent(discounted)).toBe(20)
    expect(notDiscounted.originalPrice).toBeUndefined()
    expect(purchaseSubscriptionDiscountAmount(notDiscounted)).toBe(0)
  })

  it('uses an active same-group internal subscription as the deferred renewal signal', () => {
    const [internal, external] = buildPurchaseSubscriptionOptions(
      [plan({ group_id: 42 })],
      [ninePlus()],
    )
    const subscriptions = [{ group_id: 42, status: 'active' }] as UserSubscription[]

    expect(isPurchaseSubscriptionRenewal(internal, subscriptions)).toBe(true)
    expect(isPurchaseSubscriptionRenewal(external, subscriptions)).toBe(false)
    expect(isPurchaseSubscriptionRenewal(internal, [{ group_id: 42, status: 'expired' }] as UserSubscription[])).toBe(false)
  })

  it('recognizes the existing localized NinePlus subscription markers without classifying recharge products', () => {
    expect(isNinePlusSubscriptionProduct(ninePlus({ category: '', display_name: '年度会员' }))).toBe(true)
    expect(isNinePlusSubscriptionProduct(ninePlus({ category: '', display_name: 'Pro Membership' }))).toBe(true)
    expect(isNinePlusSubscriptionProduct(ninePlus({ category: 'recharge', display_name: '余额充值', description: 'API balance' }))).toBe(false)
  })
})
