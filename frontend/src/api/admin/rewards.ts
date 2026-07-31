/**
 * Admin reward campaign and skin APIs.
 *
 * Reward amounts are always expressed in USD. Campaign versions are created
 * by the server; the client only submits the next mutable campaign draft.
 */

import { apiClient } from '../client'
import type { BasePaginationResponse } from '@/types'

export type RewardCampaignStatus =
  | 'draft'
  | 'scheduled'
  | 'active'
  | 'paused'
  | 'ended'
  | 'archived'

export type RewardIssuanceMode = 'on_access' | 'scheduled_batch'
export type RewardSkinStatus = 'enabled' | 'disabled' | 'archived'
export type RewardGrantStatus = 'pending' | 'claimed' | 'expired' | 'cancelled'
export type RewardJobStatus =
  | 'pending'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'dead'
  | 'cancelled'

export type RewardAudienceField =
  | 'registered_at'
  | 'registration_source'
  | 'last_active_at'
  | 'balance'
  | 'subscription_group_id'
  | 'user_id'
  | 'request_count_7d'
  | 'request_count_30d'
  | 'actual_cost_7d'
  | 'actual_cost_30d'
  | 'last_api_used_at'
  | 'recharge_amount_30d'
  | 'recharge_amount_total'

export type RewardAudienceOperator =
  | 'eq'
  | 'neq'
  | 'gt'
  | 'gte'
  | 'lt'
  | 'lte'
  | 'in'
  | 'not_in'
  | 'before'
  | 'after'
  | 'within_days'

export interface RewardAudienceCondition {
  field: RewardAudienceField
  operator: RewardAudienceOperator
  value: string | number | Array<string | number> | null
}

export interface RewardAudienceConditionGroup {
  all_of: RewardAudienceCondition[]
}

export interface RewardAudience {
  any_of: RewardAudienceConditionGroup[]
}

export interface RewardAmountTier {
  amount: number
  weight: number
}

export interface RewardSkinAllocation {
  skin_id: number
  weight: number
}

export interface RewardCampaignCopy {
  title: string
  hint: string
  scratch_prompt: string
  claim_cta: string
  success_message: string
}

export interface RewardCampaignCopySet {
  zh: RewardCampaignCopy
  en: RewardCampaignCopy
}

export interface RewardCampaignDraft {
  name: string
  description: string
  issuance_mode: RewardIssuanceMode
  timezone: string
  starts_at: string
  ends_at: string
  priority: number
  win_probability: number
  max_grants_per_user: number
  evaluation_interval_minutes: number
  cooldown_days: number
  control_group_percent: number
  total_budget: number
  amount_tiers: RewardAmountTier[]
  audience: RewardAudience
  skin_allocations: RewardSkinAllocation[]
  copy: RewardCampaignCopySet
}

export interface RewardCampaign extends RewardCampaignDraft {
  id: number
  status: RewardCampaignStatus
  current_version: number
  reserved_budget: number
  spent_budget: number
  released_budget: number
  created_at: string
  updated_at: string
  published_at?: string | null
}

