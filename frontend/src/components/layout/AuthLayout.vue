<template>
  <div class="landing-shell">
    <main class="auth-shell" :style="authStyle">
      <HeroCurveLines />

      <section class="auth-visual" aria-label="星链账号服务">
        <img class="auth-visual-image" :src="HERO_BACKGROUND_IMAGE" alt="" />
        <div class="auth-visual-content auth-reveal">
          <router-link class="auth-brand" to="/" :aria-label="`${brandName} home`">
            <span class="auth-brand-word">{{ brandName }}</span>
            <span class="auth-brand-submark">API</span>
          </router-link>

          <div class="auth-visual-copy">
            <p>{{ side.eyebrow }}</p>
            <h1>{{ side.title }}</h1>
            <span>{{ side.subtitle }}</span>
          </div>

          <div class="auth-feature-list" aria-label="服务能力">
            <span v-for="feature in side.features" :key="feature" class="auth-feature-pill">
              {{ feature }}
            </span>
          </div>
        </div>
      </section>

      <section class="auth-panel" :aria-labelledby="title ? 'auth-title' : undefined">
        <div class="auth-card auth-reveal auth-reveal--delay">
          <span class="fx-border-beam" aria-hidden="true" />

          <div class="auth-mobile-top">
            <router-link class="auth-back-link" to="/">
              <svg
                aria-hidden="true"
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <path d="m12 19-7-7 7-7" />
                <path d="M19 12H5" />
              </svg>
              返回首页
            </router-link>
            <span class="auth-mobile-brand">{{ brandName }}</span>
          </div>

          <div v-if="eyebrow || title || subtitle" class="auth-header">
            <p v-if="eyebrow">{{ eyebrow }}</p>
            <h2 v-if="title" id="auth-title">{{ title }}</h2>
            <span v-if="subtitle">{{ subtitle }}</span>
          </div>

          <slot />

          <div v-if="$slots.footer" class="auth-footer-link">
            <slot name="footer" />
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import '@/styles/landing.css'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import HeroCurveLines from '@/components/landing/HeroCurveLines.vue'
import { authSideCopy, brandName, HERO_BACKGROUND_IMAGE } from '@/data/landing'
import { applyLandingSeo } from '@/utils/landingSeo'
import { useLandingLightTheme } from '@/composables/useLandingTheme'

defineProps<{
  /** 卡片头部：小标（如 Welcome back）。缺省时由插槽内容自带标题。 */
  eyebrow?: string
  title?: string
  subtitle?: string
}>()

const route = useRoute()

useLandingLightTheme()

// 左侧视觉面板文案按路由选择；回调页 / 邮箱验证等使用登录文案
const side = computed(() => {
  if (route.path === '/register') return authSideCopy.register
  if (route.path === '/forgot-password' || route.path === '/reset-password') {
    return authSideCopy['reset-password']
  }
  return authSideCopy.login
})

const authStyle = {
  '--auth-bg-image': `url("${HERO_BACKGROUND_IMAGE}")`
}

const SEO_PATHS = ['/login', '/register', '/forgot-password', '/reset-password']

onMounted(() => {
  if (SEO_PATHS.includes(route.path)) {
    applyLandingSeo(route.path)
  }
})
</script>
