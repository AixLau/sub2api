/**
 * User API endpoints
 * Handles user profile management and password changes
 */

import { apiClient } from './client'
import {
  resolveWeChatOAuthStartStrict,
  prepareOAuthBindAccessTokenCookie,
  type WeChatOAuthPublicSettings,
} from './auth'
import type {
  User,
  ChangePasswordRequest,
  NotifyEmailEntry,
  UserAuthProvider,
  UserAffiliateDetail,
  AffiliateTransferResponse,
  PlatformQuotasResponse,
  PendingRewardsResponse,
  RewardClaimResponse,
  RewardGrant,
  SurpriseRewardStatusResponse,
  WelcomeRewardClaimResponse,
} from '@/types'

export interface UserMerchantIntegration {
  id: number
  name: string
  code: string
  description: string
}

export interface UserMerchantBinding {
  id: number
  integration_id: number
  integration_name?: string
  integration_code?: string
  external_user_id: string
  external_account: string
  status: string
  last_login_at?: string
  last_recharge_sync_at?: string
}

export async function listMerchantIntegrations(): Promise<UserMerchantIntegration[]> {
  const { data } = await apiClient.get<UserMerchantIntegration[]>('/merchant-integrations')
  return data
}

export async function launchMerchantIntegration(integrationId: number): Promise<{ redirect_url: string }> {
  const { data } = await apiClient.post<{ redirect_url: string }>(`/merchant-integrations/${integrationId}/launch`)
  return data
}

export async function listMerchantBindings(): Promise<UserMerchantBinding[]> {
  const { data } = await apiClient.get<UserMerchantBinding[]>('/merchant-integrations/bindings')
  return data
}

/**
 * Get current user profile
 * @returns User profile data
 */
export async function getProfile(): Promise<User> {
  const { data } = await apiClient.get<User>('/user/profile')
  return data
}

export async function claimWelcomeReward(): Promise<WelcomeRewardClaimResponse> {
  const { data } = await apiClient.post<WelcomeRewardClaimResponse>('/user/welcome-reward/claim')
  return data
}

export async function checkWelcomeReward(): Promise<SurpriseRewardStatusResponse> {
  const { data } = await apiClient.post<SurpriseRewardStatusResponse>('/user/welcome-reward/check')
  return data
}

export async function checkSurpriseReward(): Promise<SurpriseRewardStatusResponse> {
  const { data } = await apiClient.post<SurpriseRewardStatusResponse>('/user/surprise-reward/check')
  return data
}

export async function claimSurpriseReward(): Promise<WelcomeRewardClaimResponse> {
  const { data } = await apiClient.post<WelcomeRewardClaimResponse>('/user/surprise-reward/claim')
  return data
}

export async function getPendingRewards(): Promise<RewardGrant[]> {
  const { data } = await apiClient.get<PendingRewardsResponse>('/user/rewards/pending')
  return data.items
}

export async function viewReward(grantID: number): Promise<void> {
  await apiClient.post(`/user/rewards/${grantID}/view`)
}

export async function claimReward(grantID: number): Promise<RewardClaimResponse> {
  const { data } = await apiClient.post<RewardClaimResponse>(`/user/rewards/${grantID}/claim`)
  return data
}

/**
 * Update current user profile
 * @param profile - Profile data to update
 * @returns Updated user profile data
 */
export async function updateProfile(profile: {
  username?: string
  avatar_url?: string | null
  balance_notify_enabled?: boolean
  balance_notify_threshold?: number | null
  balance_notify_extra_emails?: NotifyEmailEntry[]
}): Promise<User> {
  const { data } = await apiClient.put<User>('/user', profile)
  return data
}

/**
 * Change current user password
 * @param passwords - Old and new password
 * @returns Success message
 */
export async function changePassword(
  oldPassword: string,
  newPassword: string
): Promise<{ message: string }> {
  const payload: ChangePasswordRequest = {
    old_password: oldPassword,
    new_password: newPassword
  }

  const { data } = await apiClient.put<{ message: string }>('/user/password', payload)
  return data
}

/**
 * Send verification code for adding a notify email
 * @param email - Email address to verify
 */
export async function sendNotifyEmailCode(email: string): Promise<void> {
  await apiClient.post('/user/notify-email/send-code', { email })
}

/**
 * Verify and add a notify email
 * @param email - Email address to add
 * @param code - Verification code
 */
export async function verifyNotifyEmail(email: string, code: string): Promise<void> {
  await apiClient.post('/user/notify-email/verify', { email, code })
}