export interface RewardCampaignListParams {
  page?: number
  page_size?: number
  status?: RewardCampaignStatus | ''
  issuance_mode?: RewardIssuanceMode | ''
  search?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface RewardAudienceEstimate {
  matched_users: number
  expected_winners: number
  expected_cost: number
  maximum_cost: number
  control_group_users: number
  data_updated_at: string
  warnings?: string[]
}

export interface RewardSkin {
  id: number
  name: string
  description: string
  alt_text: string
  image_url: string
  mime_type: 'image/png' | 'image/jpeg' | 'image/webp'
  width: number
  height: number
  size_bytes: number
  sha256: string
  status: RewardSkinStatus
  created_at: string
  updated_at: string
}

export interface RewardCampaignStats {
  evaluated: number
  granted: number
  viewed: number
  scratched: number
  claimed: number
  expired: number
  pending: number
  control_group: number
  total_budget: number
  reserved_budget: number
  spent_budget: number
  released_budget: number
  amount_distribution: Array<{ amount: number; count: number; total: number }>
  updated_at: string
}

export interface RewardGrant {
  id: string
  user_id: number
  user_email?: string
  campaign_version: number
  source: RewardIssuanceMode | 'legacy_migration' | string
  status: RewardGrantStatus
  amount: number
  expires_at: string
  viewed_at?: string | null
  claimed_at?: string | null
  balance_after?: number | null
  created_at: string
}

export interface RewardCampaignJob {
  id: string
  campaign_version: number
  status: RewardJobStatus
  processed_users: number
  matched_users: number
  granted_users: number
  cursor_user_id: number
  attempts: number
  scheduled_at: string
  started_at?: string | null
  finished_at?: string | null
  lease_expires_at?: string | null
  last_error?: string | null
}

export interface RewardSkinUploadMetadata {
  name: string
  description?: string
  alt_text: string
}

type RequestOptions = { signal?: AbortSignal }
type UnknownRecord = Record<string, any>

function asRecord(value: unknown): UnknownRecord {
  return value && typeof value === 'object' ? value as UnknownRecord : {}
}

function asNumber(value: unknown, fallback = 0): number {
  const result = Number(value)
  return Number.isFinite(result) ? result : fallback
}

function normalizeCopy(rawValue: unknown): RewardCampaignCopySet {
  const raw = asRecord(rawValue)
  if (raw.zh || raw.en) {
    return {
      zh: normalizeLocaleCopy(raw.zh),
      en: normalizeLocaleCopy(raw.en ?? raw.zh)
    }
  }
  const legacy = normalizeLocaleCopy({
    title: raw.title,
    hint: raw.hint ?? raw.prompt,
    scratch_prompt: raw.scratch_prompt ?? raw.gesture_hint ?? raw.cover_text,
    claim_cta: raw.claim_cta ?? raw.continue_text,
    success_message: raw.success_message ?? raw.credited_text ?? raw.won_text
  })
  return { zh: legacy, en: { ...legacy } }
}

function normalizeLocaleCopy(value: unknown): RewardCampaignCopy {
  const raw = asRecord(value)
  return {
    title: String(raw.title ?? ''),
    hint: String(raw.hint ?? ''),
    scratch_prompt: String(raw.scratch_prompt ?? ''),
    claim_cta: String(raw.claim_cta ?? ''),
    success_message: String(raw.success_message ?? '')
  }
}

function normalizeCampaign(rawValue: unknown): RewardCampaign {
  const raw = asRecord(rawValue)
  const config = asRecord(raw.config)
  const audience = asRecord(raw.audience ?? config.audience)
  const cooldownMinutes = asNumber(
    raw.claim_cooldown_minutes ?? config.claim_cooldown_minutes,
    0
  )
  return {
    id: asNumber(raw.id),
    name: String(raw.name ?? ''),
    description: String(raw.description ?? ''),
    status: String(raw.status ?? 'draft') as RewardCampaignStatus,
    issuance_mode: String(raw.issuance_mode ?? 'on_access') as RewardIssuanceMode,
    timezone: String(raw.timezone ?? 'UTC'),
    starts_at: String(raw.starts_at ?? ''),
    ends_at: String(raw.ends_at ?? ''),
    priority: asNumber(raw.priority ?? config.priority),
    win_probability: asNumber(raw.win_probability ?? config.win_probability),
    max_grants_per_user: asNumber(
      raw.max_grants_per_user ?? raw.per_user_limit ?? config.per_user_limit,
      1
    ),
    evaluation_interval_minutes: asNumber(
      raw.evaluation_interval_minutes ?? config.evaluation_interval_minutes
    ),
    cooldown_days: asNumber(raw.cooldown_days, cooldownMinutes / 1440),
    control_group_percent: asNumber(
      raw.control_group_percent ?? config.control_group_percent
    ),
    total_budget: asNumber(raw.total_budget),
    reserved_budget: asNumber(raw.reserved_budget),
    spent_budget: asNumber(raw.spent_budget),
    released_budget: asNumber(raw.released_budget),
    amount_tiers: (raw.amount_tiers ?? config.amount_tiers ?? []).map((tier: UnknownRecord) => ({
      amount: asNumber(tier.amount),
      weight: asNumber(tier.weight)
    })),
    audience: Array.isArray(audience.any_of)
      ? audience as unknown as RewardAudience
      : { any_of: [] },
    skin_allocations: (raw.skin_allocations ?? raw.skin_weights ?? config.skin_weights ?? [])
      .map((skin: UnknownRecord) => ({
        skin_id: asNumber(skin.skin_id),
        weight: asNumber(skin.weight)
      })),
    copy: normalizeCopy(raw.copy ?? raw.copy_i18n ?? config.copy_i18n ?? config.copy),
    current_version: asNumber(raw.current_version ?? raw.version_number, 0),
    created_at: String(raw.created_at ?? ''),
    updated_at: String(raw.updated_at ?? ''),
    published_at: raw.published_at ? String(raw.published_at) : null
  }
}

function legacyCopy(copy: RewardCampaignCopy) {
  return {
    title: copy.title,
    prompt: copy.hint,
    cover_text: copy.scratch_prompt,
    gesture_hint: copy.scratch_prompt,
    revealed_hint: copy.success_message,
    won_text: copy.success_message,
    credited_text: copy.success_message,
    continue_text: copy.claim_cta
  }
}

function campaignPayload(draft: RewardCampaignDraft): UnknownRecord {
  const startsAt = zonedLocalDateTimeToISO(draft.starts_at, draft.timezone)
  const endsAt = zonedLocalDateTimeToISO(draft.ends_at, draft.timezone)
  const audience = serializeRewardAudience(draft.audience, draft.timezone)
  const sharedConfig = {
    title: draft.copy.zh.title,
    priority: draft.priority,
    win_probability: draft.win_probability,
    per_user_limit: draft.max_grants_per_user,
    evaluation_interval_minutes: draft.evaluation_interval_minutes,
    claim_cooldown_minutes: Math.round(draft.cooldown_days * 1440),
    control_group_percent: draft.control_group_percent,
    amount_tiers: draft.amount_tiers,
    audience,
    copy: legacyCopy(draft.copy.zh),
    copy_i18n: {
      zh: legacyCopy(draft.copy.zh),
      en: legacyCopy(draft.copy.en)
    },
    skin_weights: draft.skin_allocations
  }
  return {
    ...draft,
    title: draft.copy.zh.title,
    starts_at: startsAt,
    ends_at: endsAt,
    audience,
    per_user_limit: draft.max_grants_per_user,
    claim_cooldown_minutes: Math.round(draft.cooldown_days * 1440),
    skin_weights: draft.skin_allocations,
    copy_i18n: {
      zh: legacyCopy(draft.copy.zh),
      en: legacyCopy(draft.copy.en)
    },
    config: sharedConfig
  }
}

const rewardAudienceDateFields = new Set<RewardAudienceField>([
  'registered_at',
  'last_active_at',
  'last_api_used_at'
])

export function serializeRewardAudience(
  audience: RewardAudience,
  timezone: string
): RewardAudience {
  return {
    any_of: audience.any_of.map((group) => ({
      all_of: group.all_of.map((condition) => {
        let value = condition.value
        if (
          rewardAudienceDateFields.has(condition.field) &&
          (condition.operator === 'before' || condition.operator === 'after') &&
          typeof value === 'string'
        ) {
          value = zonedLocalDateTimeToISO(value, timezone)
        }
        if (
          (condition.operator === 'in' || condition.operator === 'not_in') &&
          !Array.isArray(value)
        ) {
          value = value === null || value === '' ? [] : [value]
        }
        return { ...condition, value }
      })
    }))
  }
}

/**
 * Convert an IANA-zoned wall-clock value from a datetime-local input to UTC.
 * Two passes account for offset changes near daylight-saving transitions.
 */
export function zonedLocalDateTimeToISO(localValue: string, timezone: string): string {
  if (!localValue) return ''
  if (/[zZ]|[+-]\d{2}:\d{2}$/.test(localValue)) {
    const direct = new Date(localValue)
    return Number.isNaN(direct.getTime()) ? localValue : direct.toISOString()
  }

  const match = localValue.match(
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/
  )
  if (!match) return localValue
  const [, year, month, day, hour, minute, second = '0'] = match
  const wallClockUTC = Date.UTC(
    Number(year),
    Number(month) - 1,
    Number(day),
    Number(hour),
    Number(minute),
    Number(second)
  )

  let instant = wallClockUTC
  for (let index = 0; index < 3; index++) {
    instant = wallClockUTC - timezoneOffsetAt(instant, timezone)
  }
  return new Date(instant).toISOString()
}

function timezoneOffsetAt(instant: number, timezone: string): number {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: timezone,
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  }).formatToParts(new Date(instant))
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  const representedAsUTC = Date.UTC(
    Number(values.year),
    Number(values.month) - 1,
    Number(values.day),
    Number(values.hour),
    Number(values.minute),
    Number(values.second)
  )
  return representedAsUTC - Math.floor(instant / 1000) * 1000
}

