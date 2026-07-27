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
interface PendingWelcomeReward {
  amount: number
  skinId: WelcomeRewardSkinId
}

const pendingWelcomeReward = ref<PendingWelcomeReward | null>(null)
const pendingWelcomeRewardKey = 'pending_welcome_reward'

function loadPendingWelcomeReward() {
  const raw = localStorage.getItem(pendingWelcomeRewardKey)
  try {
    const reward = raw
      ? JSON.parse(raw) as { amount?: unknown; user_id?: unknown; skin_id?: unknown }
      : null
    const amount = reward?.amount
    if (
      typeof amount === 'number' &&
      Number.isInteger(amount) &&
      amount >= 1 &&
      amount <= 5 &&
      reward?.user_id === authStore.user?.id
    ) {
      const skinId = isWelcomeRewardSkinId(reward?.skin_id)
        ? reward.skin_id
        : pickWelcomeRewardSkinId()
      pendingWelcomeReward.value = { amount, skinId }
      if (reward?.skin_id !== skinId) {
        localStorage.setItem(
          pendingWelcomeRewardKey,
          JSON.stringify({ ...reward, skin_id: skinId })
        )
      }
      return
    }
  } catch {
    // Invalid or stale reward markers are cleared below.
  }
  localStorage.removeItem(pendingWelcomeRewardKey)
}

function finishWelcomeReward() {
  localStorage.removeItem(pendingWelcomeRewardKey)
  pendingWelcomeReward.value = null
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
    loadPendingWelcomeReward()
    announcementStore.fetchAnnouncements()
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)
})

onMounted(async () => {
  loadPendingWelcomeReward()
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
    v-if="pendingWelcomeReward !== null && authStore.isAuthenticated && !authStore.isAdmin"
    :show="true"
    :amount="pendingWelcomeReward.amount"
    :skin-id="pendingWelcomeReward.skinId"
    @finish="finishWelcomeReward"
  />
</template>
