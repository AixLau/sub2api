import { apiClient } from '../client'

export interface CredentialInstance {
  max_concurrency: number
  state: string
  overhang: number
  unknown_occupied: number
  active_bindings: number
  archived_at?: string
  transport_state: string
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
  group_ids: number[]
  proxy_id: number | null
  account_id: number
  account_max_concurrency: number
  configured_capacity: number
  effective_configured_capacity: number
  available_capacity: number
  health_state: string
  unknown_occupied: number
  observed_at: string
  archived_at?: string
  id: number
  name: string
  provider?: string
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
  unresolved_leases: { id: string; instance_id: number; state: string; observed_at: string }[]
  wait_reasons: { reason: string; count: number }[]
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
export async function importCredential(accessToken: string, refreshToken: string, operation: string, clientId?: string) {
  return (await apiClient.post<{ import_id: string; verification_state: string; expires_at: string }>('/admin/credential-imports', { access_token: accessToken, refresh_token: refreshToken, ...(clientId ? { client_id: clientId } : {}) }, { headers: { 'Idempotency-Key': operation } })).data
}
export async function updateCredentialLimit(principal: CredentialPrincipal, requestedLimit: number) {
  return (await apiClient.patch<CredentialPrincipal>(`/admin/upstream-principals/${principal.id}`, { requested_limit: requestedLimit }, { headers: { 'If-Match': `"v${principal.config_version}"` } })).data
}

export interface InstanceConfiguration { id: number; name: string; max_concurrency: number }
export interface PrincipalConfiguration {
  group_ids?: number[]
  name?: string
  account_max_concurrency?: number
  instances?: InstanceConfiguration[]
  admin_state?: string
  drain_deadline?: string
  archive?: boolean
}
export async function saveCredentialConfiguration(principal: CredentialPrincipal, input: PrincipalConfiguration) {
  return (await apiClient.patch<CredentialPrincipal>(`/admin/upstream-principals/${principal.id}`, input, { headers: { 'If-Match': `"v${principal.config_version}"` } })).data
}
export interface CredentialInstanceInput { credential_import_id: string; name: string; max_concurrency: number; replace_instance_id?: number; drain_deadline?: string }
export interface CredentialAccountInput {
  extra?: Record<string, unknown>
  name: string; account_max_concurrency: number; instances: CredentialInstanceInput[]
  group_ids?: number[]; proxy_id?: number | null; priority?: number; rate_multiplier?: number
}
export async function createCredentialAccount(input: CredentialAccountInput, operation: string) {
  return (await apiClient.post<{id: number}>('/admin/upstream-principals', input, { headers: { 'Idempotency-Key': operation } })).data
}
export async function addCredentialInstance(principal: CredentialPrincipal, input: CredentialInstanceInput, operation: string) {
  return (await apiClient.post<{instance_id: number}>(`/admin/upstream-principals/${principal.id}/instances`, input, { headers: { 'If-Match': `"v${principal.config_version}"`, 'Idempotency-Key': operation } })).data
}
export async function controlCredentialInstance(principal: CredentialPrincipal, id: number, input: { admin_state?: string; drain_deadline?: string; archive?: boolean }) {
  return (await apiClient.patch<CredentialPrincipal>(`/admin/credential-instances/${id}`, input, { headers: { 'If-Match': `"v${principal.config_version}"` } })).data
}
export async function refreshCredentialInstance(id: number) {
  return (await apiClient.post(`/admin/credential-instances/${id}/refresh`)).data
}
export function credentialErrorReason(error: unknown): string {
  const value = error as { reason?: string; code?: string; response?: {data?: {reason?: string; code?: string}} }
  return value?.reason || value?.response?.data?.reason || (typeof value?.code === 'string' ? value.code : '') || value?.response?.data?.code || 'UNAVAILABLE'
}

export async function reverifyCredentialImport(id: string) {
  return (await apiClient.post<{verification_state: string}>(`/admin/credential-imports/${id}/verify`)).data
}

export async function resolveCredentialLease(id: string, evidence: string, reason: string) {
  return (await apiClient.post(`/admin/request-leases/${id}/resolve`, { evidence, reason, confirm: true })).data
}