function normalizePage<T>(
  value: unknown,
  normalizer: (item: unknown) => T,
  pageFallback = 1,
  pageSizeFallback = 20
): BasePaginationResponse<T> {
  const raw = asRecord(value)
  const source = Array.isArray(value)
    ? value
    : raw.items ?? raw.campaigns ?? raw.skins ?? raw.grants ?? raw.jobs ?? []
  const items = Array.isArray(source) ? source.map(normalizer) : []
  const total = asNumber(raw.total, items.length)
  const pageSize = asNumber(raw.page_size, pageSizeFallback)
  return {
    items,
    total,
    page: asNumber(raw.page, pageFallback),
    page_size: pageSize,
    pages: asNumber(raw.pages, pageSize > 0 ? Math.ceil(total / pageSize) : 0)
  }
}

export async function listCampaigns(
  params: RewardCampaignListParams = {},
  options?: RequestOptions
): Promise<BasePaginationResponse<RewardCampaign>> {
  const { data } = await apiClient.get<unknown>(
    '/admin/reward-campaigns',
    { params, signal: options?.signal }
  )
  return normalizePage(data, normalizeCampaign, params.page ?? 1, params.page_size ?? 20)
}

export async function getCampaign(id: number): Promise<RewardCampaign> {
  const { data } = await apiClient.get<unknown>(`/admin/reward-campaigns/${id}`)
  return normalizeCampaign(data)
}

