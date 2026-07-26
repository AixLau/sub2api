<script setup lang="ts">
import { computed, onBeforeMount, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore, useAppStore } from '@/stores'
import LandingShell from '@/components/landing/LandingShell.vue'
import HeroSection from '@/components/landing/HeroSection.vue'
import TrustedBySection from '@/components/landing/TrustedBySection.vue'
import { applyLandingSeo } from '@/utils/landingSeo'

const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

// 管理后台可通过 home_content 设置整体替换首页（URL → iframe，HTML → 直接渲染）
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// 与线上落地页行为一致：已登录用户访问首页直接进入对应控制台
onBeforeMount(() => {
  authStore.checkAuth()
  if (authStore.isAuthenticated) {
    router.replace(authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
  }
})

onMounted(() => {
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
  applyLandingSeo('/')
})
</script>

<template>
  <div v-if="homeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <LandingShell v-else variant="home">
    <HeroSection />
    <TrustedBySection />
  </LandingShell>
</template>
