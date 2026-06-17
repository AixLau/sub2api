import type { UserSubscription } from '@/types'

const ONE_DAY_MS = 24 * 60 * 60 * 1000
const ONE_WEEK_MS = 7 * ONE_DAY_MS
const THIRTY_DAYS_MS = 30 * ONE_DAY_MS

export interface RemainingDurationParts {
  days: number
  hours: number
  minutes: number
}

export type SubscriptionQuotaWindow = 'daily' | 'weekly' | 'monthly'

export interface SubscriptionQuotaBoundary {
  at: Date
  kind: 'reset' | 'expiry'
}

export function isOneTimeDailyQuota(
  subscription: Pick<UserSubscription, 'starts_at' | 'expires_at'>
): boolean {
  if (!subscription.starts_at || !subscription.expires_at) return false

  const startsAt = new Date(subscription.starts_at).getTime()
  const expiresAt = new Date(subscription.expires_at).getTime()

  if (!Number.isFinite(startsAt) || !Number.isFinite(expiresAt)) return false

  return expiresAt <= startsAt + ONE_DAY_MS
}

export function getRemainingDurationParts(
  targetAt: Date | string,
  now: Date = new Date()
): RemainingDurationParts | null {
  const targetTime = targetAt instanceof Date ? targetAt.getTime() : new Date(targetAt).getTime()
  const nowTime = now.getTime()

  if (!Number.isFinite(targetTime) || !Number.isFinite(nowTime)) return null

  const diffMs = targetTime - nowTime
  if (diffMs <= 0) return null

  const totalMinutes = Math.floor(diffMs / (1000 * 60))
  const days = Math.floor(totalMinutes / (24 * 60))
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60)
  const minutes = totalMinutes % 60

  return { days, hours, minutes }
}

export function getSubscriptionQuotaBoundary(
  windowStart: string | null | undefined,
  period: SubscriptionQuotaWindow,
  expiresAt?: string | null
): SubscriptionQuotaBoundary | null {
  if (!windowStart) return null

  const startTime = new Date(windowStart).getTime()
  if (!Number.isFinite(startTime)) return null

  const periodMs = quotaWindowPeriodMs(period)
  const resetTime = startTime + periodMs
  const expiryTime = expiresAt ? new Date(expiresAt).getTime() : Number.POSITIVE_INFINITY

  if (!Number.isFinite(expiryTime)) {
    return { at: new Date(resetTime), kind: 'reset' }
  }

  if (resetTime + periodMs > expiryTime) {
    return { at: new Date(expiryTime), kind: 'expiry' }
  }

  return { at: new Date(resetTime), kind: 'reset' }
}

function quotaWindowPeriodMs(period: SubscriptionQuotaWindow): number {
  switch (period) {
    case 'daily':
      return ONE_DAY_MS
    case 'weekly':
      return ONE_WEEK_MS
    case 'monthly':
      return THIRTY_DAYS_MS
  }
}