export async function createCampaign(payload: RewardCampaignDraft): Promise<RewardCampaign> {
  const { data } = await apiClient.post<unknown>(
    '/admin/reward-campaigns',
    campaignPayload(payload)
  )
  return normalizeCampaign(data)
}

export async function updateCampaign(
  id: number,
  payload: RewardCampaignDraft
): Promise<RewardCampaign> {
  const { data } = await apiClient.put<unknown>(
    `/admin/reward-campaigns/${id}`,
    campaignPayload(payload)
  )
  return normalizeCampaign(data)
}

export async function cloneCampaign(id: number): Promise<RewardCampaign> {
  const { data } = await apiClient.post<unknown>(`/admin/reward-campaigns/${id}/clone`)
  return normalizeCampaign(data)
}

export async function estimateAudience(
  payload: RewardCampaignDraft,
  options?: RequestOptions
): Promise<RewardAudienceEstimate> {
  const { data } = await apiClient.post<UnknownRecord>(
    '/admin/reward-campaigns/estimate',
    campaignPayload(payload),
    { signal: options?.signal }
  )
  return {
    matched_users: asNumber(data.matched_users ?? data.eligible_users),
    expected_winners: asNumber(data.expected_winners),
    expected_cost: asNumber(data.expected_cost),
    maximum_cost: asNumber(data.maximum_cost),
    control_group_users: asNumber(data.control_group_users),
    data_updated_at: String(data.data_updated_at ?? ''),
    warnings: Array.isArray(data.warnings) ? data.warnings.map(String) : []
  }
}

async function runCampaignAction(
  id: number,
  action: 'publish' | 'pause' | 'resume' | 'end' | 'archive'
): Promise<RewardCampaign> {
  const { data } = await apiClient.post<unknown>(
    `/admin/reward-campaigns/${id}/${action}`
  )
  return normalizeCampaign(data)
}

export const publishCampaign = (id: number) => runCampaignAction(id, 'publish')
export const pauseCampaign = (id: number) => runCampaignAction(id, 'pause')
export const resumeCampaign = (id: number) => runCampaignAction(id, 'resume')
export const endCampaign = (id: number) => runCampaignAction(id, 'end')
export const archiveCampaign = (id: number) => runCampaignAction(id, 'archive')

