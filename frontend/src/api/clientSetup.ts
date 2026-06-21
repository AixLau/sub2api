import { apiClient } from './client'

export interface ClientSetupSession {
  setup_id: string
  device_code: string
  client: 'codex' | 'claude'
  status: 'pending' | 'approved' | 'exchanged'
  verify_url?: string
  redirect_uri?: string
  expires_in?: number
}

export interface ClientSetupApproveResponse {
  setup_id: string
  client: 'codex' | 'claude'
  status: 'approved'
  setup_token: string
  redirect_uri?: string
}

export async function getSession(setupId: string): Promise<ClientSetupSession> {
  const { data } = await apiClient.get<ClientSetupSession>(`/client-setup/sessions/${encodeURIComponent(setupId)}`)
  return data
}

export async function approveSession(
  setupId: string,
  payload: {
    device_code: string
    client?: string
  }
): Promise<ClientSetupApproveResponse> {
  const { data } = await apiClient.post<ClientSetupApproveResponse>(
    `/client-setup/sessions/${encodeURIComponent(setupId)}/approve`,
    payload
  )
  return data
}

export const clientSetupAPI = {
  getSession,
  approveSession
}

export default clientSetupAPI
