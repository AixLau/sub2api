<script setup lang="ts">
import { computed, onBeforeMount, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import LandingShell from '@/components/landing/LandingShell.vue'
import HeroSection from '@/components/landing/HeroSection.vue'
import TrustedBySection from '@/components/landing/TrustedBySection.vue'
import { applyLandingSeo } from '@/utils/landingSeo'
import { sanitizeUrl } from '@/utils/url'

const router = useRouter()
const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

// 管理后台可通过 home_content 设置整体替换首页（URL → iframe，HTML → 直接渲染）
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || '')
const siteLogo = computed(() => sanitizeUrl(
  appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '/logo.svg',
  { allowRelative: true, allowDataUrl: true }
))
const dashboardPath = computed(() => authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
const compactDestination = computed(() => authStore.isAuthenticated ? dashboardPath.value : '/login')

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// 与线上落地页行为一致：已登录用户访问首页直接进入对应控制台
onBeforeMount(() => {
  authStore.checkAuth()
  if (authStore.isAuthenticated) {
    router?.replace(authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
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
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white"
  >
    <header class="border-b border-gray-200 px-4 py-4 dark:border-dark-800">
      <nav class="mx-auto flex w-full max-w-5xl items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-3">
          <img :src="siteLogo" alt="Logo" class="h-9 w-9 rounded-lg object-contain" />
          <span class="truncate font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex items-center gap-2">
          <LocaleSwitcher />
          <router-link
            :to="compactDestination"
            class="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-gray-900"
          >
            {{ authStore.isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>
    <main class="flex flex-1 items-center justify-center px-6 py-16 text-center">
      <div class="max-w-2xl">
        <img :src="siteLogo" alt="Logo" class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain" />
        <h1 class="break-words text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link
          :to="compactDestination"
          class="mt-8 inline-flex rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700"
        >
          {{ authStore.isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
          <Icon name="arrowRight" size="sm" class="ml-2" />
        </router-link>
      </div>
    </main>
  </div>

  <LandingShell v-else variant="home">
    <HeroSection />
    <TrustedBySection />
  </LandingShell>
</template>
