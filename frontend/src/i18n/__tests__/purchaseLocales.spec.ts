import { describe, expect, it } from 'vitest'
import zh from '../locales/zh'
import en from '../locales/en'

describe('purchase page locale copy', () => {
  it('keeps purchase page descriptions focused on the user action', () => {
    const descriptions = [
      zh.purchase.description,
      en.purchase.description,
    ]

    for (const description of descriptions) {
      expect(description).not.toMatch(/内嵌|iframe|embedded/i)
      expect(description).toMatch(/\S/)
    }
  })

  it('keeps the shared purchase hero and checkout vocabulary complete in every locale', () => {
    const heroKeys = ['lineOne', 'lineTwoAccent', 'securityLine'] as const
    const rechargeKeys = [
      'alipayAssurance',
      'alipayBrand',
      'creditedBalance',
      'creditedUsd',
      'oneToOneAgreement',
      'oneToOneConfigurationWarning',
      'paymentSecurityHint',
      'wechatPayAssurance',
      'wechatPayBrand',
    ] as const
    const subscriptionKeys = [
      'oneTimePurchase',
      'originalPrice',
      'pendingManualRenewal',
      'renewalTarget',
      'selectPlan',
      'selectPlanFirst',
      'selectedPlan',
      'validity',
    ] as const

    for (const messages of [zh, en]) {
      for (const key of heroKeys) expect(messages.payment.purchaseHero[key]).toMatch(/\S/)
      for (const key of rechargeKeys) expect(messages.payment.rechargeUi[key]).toMatch(/\S/)
      for (const key of subscriptionKeys) expect(messages.payment.subscriptionUi[key]).toMatch(/\S/)
    }

    expect(Object.keys(zh.payment.purchaseHero).sort()).toEqual(
      Object.keys(en.payment.purchaseHero).sort(),
    )
    expect(Object.keys(zh.payment.subscriptionUi).sort()).toEqual(
      Object.keys(en.payment.subscriptionUi).sort(),
    )
  })

  it('states the standard recharge contract as exact one-to-one API balance credit', () => {
    for (const agreement of [
      zh.payment.rechargeUi.oneToOneAgreement,
      en.payment.rechargeUi.oneToOneAgreement,
    ]) {
      expect(agreement).toMatch(/¥1\s*=\s*\$1/)
      expect(agreement).not.toMatch(/≈|exchange rate|汇率/i)
    }
  })

  it('keeps the one-time purchase label without verbose renewal or coupon explanations', () => {
    for (const messages of [zh, en]) {
      const subscriptionUi = messages.payment.subscriptionUi
      const purchaseCopy = JSON.stringify({
        purchaseHero: messages.payment.purchaseHero,
        rechargeUi: messages.payment.rechargeUi,
        subscriptionUi,
      })

      expect(subscriptionUi.oneTimePurchase).toMatch(/\S/)
      expect(subscriptionUi).not.toHaveProperty('activationRule')
      expect(subscriptionUi).not.toHaveProperty('noAutomaticRenewal')
      expect(subscriptionUi).not.toHaveProperty('manualRenewal')
      expect(subscriptionUi).not.toHaveProperty('renewalMethod')
      expect(subscriptionUi).not.toHaveProperty('nextBillingDate')
      expect(subscriptionUi).not.toHaveProperty('automaticRenewalToggle')
      expect(purchaseCopy).not.toMatch(/优惠券|coupon/i)
    }
  })
})
