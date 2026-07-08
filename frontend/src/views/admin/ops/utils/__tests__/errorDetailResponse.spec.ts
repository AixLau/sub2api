import { describe, expect, it } from 'vitest'
import type { OpsErrorDetail } from '@/api/admin/ops'
import { resolveEmbeddedUpstreamErrors, resolvePrimaryResponseBody, resolveUpstreamPayload } from '../errorDetailResponse'

function makeDetail(overrides: Partial<OpsErrorDetail>): OpsErrorDetail {
  return {
    id: 1,
    created_at: '2026-01-01T00:00:00Z',
    phase: 'request',
    type: 'api_error',
    error_owner: 'platform',
    error_source: 'gateway',
    severity: 'P2',
    status_code: 502,
    platform: 'openai',
    model: 'gpt-4o-mini',
    resolved: false,
    client_request_id: 'crid-1',
    request_id: 'rid-1',
    message: 'Upstream request failed',
    user_email: 'user@example.com',
    account_name: 'acc',
    group_name: 'group',
    error_body: '',
    user_agent: '',
    is_business_limited: false,
    ...overrides
  }
}

describe('errorDetailResponse', () => {
  it('prefers upstream payload for request modal when error_body is generic gateway wrapper', () => {
    const detail = makeDetail({
      error_body: JSON.stringify({
        type: 'error',
        error: {
          type: 'upstream_error',
          message: 'Upstream request failed'
        }
      }),
      upstream_error_detail: '{"provider_message":"real upstream detail"}'
    })

    expect(resolvePrimaryResponseBody(detail, 'request')).toBe('{"provider_message":"real upstream detail"}')
  })

  it('prefers upstream payload when localized request body is generic gateway wrapper', () => {
    const detail = makeDetail({
      error_body: JSON.stringify({
        type: 'error',
        error: {
          type: 'upstream_error',
          message: '请求处理失败，请稍后重试'
        }
      }),
      upstream_error_detail: '{"provider_message":"real upstream detail"}'
    })

    expect(resolvePrimaryResponseBody(detail, 'request')).toBe('{"provider_message":"real upstream detail"}')
  })

  it('keeps error_body for request modal when body is not generic wrapper', () => {
    const detail = makeDetail({
      error_body: JSON.stringify({
        type: 'error',
        error: {
          type: 'upstream_error',
          message: 'Upstream authentication failed, please contact administrator'
        }
      }),
      upstream_error_detail: '{"provider_message":"real upstream detail"}'
    })

    expect(resolvePrimaryResponseBody(detail, 'request')).toBe(detail.error_body)
  })

  it('uses upstream payload first in upstream modal', () => {
    const detail = makeDetail({
      phase: 'upstream',
      upstream_error_message: 'provider 503 overloaded',
      error_body: '{"type":"error","error":{"type":"upstream_error","message":"Upstream request failed"}}'
    })

    expect(resolvePrimaryResponseBody(detail, 'upstream')).toBe('provider 503 overloaded')
  })

  it('falls back to upstream payload when request error_body is empty', () => {
    const detail = makeDetail({
      error_body: '',
      upstream_error_message: 'dial tcp timeout'
    })

    expect(resolvePrimaryResponseBody(detail, 'request')).toBe('dial tcp timeout')
  })

  it('resolves upstream payload by detail -> events -> message priority', () => {
    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: 'detail payload',
      upstream_errors: '[{"message":"event payload"}]',
      upstream_error_message: 'message payload'
    }))).toBe('detail payload')

    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: '',
      upstream_errors: '[{"message":"event payload"}]',
      upstream_error_message: 'message payload'
    }))).toBe('[{"message":"event payload"}]')

    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: '',
      upstream_errors: '',
      upstream_error_message: 'message payload'
    }))).toBe('message payload')
  })

  it('treats empty JSON placeholders in upstream payload as empty', () => {
    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: '',
      upstream_errors: '[]',
      upstream_error_message: ''
    }))).toBe('')

    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: '',
      upstream_errors: '{}',
      upstream_error_message: ''
    }))).toBe('')

    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: '',
      upstream_errors: 'null',
      upstream_error_message: ''
    }))).toBe('')
  })

  it('skips placeholder candidates and falls back to the next upstream field', () => {
    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: '',
      upstream_errors: '[]',
      upstream_error_message: 'fallback message'
    }))).toBe('fallback message')

    expect(resolveUpstreamPayload(makeDetail({
      upstream_error_detail: 'null',
      upstream_errors: '',
      upstream_error_message: 'fallback message'
    }))).toBe('fallback message')
  })

  it('expands upstream_errors stored on the current request detail', () => {
    const detail = makeDetail({
      upstream_errors: JSON.stringify([
        {
          at_unix_ms: 1782665127000,
          platform: 'openai',
          account_id: 158,
          account_name: 'openai-oauth',
          upstream_status_code: 403,
          upstream_request_id: 'up-rid',
          kind: 'http_error',
          message: 'insufficient balance',
          detail: '{"error":{"message":"insufficient balance","type":"billing_error"}}'
        }
      ])
    })

    expect(resolveEmbeddedUpstreamErrors(detail)).toEqual([
      expect.objectContaining({
        id: -1,
        phase: 'upstream',
        status_code: 403,
        platform: 'openai',
        account_id: 158,
        account_name: 'openai-oauth',
        request_id: 'up-rid',
        message: 'insufficient balance',
        upstream_error_detail: '{"error":{"message":"insufficient balance","type":"billing_error"}}'
      })
    ])
  })

  it('falls back to upstream message fields when upstream_errors is empty', () => {
    const detail = makeDetail({
      upstream_status_code: 403,
      upstream_error_message: 'insufficient balance',
      upstream_error_detail: '{"error":{"message":"insufficient balance","type":"billing_error"}}'
    })

    expect(resolveEmbeddedUpstreamErrors(detail)).toEqual([
      expect.objectContaining({
        id: -1,
        status_code: 403,
        message: 'insufficient balance',
        upstream_error_detail: '{"error":{"message":"insufficient balance","type":"billing_error"}}'
      })
    ])
  })
})