export async function getCampaignStats(id: number): Promise<RewardCampaignStats> {
  const { data } = await apiClient.get<UnknownRecord>(
    `/admin/reward-campaigns/${id}/stats`
  )
  const budget = asRecord(data.budget)
  const rawDistribution = data.amount_distribution
  const amountDistribution = Array.isArray(rawDistribution)
    ? rawDistribution.map((item) => ({
        amount: asNumber(item.amount),
        count: asNumber(item.count),
        total: asNumber(item.total, asNumber(item.amount) * asNumber(item.count))
      }))
    : Object.entries(asRecord(rawDistribution)).map(([amount, count]) => ({
        amount: asNumber(amount),
        count: asNumber(count),
        total: asNumber(amount) * asNumber(count)
      }))
  return {
    evaluated: asNumber(data.evaluated),
    granted: asNumber(data.granted ?? data.won),
    viewed: asNumber(data.viewed),
    scratched: asNumber(data.scratched ?? data.claimed),
    claimed: asNumber(data.claimed),
    expired: asNumber(data.expired),
    pending: asNumber(data.pending),
    control_group: asNumber(data.control_group),
    total_budget: asNumber(data.total_budget ?? budget.total),
    reserved_budget: asNumber(data.reserved_budget ?? budget.reserved),
    spent_budget: asNumber(data.spent_budget ?? budget.spent),
    released_budget: asNumber(data.released_budget ?? budget.released),
    amount_distribution: amountDistribution,
    updated_at: String(data.updated_at ?? '')
  }
}

export async function listCampaignGrants(
  id: number,
  params: { page?: number; page_size?: number; status?: RewardGrantStatus | ''; search?: string } = {},
  options?: RequestOptions
): Promise<BasePaginationResponse<RewardGrant>> {
  const { data } = await apiClient.get<unknown>(
    `/admin/reward-campaigns/${id}/grants`,
    { params, signal: options?.signal }
  )
  return normalizePage(data, normalizeGrant, params.page ?? 1, params.page_size ?? 20)
}

export async function listCampaignJobs(
  id: number,
  params: { page?: number; page_size?: number } = {},
  options?: RequestOptions
): Promise<BasePaginationResponse<RewardCampaignJob>> {
  const { data } = await apiClient.get<unknown>(
    `/admin/reward-campaigns/${id}/jobs`,
    { params, signal: options?.signal }
  )
  return normalizePage(data, normalizeJob, params.page ?? 1, params.page_size ?? 20)
}

export async function createCampaignJob(id: number): Promise<RewardCampaignJob> {
  const { data } = await apiClient.post<unknown>(
    `/admin/reward-campaigns/${id}/jobs`
  )
  return normalizeJob(data)
}

export async function listSkins(
  params: { page?: number; page_size?: number; status?: RewardSkinStatus | '' } = {},
  options?: RequestOptions
): Promise<BasePaginationResponse<RewardSkin>> {
  const { data } = await apiClient.get<unknown>(
    '/admin/reward-skins',
    { params, signal: options?.signal }
  )
  return normalizePage(data, normalizeSkin, params.page ?? 1, params.page_size ?? 20)
}

