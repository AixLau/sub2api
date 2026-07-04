import type { SubscriptionPlan } from '@/types/payment'

export interface SubscriptionPlanDisplay {
  description: string
  quotaSummary: string
  validitySummary: string
}

export interface SubscriptionPlanDisplayLabels {
  quotaUnit: string
  dayUnit: string
  monthUnit: string
  yearUnit: string
  validitySuffix: string
}

type Translate = (key: string) => string

function translatedOrFallback(t: Translate, key: string, fallback: string): string {
  const value = t(key)
  return value === key ? fallback : value
}

export function buildSubscriptionPlanDisplayLabels(t: Translate): SubscriptionPlanDisplayLabels {
  return {
    quotaUnit: translatedOrFallback(t, 'payment.planCard.quotaUnit', '额度'),
    dayUnit: translatedOrFallback(t, 'payment.days', '天'),
    monthUnit: translatedOrFallback(t, 'payment.months', '个月'),
    yearUnit: translatedOrFallback(t, 'payment.years', '年'),
    validitySuffix: translatedOrFallback(t, 'payment.planCard.validitySuffix', '有效'),
  }
}

function formatNumericValue(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/\.?0+$/, '')
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function uniquePattern(values: string[]): string {
  return Array.from(new Set(values.filter(Boolean))).map(escapeRegExp).join('|')
}

export function formatSubscriptionQuota(value: number | null | undefined, labels: Pick<SubscriptionPlanDisplayLabels, 'quotaUnit'>): string {
  if (value == null || value <= 0) return ''
  return `${formatNumericValue(value)} ${labels.quotaUnit}`.trim()
}

export function formatSubscriptionValidity(
  plan: Pick<SubscriptionPlan, 'validity_days' | 'validity_unit'>,
  labels: Pick<SubscriptionPlanDisplayLabels, 'dayUnit' | 'monthUnit' | 'yearUnit' | 'validitySuffix'>,
): string {
  const unit = plan.validity_unit || 'day'
  const count = plan.validity_days || 1
  const unitLabel = unit === 'month' ? labels.monthUnit : unit === 'year' ? labels.yearUnit : labels.dayUnit
  return `${count} ${unitLabel}${labels.validitySuffix}`.trim()
}

function stripGeneratedSummary(description: string, quotaSummary: string, labels: Pick<SubscriptionPlanDisplayLabels, 'quotaUnit' | 'dayUnit'>, validityDays: number): string {
  if (!description) return ''

  let result = description.trim()
  if (quotaSummary) {
    const quotaUnitPattern = uniquePattern([labels.quotaUnit, '额度', 'credits', 'quota'])
    const dayUnitPattern = uniquePattern([labels.dayUnit, '天', 'days', 'day'])
    const quotaValue = quotaSummary.replace(new RegExp(`\\s*(?:${quotaUnitPattern})$`, 'i'), '')
    result = result
      .replace(new RegExp(`包含\\s*${quotaValue}\\s*(?:${quotaUnitPattern})\\s*[\/／]\\s*${validityDays}\\s*(?:${dayUnitPattern})`, 'gi'), '')
      .replace(new RegExp(`${quotaValue}\\s*(?:${quotaUnitPattern})\\s*[\/／]\\s*${validityDays}\\s*(?:${dayUnitPattern})`, 'gi'), '')
      .replace(new RegExp(`包含\\s*${quotaValue}\\s*(?:${quotaUnitPattern})`, 'gi'), '')
  }

  return result
    .replace(new RegExp(`${validityDays}\\s*(?:${uniquePattern([labels.dayUnit, '天', 'days', 'day'])})`, 'gi'), '')
    .replace(/^[\s,，/／|｜;；·\-]+|[\s,，/／|｜;；·\-]+$/g, '')
    .replace(/\s{2,}/g, ' ')
    .trim()
}

export function buildSubscriptionPlanDisplay(plan: SubscriptionPlan, labels: SubscriptionPlanDisplayLabels): SubscriptionPlanDisplay {
  const quotaSummary = formatSubscriptionQuota(plan.monthly_limit_usd, labels)
  const validitySummary = formatSubscriptionValidity(plan, labels)
  return {
    description: stripGeneratedSummary(plan.description || '', quotaSummary, labels, plan.validity_days),
    quotaSummary,
    validitySummary,
  }
}
