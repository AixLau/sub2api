import { apiClient } from '../client'

export type MerchantIntegrationStatus = 'draft' | 'active' | 'disabled'
export type MerchantEndpointStatus = 'draft' | 'active' | 'disabled'
export type MerchantEndpointType =
  | 'register_login'
  | 'register'
  | 'login'
  | 'token'
  | 'sync'
  | 'bind'
  | 'status'
  | 'callback'
  | 'recharge_records'

export interface MerchantAPIEndpoint {
  id: number
  integration_id: number
  type: MerchantEndpointType
  url: string
  method: string
  content_type: string
  query_template: Record<string, unknown>
  header_template: Record<string, unknown>
  body_template: Record<string, unknown>
  auth_type: 'none' | 'api_key' | 'bearer' | 'basic' | 'hmac'
  secret_ref?: string
  response_mapping: Record<string, unknown>
  success_rule: Record<string, unknown>
  retry_policy: Record<string, unknown>
  timeout_ms: number
  status: MerchantEndpointStatus
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface MerchantIntegration {
  id: number
  name: string
  code: string
  mode: string
  merchant_code: string
  description: string
  status: MerchantIntegrationStatus
  enabled: boolean
  redirect_hosts: string[]
  endpoints: MerchantAPIEndpoint[]
  created_at: string
  updated_at: string
}

export interface MerchantIntegrationInput {
  name: string
  code: string
  mode?: string
  merchant_code: string
  description?: string
  status?: MerchantIntegrationStatus
  enabled?: boolean
  redirect_hosts: string[]
}

export interface MerchantEndpointInput {
  type: MerchantEndpointType
  url: string
  method: string
  content_type: string
  query_template: Record<string, unknown>
  header_template: Record<string, unknown>
  body_template: Record<string, unknown>
  auth_type: MerchantAPIEndpoint['auth_type']
  secret_ref?: string
  response_mapping: Record<string, unknown>
  success_rule: Record<string, unknown>
  retry_policy: Record<string, unknown>
  timeout_ms: number
  status: MerchantEndpointStatus
  enabled: boolean
}

export interface MerchantBinding {
  id: number
  integration_id: number
  integration_name?: string
  integration_code?: string
  user_id: number
  external_user_id: string
  external_account: string
  status: string
  last_login_at?: string
  last_sync_at?: string
  last_recharge_sync_at?: string
  recharge_sync_available: boolean
  created_at: string
  updated_at: string
}

export interface MerchantRechargeRecord {
  id: number
  integration_id: number
  user_id: string
  platform_user_id?: number
  order_no: string
  amount: string
  currency: string
  balance_before: string
  balance_after: string
  charge_type: string
  pay_method: string
  status: string
  platform_order_no: string
  created_at: string
  synced_at: string
  updated_at: string
}

export interface MerchantTestResult {
  endpoint_id: number
  http_status: number
  successful: boolean
  code?: string
  message?: string
  redirect_url?: string
  response?: Record<string, unknown>
}

export interface MerchantEndpointTestInput {
  user_id?: number
  start_time?: string
  end_time?: string
}

export interface MerchantRechargeSyncResult {
  binding_id: number
  synced: number
  records: MerchantRechargeRecord[]
}

export async function list(includeDisabled = true): Promise<MerchantIntegration[]> {
  const { data } = await apiClient.get<MerchantIntegration[]>('/admin/merchant-integrations', {
    params: { include_disabled: includeDisabled }
  })
  return data
}

export async function getById(id: number): Promise<MerchantIntegration> {
  const { data } = await apiClient.get<MerchantIntegration>(`/admin/merchant-integrations/${id}`)
  return data
}

export async function create(input: MerchantIntegrationInput): Promise<MerchantIntegration> {
  const { data } = await apiClient.post<MerchantIntegration>('/admin/merchant-integrations', input)
  return data
}

export async function update(id: number, input: MerchantIntegrationInput): Promise<MerchantIntegration> {
  const { data } = await apiClient.put<MerchantIntegration>(`/admin/merchant-integrations/${id}`, input)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/merchant-integrations/${id}`)
}

export async function setEnabled(id: number, enabled: boolean): Promise<MerchantIntegration> {
  const { data } = await apiClient.post<MerchantIntegration>(`/admin/merchant-integrations/${id}/enabled`, { enabled })
  return data
}

export async function createEndpoint(integrationId: number, input: MerchantEndpointInput): Promise<MerchantAPIEndpoint> {
  const { data } = await apiClient.post<MerchantAPIEndpoint>(`/admin/merchant-integrations/${integrationId}/endpoints`, input)
  return data
}

export async function updateEndpoint(integrationId: number, endpointId: number, input: MerchantEndpointInput): Promise<MerchantAPIEndpoint> {
  const { data } = await apiClient.put<MerchantAPIEndpoint>(`/admin/merchant-integrations/${integrationId}/endpoints/${endpointId}`, input)
  return data
}

export async function removeEndpoint(integrationId: number, endpointId: number): Promise<void> {
  await apiClient.delete(`/admin/merchant-integrations/${integrationId}/endpoints/${endpointId}`)
}

export async function setEndpointEnabled(integrationId: number, endpointId: number, enabled: boolean): Promise<MerchantAPIEndpoint> {
  const { data } = await apiClient.post<MerchantAPIEndpoint>(
    `/admin/merchant-integrations/${integrationId}/endpoints/${endpointId}/enabled`,
    { enabled }
  )
  return data
}

export async function testEndpoint(
  integrationId: number,
  endpointId: number,
  input: MerchantEndpointTestInput = {}
): Promise<MerchantTestResult> {
  const { data } = await apiClient.post<MerchantTestResult>(
    `/admin/merchant-integrations/${integrationId}/endpoints/${endpointId}/test`,
    input
  )
  return data
}

export async function listUserBindings(userId: number): Promise<MerchantBinding[]> {
  const { data } = await apiClient.get<MerchantBinding[]>(`/admin/users/${userId}/merchant-bindings`)
  return data
}

export async function listUserRechargeRecords(
  userId: number,
  bindingId: number,
  page = 1,
  pageSize = 20
): Promise<{ items: MerchantRechargeRecord[]; total: number }> {
  const { data } = await apiClient.get<{ items: MerchantRechargeRecord[]; total: number }>(
    `/admin/users/${userId}/merchant-bindings/${bindingId}/recharge-records`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

export async function syncUserRechargeRecords(
  userId: number,
  bindingId: number,
  input: { start_time?: string; end_time?: string } = {}
): Promise<MerchantRechargeSyncResult> {
  const { data } = await apiClient.post<MerchantRechargeSyncResult>(
    `/admin/users/${userId}/merchant-bindings/${bindingId}/recharge-records/sync`,
    input
  )
  return data
}

const merchantIntegrationsAPI = {
  list,
  getById,
  create,
  update,
  remove,
  setEnabled,
  createEndpoint,
  updateEndpoint,
  removeEndpoint,
  setEndpointEnabled,
  testEndpoint,
  listUserBindings,
  listUserRechargeRecords,
  syncUserRechargeRecords
}

export default merchantIntegrationsAPI
