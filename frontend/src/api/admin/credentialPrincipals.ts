import { apiClient } from '../client'

export interface CredentialInstance {
  id: number
  account_id: number
  name: string
  generation: string
  credential_version: number
  weight: number
  hard_max: number | null
  effective_hard_max: number
  occupied: number
  admin_state: string
  credential_state: string
  identity_source: string
}
export interface CredentialPrincipal {
  id: number
  name: string
  requested_limit: number
  occupied: number
  overhang: number
  config_version: number
  admin_state: string
  admission_state: string
  verification_state: string
  routing_mode: string
  instances: CredentialInstance[]
}
export interface CredentialRuntime {
  reserved: number
  dispatching: number
  running: number
  cancelling: number
  orphaned: number
  queued: number
  counter_mismatch: boolean
  observed_at: string
}
export async function listCredentialPrincipals(after = 0) {
  return (await apiClient.get<{ items: CredentialPrincipal[]; http_enabled: boolean }>('/admin/upstream-principals', { params: { after } })).data
}
export async function getCredentialPrincipal(id: number) {
  return (await apiClient.get<CredentialPrincipal>(`/admin/upstream-principals/${id}`)).data
}
export async function getCredentialRuntime(id: number) {
  return (await apiClient.get<CredentialRuntime>(`/admin/upstream-principals/${id}/runtime`)).data
}
export async function importCredential(accessToken: string, refreshToken: string, operation: string) {
  return (await apiClient.post<{ import_id: string; verification_state: string; expires_at: string }>('/admin/credential-imports', { access_token: accessToken, refresh_token: refreshToken }, { headers: { 'Idempotency-Key': operation } })).data
}
export async function updateCredentialLimit(principal: CredentialPrincipal, requestedLimit: number) {
  return (await apiClient.patch<CredentialPrincipal>(`/admin/upstream-principals/${principal.id}`, { requested_limit: requestedLimit }, { headers: { 'If-Match': `"v${principal.config_version}"` } })).data
}
