<script setup lang="ts">
import { RouterView, useRouter, useRoute } from 'vue-router'
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import AdminComplianceDialog from '@/components/admin/AdminComplianceDialog.vue'
import RewardGrantDialog from '@/components/auth/RewardGrantDialog.vue'
import WelcomeRewardDialog from '@/components/auth/WelcomeRewardDialog.vue'
import { resolveRouteDocumentTitle } from '@/router/title'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import {
  REWARD_QUEUE_OPEN_EVENT,
  useAdminComplianceStore,
  useAdminSettingsStore,
  useAnnouncementStore,
  useAppStore,
  useAuthStore,
  useRewardStore,
  useSubscriptionStore,
} from '@/stores'
import { getSetupStatus } from '@/api/setup'
import { updateFavicon } from '@/utils/branding'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import type { RewardClaimResponse } from '@/types'
import {
  welcomeRewardSkins,
  type WelcomeRewardSkinId,
} from '@/components/auth/welcomeRewardSkins'

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const adminSettingsStore = useAdminSettingsStore()
const rewardStore = useRewardStore()
const rewardCampaignsEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.rewardCampaigns))
let rewardDateBoundaryTimer: ReturnType<typeof setTimeout> | null = null
let rewardPollTimer: ReturnType<typeof setInterval> | null = null
let legacyRewardCheck: {
  userID: number
  generation: number
  promise: Promise<void>
} | null = null
let legacyRewardGeneration = 0
const legacyReward = ref<{
  userID: number
  variant: 'welcome' | 'surprise'
  skinID: WelcomeRewardSkinId
} | null>(null)
const legacyFallbackSkinID = welcomeRewardSkins[0]!.id

function invalidateLegacyRewards() {
  legacyRewardGeneration++
  legacyRewardCheck = null
  legacyReward.value = null
  authStore.clearPendingWelcomeReward()
}

function isLegacyRewardContextCurrent(userID: number, generation: number): boolean {
  return (
    generation === legacyRewardGeneration &&
    appStore.publicSettingsLoaded &&
    !rewardCampaignsEnabled.value &&
    authStore.isAuthenticated &&
    !authStore.isAdmin &&
    authStore.user?.id === userID
  )
}

async function refreshRewards(force = false): Promise<void> {
  const userID = authStore.user?.id
  if (
    !appStore.publicSettingsLoaded ||
    !rewardCampaignsEnabled.value ||
    !authStore.isAuthenticated ||
    authStore.isAdmin ||
    !userID
  ) {
    rewardStore.reset()
    return
  }
  try {
    await rewardStore.fetchPending(userID, force)
  } catch (error) {
    console.error('Failed to fetch pending rewards:', error)
  }
}

async function refreshLegacyRewards(): Promise<void> {
  const userID = authStore.user?.id
  if (
    !appStore.publicSettingsLoaded ||
    rewardCampaignsEnabled.value ||
    !authStore.isAuthenticated ||
    authStore.isAdmin ||
    !userID
  ) {
    return
  }
  if (legacyReward.value?.userID === userID) return

  const generation = legacyRewardGeneration
  const existingCheck = legacyRewardCheck
  if (existingCheck?.userID === userID && existingCheck.generation === generation) {
    await existingCheck.promise
    return
  }

  const request = (async () => {
    try {
      const welcomePending = await authStore.checkWelcomeReward()
      if (!isLegacyRewardContextCurrent(userID, generation)) return
      if (welcomePending) {
        legacyReward.value = { userID, variant: 'welcome', skinID: legacyFallbackSkinID }
        return
      }

      const surprisePending = await authStore.checkSurpriseReward()
      if (!isLegacyRewardContextCurrent(userID, generation)) return
      if (surprisePending) {
        legacyReward.value = { userID, variant: 'surprise', skinID: legacyFallbackSkinID }
      }
    } catch (error) {
      if (isLegacyRewardContextCurrent(userID, generation)) {
        console.error('Failed to check legacy rewards:', error)
      }
    }
  })()
  const check = { userID, generation, promise: request }
  legacyRewardCheck = check
  try {
    await request
  } finally {
    if (legacyRewardCheck === check) legacyRewardCheck = null
  }
}

function finishLegacyReward() {
  const completedVariant = legacyReward.value?.variant
  legacyReward.value = null
  if (completedVariant === 'welcome') void refreshLegacyRewards()
}

function scheduleRewardDateBoundary() {
  if (rewardDateBoundaryTimer !== null) {
    clearTimeout(rewardDateBoundaryTimer)
    rewardDateBoundaryTimer = null
  }
  if (
    !appStore.publicSettingsLoaded ||
    !rewardCampaignsEnabled.value ||
    !authStore.isAuthenticated ||
    authStore.isAdmin
  ) return

  const now = new Date()
  const nextDay = new Date(now)
  nextDay.setHours(24, 0, 1, 0)
  rewardDateBoundaryTimer = setTimeout(() => {
    void refreshRewards(true)
    scheduleRewardDateBoundary()
  }, Math.max(1_000, nextDay.getTime() - now.getTime()))
}

function scheduleRewardPolling() {
  if (rewardPollTimer !== null) clearInterval(rewardPollTimer)
  rewardPollTimer = null
  if (
    !appStore.publicSettingsLoaded ||
    !authStore.isAuthenticated ||
    authStore.isAdmin ||
    !authStore.user?.id
  ) return

  rewardPollTimer = setInterval(() => {
    if (rewardCampaignsEnabled.value) {
      void refreshRewards(true)
    } else {
      void refreshLegacyRewards()
    }
  }, 5 * 60 * 1000)
}

function openRewardQueue() {
  rewardStore.reopen()
}

