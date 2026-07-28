<script setup lang="ts">
import { RouterView, useRouter, useRoute } from 'vue-router'
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import AdminComplianceDialog from '@/components/admin/AdminComplianceDialog.vue'
import WelcomeRewardDialog from '@/components/auth/WelcomeRewardDialog.vue'
import { resolveRouteDocumentTitle } from '@/router/title'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import { useAppStore, useAuthStore, useSubscriptionStore, useAnnouncementStore, useAdminComplianceStore, useAdminSettingsStore } from '@/stores'
import { getSetupStatus } from '@/api/setup'
import { updateFavicon } from '@/utils/branding'
import {
  isWelcomeRewardSkinId,
  pickWelcomeRewardSkinId,
  type WelcomeRewardSkinId
} from '@/components/auth/welcomeRewardSkins'

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const adminSettingsStore = useAdminSettingsStore()
const pendingWelcomeRewardSkin = ref<WelcomeRewardSkinId | null>(null)
const pendingSurpriseRewardSkin = ref<WelcomeRewardSkinId | null>(null)
const pendingWelcomeRewardKey = 'pending_welcome_reward'
const pendingSurpriseRewardKey = 'pending_surprise_reward'
let surpriseRewardCheckedUserID: number | null = null

function openPendingWelcomeReward() {
  if (
    !authStore.isAuthenticated ||
    authStore.isAdmin ||
    authStore.pendingWelcomeRewardUserID !== authStore.user?.id
  ) {
    return
  }
  const raw = localStorage.getItem(pendingWelcomeRewardKey)
  try {
    const reward = raw ? JSON.parse(raw) as { user_id?: unknown; skin_id?: unknown } : null
    const skinId = isWelcomeRewardSkinId(reward?.skin_id)
      ? reward.skin_id
      : pickWelcomeRewardSkinId()
    pendingWelcomeRewardSkin.value = skinId
    if (reward?.skin_id !== skinId) {
      localStorage.setItem(
        pendingWelcomeRewardKey,
        JSON.stringify({ ...reward, user_id: authStore.user?.id, skin_id: skinId })
      )
    }
  } catch {
    const skinId = pickWelcomeRewardSkinId()
    pendingWelcomeRewardSkin.value = skinId
    localStorage.setItem(
      pendingWelcomeRewardKey,
      JSON.stringify({ user_id: authStore.user?.id, skin_id: skinId })
    )
  }
}

function finishWelcomeReward() {
  pendingWelcomeRewardSkin.value = null
}

function getPersistedRewardSkin(key: string, userID: number): WelcomeRewardSkinId {
  const raw = localStorage.getItem(key)
  try {
    const reward = raw ? JSON.parse(raw) as { user_id?: unknown; skin_id?: unknown } : null
    if (reward?.user_id === userID && isWelcomeRewardSkinId(reward.skin_id)) {
      return reward.skin_id
    }
  } catch {
    // Replace malformed local state with a valid skin below.
  }
  return pickWelcomeRewardSkinId()
}

async function checkSurpriseReward() {
  const userID = authStore.user?.id ?? null
  if (!authStore.isAuthenticated || authStore.isAdmin || userID === null) {
    return
  }
  if (surpriseRewardCheckedUserID === userID) {
    return
  }

  surpriseRewardCheckedUserID = userID
  try {
    const pending = await authStore.checkSurpriseReward()
    if (!pending) {
      localStorage.removeItem(pendingSurpriseRewardKey)
      pendingSurpriseRewardSkin.value = null
      return
    }
    const skinId = getPersistedRewardSkin(pendingSurpriseRewardKey, userID)
    pendingSurpriseRewardSkin.value = skinId
    localStorage.setItem(
      pendingSurpriseRewardKey,
      JSON.stringify({ user_id: userID, skin_id: skinId })
    )
  } catch (error) {
    surpriseRewardCheckedUserID = null
    console.error('Failed to check surprise reward:', error)
  }
}

function finishSurpriseReward() {
  pendingSurpriseRewardSkin.value = null
  localStorage.removeItem(pendingSurpriseRewardKey)
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
    checkSurpriseReward()
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
      pendingWelcomeRewardSkin.value = null
      pendingSurpriseRewardSkin.value = null
      surpriseRewardCheckedUserID = null
      subscriptionStore.clear()
      announcementStore.reset()
      adminComplianceStore.reset()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  },
  { immediate: true }
)

// Route change trigger (throttled by store)
router.afterEach(() => {
  if (authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)
})

watch(
  [
    () => authStore.pendingWelcomeRewardUserID,
    () => authStore.user?.id,
    () => authStore.isAuthenticated
  ],
  openPendingWelcomeReward,
  { immediate: true }
)

watch(
  [
    () => authStore.user?.id,
    () => authStore.isAuthenticated,
    () => authStore.isAdmin
  ],
  () => {
    void checkSurpriseReward()
  },
  { immediate: true }
)

onMounted(async () => {
  window.addEventListener('admin-compliance-required', onAdminComplianceRequired)

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
  <WelcomeRewardDialog
    v-if="pendingWelcomeRewardSkin !== null && authStore.isAuthenticated && !authStore.isAdmin"
    :show="true"
    :skin-id="pendingWelcomeRewardSkin"
    @finish="finishWelcomeReward"
  />
  <WelcomeRewardDialog
    v-if="
      pendingWelcomeRewardSkin === null &&
      pendingSurpriseRewardSkin !== null &&
      authStore.isAuthenticated &&
      !authStore.isAdmin
    "
    :show="true"
    :skin-id="pendingSurpriseRewardSkin"
    variant="surprise"
    @finish="finishSurpriseReward"
  />
</template>
