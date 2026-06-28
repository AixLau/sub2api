import type { OpsErrorDetail } from '@/api/admin/ops'

const GENERIC_UPSTREAM_MESSAGES = new Set([
  'upstream request failed',
  'upstream request failed after retries',
  'upstream gateway error',
  'upstream service temporarily unavailable'
])

type ParsedGatewayError = {
  type: string
  message: string
}

function parseGatewayErrorBody(raw: string): ParsedGatewayError | null {
  const text = String(raw || '').trim()
  if (!text) return null

  try {
    const parsed = JSON.parse(text) as Record<string, any>
    const err = parsed?.error as Record<string, any> | undefined
    if (!err || typeof err !== 'object') return null

    const type = typeof err.type === 'string' ? err.type.trim() : ''
    const message = typeof err.message === 'string' ? err.message.trim() : ''
    if (!type && !message) return null

    return { type, message }
  } catch {
    return null
  }
}

function isGenericGatewayUpstreamError(raw: string): boolean {
  const parsed = parseGatewayErrorBody(raw)
  if (!parsed) return false
  if (parsed.type !== 'upstream_error') return false
  return GENERIC_UPSTREAM_MESSAGES.has(parsed.message.toLowerCase())
}

export function resolveUpstreamPayload(
  detail: Pick<OpsErrorDetail, 'upstream_error_detail' | 'upstream_errors' | 'upstream_error_message'> | null | undefined
): string {
  if (!detail) return ''

  const candidates = [
    detail.upstream_error_detail,
    detail.upstream_errors,
    detail.upstream_error_message
  ]

  for (const candidate of candidates) {
    const payload = String(candidate || '').trim()
    if (!payload) continue

    // Normalize common "empty but present" JSON placeholders.
    if (payload === '[]' || payload === '{}' || payload.toLowerCase() === 'null') {
      continue
    }

    return payload
  }

  return ''
}

export function resolvePrimaryResponseBody(
  detail: OpsErrorDetail | null,
  errorType?: 'request' | 'upstream'
): string {
  if (!detail) return ''

  const upstreamPayload = resolveUpstreamPayload(detail)
  const errorBody = String(detail.error_body || '').trim()

  if (errorType === 'upstream') {
    return upstreamPayload || errorBody
  }

  if (!errorBody) {
    return upstreamPayload
  }

  // For request detail modal, keep client-visible body by default.
  // But if that body is a generic gateway wrapper, show upstream payload first.
  if (upstreamPayload && isGenericGatewayUpstreamError(errorBody)) {
    return upstreamPayload
  }

  return errorBody
}

type EmbeddedUpstreamEvent = {
  at_unix_ms?: number
  platform?: string
  account_id?: number
  account_name?: string
  upstream_status_code?: number
  upstream_request_id?: string
  kind?: string
  message?: string
  detail?: string
  upstream_response_body?: string
}

function parseEmbeddedUpstreamEvents(raw: string | null | undefined): EmbeddedUpstreamEvent[] {
  const text = String(raw || '').trim()
  if (!text || text === '[]' || text.toLowerCase() === 'null') return []

  try {
    const parsed = JSON.parse(text)
    return Array.isArray(parsed) ? parsed.filter((item) => item && typeof item === 'object') : []
  } catch {
    return []
  }
}

function syntheticUpstreamDetail(base: OpsErrorDetail, id: number): OpsErrorDetail {
  return {
    ...base,
    id,
    phase: 'upstream',
    error_owner: 'provider',
    error_source: 'upstream_http',
    type: base.type || 'upstream_error',
    message: base.upstream_error_message || base.message || '',
    status_code: base.upstream_status_code ?? base.status_code,
    request_id: base.request_id || base.client_request_id || '',
    upstream_error_detail: base.upstream_error_detail || '',
    upstream_error_message: base.upstream_error_message || '',
    upstream_errors: ''
  }
}

export function resolveEmbeddedUpstreamErrors(detail: OpsErrorDetail | null | undefined): OpsErrorDetail[] {
  if (!detail) return []

  const events = parseEmbeddedUpstreamEvents(detail.upstream_errors)
  if (events.length > 0) {
    return events.map((event, index) => ({
      ...syntheticUpstreamDetail(detail, -(index + 1)),
      created_at: event.at_unix_ms ? new Date(event.at_unix_ms).toISOString() : detail.created_at,
      platform: event.platform || detail.platform,
      account_id: event.account_id ?? detail.account_id,
      account_name: event.account_name || detail.account_name,
      status_code: event.upstream_status_code ?? detail.upstream_status_code ?? detail.status_code,
      request_id: event.upstream_request_id || detail.request_id || detail.client_request_id || '',
      type: event.kind || detail.type || 'upstream_error',
      message: event.message || detail.upstream_error_message || detail.message || '',
      upstream_error_message: event.message || detail.upstream_error_message || '',
      upstream_error_detail: event.upstream_response_body || event.detail || detail.upstream_error_detail || '',
      upstream_errors: ''
    }))
  }

  const message = String(detail.upstream_error_message || '').trim()
  const payload = resolveUpstreamPayload(detail)
  if (!message && !payload && !detail.upstream_status_code) return []

  return [syntheticUpstreamDetail(detail, -1)]
}