function applyClaimedReward(result: RewardClaimResponse) {
  void authStore.applyRewardBalance(result.balance).catch((error) => {
    console.error('Failed to refresh balance after claiming reward:', error)
  })
}

function updateDocumentTitle() {
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems)
}

// Watch for site settings changes and update favicon/title
watch(
  () => appStore.siteLogo,
  (newLogo) => {
    if (newLogo) {
      updateFavicon(newLogo)
    }
  },
  { immediate: true }
)

watch(
  [
    () => route.fullPath,
    () => route.meta.title,
    () => route.meta.titleKey,
    () => appStore.siteName,
    () => appStore.cachedPublicSettings?.custom_menu_items,
    () => authStore.isAdmin,
    () => adminSettingsStore.customMenuItems,
  ],
  updateDocumentTitle,
  { deep: true }
)

// Watch for authentication state and manage subscription data + announcements
function onVisibilityChange() {
  if (document.visibilityState === 'visible' && authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
    if (appStore.publicSettingsLoaded) {
      if (rewardCampaignsEnabled.value) void refreshRewards(true)
      else void refreshLegacyRewards()
    }
    scheduleRewardDateBoundary()
  }
}

function onAdminComplianceRequired(event: Event) {
  const detail = (event as CustomEvent<Record<string, string>>).detail || {}
  adminComplianceStore.requireAcknowledgement(detail)
}

watch(
  () => authStore.isAuthenticated,
  (isAuthenticated, oldValue) => {
    if (isAuthenticated) {
      if (authStore.isAdmin) {
        adminComplianceStore.fetchStatus().catch((error) => {
          console.error('Failed to fetch admin compliance status:', error)
        })
      }

      // User logged in: preload subscriptions and start polling
      subscriptionStore.fetchActiveSubscriptions().catch((error) => {
        console.error('Failed to preload subscriptions:', error)
      })
      subscriptionStore.startPolling()

      // Announcements: new login vs page refresh restore
      if (oldValue === false) {
        // New login: delay 3s then force fetch
        setTimeout(() => announcementStore.fetchAnnouncements(true), 3000)
      } else {
        // Page refresh restore (oldValue was undefined)
        announcementStore.fetchAnnouncements()
      }

      // Register visibility change listener
      document.addEventListener('visibilitychange', onVisibilityChange)
    } else {
      // User logged out: clear data and stop polling
      rewardStore.reset()
      invalidateLegacyRewards()
      subscriptionStore.clear()
      announcementStore.reset()
      adminComplianceStore.reset()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  },
  { immediate: true }
)

// Route change trigger (throttled by store)
const removeRewardRouteHook = router.afterEach(() => {
  if (authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
    if (appStore.publicSettingsLoaded) {
      if (rewardCampaignsEnabled.value) void refreshRewards()
      else void refreshLegacyRewards()
    }
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)
  window.removeEventListener(REWARD_QUEUE_OPEN_EVENT, openRewardQueue)
  removeRewardRouteHook()
  invalidateLegacyRewards()
  if (rewardDateBoundaryTimer !== null) clearTimeout(rewardDateBoundaryTimer)
  if (rewardPollTimer !== null) clearInterval(rewardPollTimer)
})

watch(
  [
    () => authStore.user?.id,
    () => authStore.isAuthenticated,
    () => authStore.isAdmin,
    () => appStore.publicSettingsLoaded,
    () => rewardCampaignsEnabled.value,
  ],
  () => {
    rewardStore.reset()
    invalidateLegacyRewards()
    if (
      appStore.publicSettingsLoaded &&
      authStore.isAuthenticated &&
      !authStore.isAdmin &&
      authStore.user?.id
    ) {
      if (rewardCampaignsEnabled.value) {
        void refreshRewards(true)
      } else {
        void refreshLegacyRewards()
      }
    }
    scheduleRewardDateBoundary()
    scheduleRewardPolling()
  },
  { immediate: true }
)

onMounted(async () => {
  window.addEventListener('admin-compliance-required', onAdminComplianceRequired)
  window.addEventListener(REWARD_QUEUE_OPEN_EVENT, openRewardQueue)

  // Check if setup is needed
  try {
    const status = await getSetupStatus()
    if (status.needs_setup && route.path !== '/setup') {
      router.replace('/setup')
      return
    }
  } catch {
    // If setup endpoint fails, assume normal mode and continue
  }

  // Load public settings into appStore (will be cached for other components)
  await appStore.fetchPublicSettings()

  // Re-resolve document title now that site settings are available
  updateDocumentTitle()
})
</script>

<template>
  <NavigationProgress />
  <RouterView />
  <Toast />
  <AnnouncementPopup />
  <AdminComplianceDialog />
  <RewardGrantDialog
    v-if="
      appStore.publicSettingsLoaded &&
      rewardCampaignsEnabled &&
      rewardStore.currentGrant &&
      authStore.isAuthenticated &&
      !authStore.isAdmin
    "
    :key="rewardStore.currentGrant.grant_id"
    :show="true"
    :grant="rewardStore.currentGrant"
    @defer="rewardStore.deferCurrent"
    @claimed="applyClaimedReward"
    @finish="rewardStore.finishCurrent"
  />
  <WelcomeRewardDialog
    v-if="
      appStore.publicSettingsLoaded &&
      !rewardCampaignsEnabled &&
      legacyReward?.userID === authStore.user?.id &&
      authStore.isAuthenticated &&
      !authStore.isAdmin
    "
    :key="`${legacyReward?.userID}:${legacyReward?.variant}`"
    :show="true"
    :skin-id="legacyReward?.skinID ?? legacyFallbackSkinID"
    :variant="legacyReward?.variant ?? 'welcome'"
    @finish="finishLegacyReward"
  />
</template>
