<template>
  <div class="min-h-screen bg-surface-canvas text-content-primary">
    <!-- Background Decoration -->
    <div
      v-if="!route.meta.hideBackgroundMesh"
      class="pointer-events-none fixed inset-0 bg-mesh-gradient"
    ></div>

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-52']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main :class="route.meta.fullBleedContent === true ? 'p-0' : 'p-4 md:p-6 lg:p-8'">
        <Transition name="console-page" :css="!prefersReducedMotion" appear>
          <div :key="route.path">
            <slot />
          </div>
        </Transition>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { usePrefersReducedMotion } from '@/composables/usePrefersReducedMotion'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const appStore = useAppStore()
const authStore = useAuthStore()
const route = useRoute()
const { prefersReducedMotion } = usePrefersReducedMotion()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>

<style scoped>
.console-page-enter-active {
  transition:
    opacity 150ms ease-out,
    transform 150ms ease-out;
}

.console-page-enter-from {
  opacity: 0;
  transform: translateY(4px);
}

@media (prefers-reduced-motion: reduce) {
  .console-page-enter-active {
    transition: none;
  }

  .console-page-enter-from {
    opacity: 1;
    transform: none;
  }
}
</style>