/**
 * Remove a notify email
 * @param email - Email address to remove
 */
export async function removeNotifyEmail(email: string): Promise<void> {
  await apiClient.delete('/user/notify-email', { data: { email } })
}

/**
 * Toggle a notify email's disabled state
 * @param email - Email address (empty string for primary email placeholder)
 * @param disabled - Whether to disable the email
 */
export async function toggleNotifyEmail(email: string, disabled: boolean): Promise<User> {
  const { data } = await apiClient.put<User>('/user/notify-email/toggle', { email, disabled })
  return data
}

export async function sendEmailBindingCode(email: string): Promise<void> {
  await apiClient.post('/user/account-bindings/email/send-code', { email })
}

export async function bindEmailIdentity(payload: {
  email: string
  verify_code: string
  password: string
}): Promise<User> {
  const { data } = await apiClient.post<User>('/user/account-bindings/email', payload)
  return data
}

export async function unbindAuthIdentity(provider: BindableOAuthProvider): Promise<User> {
  const { data } = await apiClient.delete<User>(`/user/account-bindings/${provider}`)
  return data
}

export type BindableOAuthProvider = Exclude<UserAuthProvider, 'email'>

interface BuildOAuthBindingStartURLOptions {
  redirectTo?: string
  wechatOAuthSettings?: WeChatOAuthPublicSettings | null
}

export function resolveWeChatOAuthMode(): 'open' | 'mp' {
  if (typeof navigator === 'undefined') {
    return 'open'
  }
  return /MicroMessenger/i.test(navigator.userAgent) ? 'mp' : 'open'
}

function resolveWeChatOAuthBindingMode(
  settings?: WeChatOAuthPublicSettings | null
): 'open' | 'mp' | null {
  if (settings) {
    return resolveWeChatOAuthStartStrict(settings).mode
  }
  return resolveWeChatOAuthMode()
}

export function buildOAuthBindingStartURL(
  provider: BindableOAuthProvider,
  options: BuildOAuthBindingStartURLOptions = {}
): string | null {
  const redirectTo = options.redirectTo?.trim() || '/profile'
  const apiBase = (import.meta.env.VITE_API_BASE_URL as string | undefined) || '/api/v1'
  const normalized = apiBase.replace(/\/$/, '')
  const params = new URLSearchParams({
    redirect: redirectTo,
    intent: 'bind_current_user'
  })

  if (provider === 'wechat') {
    const mode = resolveWeChatOAuthBindingMode(options.wechatOAuthSettings)
    if (!mode) {
      return null
    }
    params.set('mode', mode)
  }

  return `${normalized}/auth/oauth/${provider}/bind/start?${params.toString()}`
}

export async function startOAuthBinding(
  provider: BindableOAuthProvider,
  options: BuildOAuthBindingStartURLOptions = {}
): Promise<void> {
  if (typeof window === 'undefined') {
    return
  }
  const startURL = buildOAuthBindingStartURL(provider, options)
  if (!startURL) {
    return
  }
  await prepareOAuthBindAccessTokenCookie()
  window.location.href = startURL
}

export async function getAffiliateDetail(): Promise<UserAffiliateDetail> {
  const { data } = await apiClient.get<UserAffiliateDetail>('/user/aff')
  return data
}

export async function transferAffiliateQuota(): Promise<AffiliateTransferResponse> {
  const { data } = await apiClient.post<AffiliateTransferResponse>('/user/aff/transfer')
  return data
}

/**
 * 获取当前用户的平台限额 + 用量。
 */
export async function getMyPlatformQuotas(): Promise<PlatformQuotasResponse> {
  const { data } = await apiClient.get<PlatformQuotasResponse>('/user/platform-quotas')
  return data
}

export const userAPI = {
  getProfile,
  checkWelcomeReward,
  claimWelcomeReward,
  checkSurpriseReward,
  claimSurpriseReward,
  getPendingRewards,
  viewReward,
  claimReward,
  updateProfile,
  changePassword,
  sendNotifyEmailCode,
  verifyNotifyEmail,
  removeNotifyEmail,
  toggleNotifyEmail,
  sendEmailBindingCode,
  bindEmailIdentity,
  unbindAuthIdentity,
  buildOAuthBindingStartURL,
  startOAuthBinding,
  getAffiliateDetail,
  transferAffiliateQuota,
  getMyPlatformQuotas,
  listMerchantIntegrations,
  launchMerchantIntegration,
  listMerchantBindings,
}

export default userAPI
