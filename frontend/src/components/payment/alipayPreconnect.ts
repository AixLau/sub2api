/**
 * Origins the official Alipay embedded QR (page.pay, qr_pay_mode=4) walks through:
 * gateway -> trade dispatcher -> cashier, plus the static host serving its scripts.
 * Warming these connections while the user is still on the recharge form removes
 * several DNS/TLS round trips from the time until the QR appears.
 */
export const ALIPAY_CASHIER_ORIGINS = [
  'https://openapi.alipay.com',
  'https://unitradeprod.alipay.com',
  'https://excashier.alipay.com',
  'https://a.alipayobjects.com',
] as const

const MARKER = 'data-alipay-preconnect'

/** Adds <link rel="preconnect"> hints once per document; safe to call repeatedly. */
export function preconnectAlipayCashier(doc: Document | undefined = typeof document === 'undefined' ? undefined : document): void {
  if (!doc?.head) return
  for (const origin of ALIPAY_CASHIER_ORIGINS) {
    if (doc.head.querySelector(`link[${MARKER}="${origin}"]`)) continue
    const link = doc.createElement('link')
    link.rel = 'preconnect'
    link.href = origin
    link.setAttribute(MARKER, origin)
    doc.head.appendChild(link)
  }
}