export async function uploadSkin(
  file: File,
  metadata: RewardSkinUploadMetadata
): Promise<RewardSkin> {
  const body = new FormData()
  body.append('file', file, file.name)
  body.append('name', metadata.name)
  body.append('alt_text', metadata.alt_text)
  if (metadata.description) body.append('description', metadata.description)

  const { data } = await apiClient.post<unknown>('/admin/reward-skins', body, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
  return normalizeSkin(data)
}

export async function updateSkin(
  id: number,
  payload: Pick<RewardSkin, 'name' | 'description' | 'alt_text' | 'status'>
): Promise<RewardSkin> {
  const wirePayload = {
    ...payload,
    status: payload.status === 'enabled'
      ? 'active'
      : payload.status === 'disabled'
        ? 'inactive'
        : payload.status
  }
  const { data } = await apiClient.put<unknown>(`/admin/reward-skins/${id}`, wirePayload)
  return normalizeSkin(data)
}

export async function archiveSkin(id: number): Promise<RewardSkin> {
  const { data } = await apiClient.post<unknown>(`/admin/reward-skins/${id}/archive`)
  return normalizeSkin(data)
}

function normalizeSkin(rawValue: unknown): RewardSkin {
  const raw = asRecord(rawValue)
  const wireStatus = String(raw.status ?? 'inactive')
  const status: RewardSkinStatus = wireStatus === 'active'
    ? 'enabled'
    : wireStatus === 'inactive'
      ? 'disabled'
      : wireStatus as RewardSkinStatus
  return {
    id: asNumber(raw.id),
    name: String(raw.name ?? ''),
    description: String(raw.description ?? ''),
    alt_text: String(raw.alt_text ?? raw.description ?? raw.name ?? ''),
    image_url: String(raw.image_url ?? ''),
    mime_type: String(raw.mime_type ?? 'image/webp') as RewardSkin['mime_type'],
    width: asNumber(raw.width),
    height: asNumber(raw.height),
    size_bytes: asNumber(raw.size_bytes ?? raw.byte_size),
    sha256: String(raw.sha256 ?? ''),
    status,
    created_at: String(raw.created_at ?? ''),
    updated_at: String(raw.updated_at ?? '')
  }
}

function normalizeGrant(rawValue: unknown): RewardGrant {
  const raw = asRecord(rawValue)
  return {
    id: String(raw.id ?? raw.grant_id ?? ''),
    user_id: asNumber(raw.user_id),
    user_email: raw.user_email ? String(raw.user_email) : undefined,
    campaign_version: asNumber(
      raw.campaign_version ?? raw.version_number ?? raw.campaign_version_id
    ),
    source: String(raw.source ?? ''),
    status: String(raw.status ?? 'pending') as RewardGrantStatus,
    amount: asNumber(raw.amount),
    expires_at: String(raw.expires_at ?? ''),
    viewed_at: raw.viewed_at ? String(raw.viewed_at) : null,
    claimed_at: raw.claimed_at ? String(raw.claimed_at) : null,
    balance_after: raw.balance_after === null || raw.balance_after === undefined
      ? null
      : asNumber(raw.balance_after),
    created_at: String(raw.created_at ?? '')
  }
}

function normalizeJob(rawValue: unknown): RewardCampaignJob {
  const raw = asRecord(rawValue)
  const rawStatus = String(raw.status ?? 'pending')
  const normalizedStatus: RewardJobStatus = ({
    processing: 'running',
    retry: 'pending',
    succeeded: 'completed',
    dead_letter: 'dead'
  } as Record<string, RewardJobStatus>)[rawStatus] ?? rawStatus as RewardJobStatus
  return {
    id: String(raw.id ?? ''),
    campaign_version: asNumber(
      raw.campaign_version ?? raw.version_number ?? raw.campaign_version_id
    ),
    status: normalizedStatus,
    processed_users: asNumber(raw.processed_users ?? raw.processed_count ?? raw.scanned_users),
    matched_users: asNumber(raw.matched_users ?? raw.eligible_count),
    granted_users: asNumber(raw.granted_users ?? raw.granted_count),
    cursor_user_id: asNumber(raw.cursor_user_id),
    attempts: asNumber(raw.attempts ?? raw.retry_count ?? raw.attempt_count),
    scheduled_at: String(raw.scheduled_at ?? ''),
    started_at: raw.started_at ? String(raw.started_at) : null,
    finished_at: raw.finished_at ? String(raw.finished_at) : null,
    lease_expires_at: raw.lease_expires_at || raw.locked_until
      ? String(raw.lease_expires_at ?? raw.locked_until)
      : null,
    last_error: raw.last_error ? String(raw.last_error) : null
  }
}

const rewardsAPI = {
  listCampaigns,
  getCampaign,
  createCampaign,
  updateCampaign,
  cloneCampaign,
  estimateAudience,
  publishCampaign,
  pauseCampaign,
  resumeCampaign,
  endCampaign,
  archiveCampaign,
  getCampaignStats,
  listCampaignGrants,
  listCampaignJobs,
  createCampaignJob,
  listSkins,
  uploadSkin,
  updateSkin,
  archiveSkin
}

export default rewardsAPI
