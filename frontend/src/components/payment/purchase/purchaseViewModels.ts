import type { UserSubscription } from '@/types'
import type { NinePlusProduct, SubscriptionPlan } from '@/types/payment'

export type PurchaseSubscriptionOption =
  | InternalSubscriptionOption
  | NinePlusSubscriptionOption

interface PurchaseSubscriptionOptionBase {
  key: string
  title: string
  description: string
  currency: string
  price: number
  originalPrice?: number
  sortOrder: number
}

export interface InternalSubscriptionOption extends PurchaseSubscriptionOptionBase {
  source: 'internal'
  id: number
  groupId: number
  plan: SubscriptionPlan
}

export interface NinePlusSubscriptionOption extends PurchaseSubscriptionOptionBase {
  source: 'nineplus'
  id: string
  product: NinePlusProduct
}

export interface NinePlusPaymentAmounts {
  price: number
  fee: number
  total: number
}

export interface CurrentSubscriptionSummary {
  planName: string
  platform: string
  remainingText: string
  pendingCount: number
  pendingDays: number
}

function finiteAmount(value: number | null | undefined): number {
  return Number.isFinite(value) ? Math.max(0, Number(value)) : 0
}

function roundCurrencyAmount(value: number): number {
  return Math.round(value * 100) / 100
}

function validOriginalPrice(originalPrice: number | undefined, price: number): number | undefined {
  const normalized = finiteAmount(originalPrice)
  return normalized > price ? normalized : undefined
}

export function purchaseSubscriptionOptionKey(
  source: PurchaseSubscriptionOption['source'],
  id: string | number,
): string {
  return `${source}:${id}`
}

export function isNinePlusProductInStock(product: NinePlusProduct): boolean {
  return product.stock_count == null || product.stock_count > 0
}

/**
 * NinePlus does not expose a dedicated subscription boolean. Keep the existing
 * checkout classification in one place so UI refactors cannot silently drop
 * externally configured subscription products.
 */
export function isNinePlusSubscriptionProduct(product: NinePlusProduct): boolean {
  const text = [product.category, product.badge, product.display_name, product.description]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()

  return [
    '套餐',
    '月包',
    '月卡',
    '年包',
    '年卡',
    '会员',
    '畅用',
    '订阅',
    'subscription',
    'membership',
  ].some(keyword => text.includes(keyword))
}

export function ninePlusSubscriptionTitle(product: NinePlusProduct): string {
  return product.display_name
    .replace(/[：:]\s*\d+(?:\.\d+)?\s*元\/月[，,]?\s*(?:月包|月卡)?\s*包含\s*\d+\s*额度.*$/, '')
    .trim() || product.display_name
}

export function ninePlusPaymentAmounts(product: NinePlusProduct): NinePlusPaymentAmounts {
  const price = roundCurrencyAmount(finiteAmount(product.price))
  const configuredFee = finiteAmount(product.fee)
  const configuredTotal = finiteAmount(product.payment_amount)
  const fee = configuredFee > 0
    ? roundCurrencyAmount(configuredFee)
    : configuredTotal > price
      ? roundCurrencyAmount(configuredTotal - price)
      : 0
  const total = configuredTotal > 0
    ? roundCurrencyAmount(configuredTotal)
    : roundCurrencyAmount(price + fee)

  return { price, fee, total }
}

export function buildInternalSubscriptionOption(plan: SubscriptionPlan): InternalSubscriptionOption {
  const price = finiteAmount(plan.price)
  return {
    source: 'internal',
    key: purchaseSubscriptionOptionKey('internal', plan.id),
    id: plan.id,
    groupId: plan.group_id,
    title: plan.name,
    description: plan.description || '',
    currency: plan.currency || 'CNY',
    price,
    originalPrice: validOriginalPrice(plan.original_price, price),
    sortOrder: plan.sort_order || 0,
    plan,
  }
}

export function buildNinePlusSubscriptionOption(product: NinePlusProduct): NinePlusSubscriptionOption {
  const amounts = ninePlusPaymentAmounts(product)
  return {
    source: 'nineplus',
    key: purchaseSubscriptionOptionKey('nineplus', product.product_id),
    id: product.product_id,
    title: ninePlusSubscriptionTitle(product),
    description: product.description || '',
    currency: product.currency || 'CNY',
    price: amounts.price,
    originalPrice: validOriginalPrice(product.original_price, amounts.price),
    sortOrder: product.sort_order || 0,
    product,
  }
}

export function buildPurchaseSubscriptionOptions(
  plans: SubscriptionPlan[],
  ninePlusProducts: NinePlusProduct[],
): PurchaseSubscriptionOption[] {
  const internal = plans
    .map(buildInternalSubscriptionOption)
    .sort((a, b) => a.sortOrder - b.sortOrder || a.price - b.price || a.id - b.id)
  const external = ninePlusProducts
    .filter(product => product.enabled && isNinePlusProductInStock(product))
    .filter(isNinePlusSubscriptionProduct)
    .map(buildNinePlusSubscriptionOption)
    // Preserve the established storefront ordering: lower-priced external
    // products first, then the provider's sort_order for price ties.
    .sort((a, b) => a.price - b.price || a.sortOrder - b.sortOrder || a.id.localeCompare(b.id))

  return [...internal, ...external]
}

export function findPurchaseSubscriptionOption(
  options: PurchaseSubscriptionOption[],
  key: string,
): PurchaseSubscriptionOption | null {
  return options.find(option => option.key === key) ?? null
}

export function purchaseSubscriptionDiscountAmount(option: PurchaseSubscriptionOption): number {
  if (option.originalPrice == null) return 0
  return roundCurrencyAmount(option.originalPrice - option.price)
}

export function purchaseSubscriptionDiscountPercent(option: PurchaseSubscriptionOption): number {
  if (option.originalPrice == null || option.originalPrice <= 0) return 0
  return Math.round((purchaseSubscriptionDiscountAmount(option) / option.originalPrice) * 100)
}

export function isPurchaseSubscriptionRenewal(
  option: PurchaseSubscriptionOption | null,
  activeSubscriptions: UserSubscription[],
): boolean {
  if (!option || option.source !== 'internal') return false
  return activeSubscriptions.some(subscription =>
    subscription.group_id === option.groupId && subscription.status === 'active'
  )
}

export function formatNinePlusQuota(product: NinePlusProduct): string {
  const quota = finiteAmount(product.quota)
  if (quota <= 0) return ''
  const unit = (product.quota_unit || '').trim()
  const normalizedUnit = unit.toUpperCase()
  if (normalizedUnit === 'USD' || unit === '$') return `$${quota.toFixed(2)}`
  if (normalizedUnit === 'CNY' || unit === '¥') return `¥${quota.toFixed(2)}`
  return unit ? `${quota} ${unit}` : quota.toFixed(2)
}
