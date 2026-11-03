import { afterEach, describe, expect, it } from 'vitest'
import { ALIPAY_CASHIER_ORIGINS, preconnectAlipayCashier } from '../alipayPreconnect'

describe('preconnectAlipayCashier', () => {
  afterEach(() => {
    document.head.querySelectorAll('link[data-alipay-preconnect]').forEach(link => link.remove())
  })

  it('adds one preconnect hint per Alipay cashier origin', () => {
    preconnectAlipayCashier()
    const links = Array.from(document.head.querySelectorAll<HTMLLinkElement>('link[rel="preconnect"][data-alipay-preconnect]'))
    expect(links.map(link => link.getAttribute('href'))).toEqual([...ALIPAY_CASHIER_ORIGINS])
    // Navigations and the cashier's scripts use no-cors connections.
    expect(links.every(link => !link.hasAttribute('crossorigin'))).toBe(true)
  })

  it('is idempotent', () => {
    preconnectAlipayCashier()
    preconnectAlipayCashier()
    expect(document.head.querySelectorAll('link[data-alipay-preconnect]')).toHaveLength(ALIPAY_CASHIER_ORIGINS.length)
  })

  it('covers the whole embedded QR redirect chain', () => {
    for (const host of ['openapi.alipay.com', 'unitradeprod.alipay.com', 'excashier.alipay.com']) {
      expect(ALIPAY_CASHIER_ORIGINS).toContain(`https://${host}`)
    }
  })
})
