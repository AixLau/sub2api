/**
 * Pinia Stores Export
 * Central export point for all application stores
 */

export { useAuthStore } from './auth'
export { useAppStore, DEFAULT_DOC_URL } from './app'
export { useAdminSettingsStore } from './adminSettings'
export { useSubscriptionStore } from './subscriptions'
export { useOnboardingStore } from './onboarding'
export { useAnnouncementStore } from './announcements'
export { usePaymentStore } from './payment'
export { useAdminComplianceStore } from './adminCompliance'
export { REWARD_QUEUE_OPEN_EVENT, useRewardStore } from './rewards'

// Re-export types for convenience
export type { User, LoginRequest, RegisterRequest, AuthResponse } from '@/types'
export type { Toast, ToastType, AppState } from '@/types'
