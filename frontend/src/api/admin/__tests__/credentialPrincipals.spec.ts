import { describe, expect, it, vi } from 'vitest'
vi.mock('../../client', () => ({ apiClient: { post: vi.fn().mockResolvedValue({ data: { verification_state: 'UNVERIFIED' } }), patch: vi.fn().mockResolvedValue({ data: {} }), get: vi.fn() } }))
import { apiClient } from '../../client'
import { importCredential, updateCredentialLimit, type CredentialPrincipal } from '../credentialPrincipals'

describe('credential principal control contracts', () => {
  it('uses a write-only POST and stable idempotency header for imports', async () => {
    await importCredential('mock-access', 'mock-refresh', 'operation')
    expect(apiClient.post).toHaveBeenCalledWith('/admin/credential-imports', { access_token: 'mock-access', refresh_token: 'mock-refresh' }, { headers: { 'Idempotency-Key': 'operation' } })
  })
  it('sends the authoritative version and preserves zero as pause', async () => {
    await updateCredentialLimit({ id: 7, config_version: 12 } as CredentialPrincipal, 0)
    expect(apiClient.patch).toHaveBeenCalledWith('/admin/upstream-principals/7', { requested_limit: 0 }, { headers: { 'If-Match': '"v12"' } })
  })
})
