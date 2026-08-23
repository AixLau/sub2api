<template>
  <div class="landing-shell app-shell" :class="{ 'app-shell--home': variant === 'home' }">
    <LandingNavbar :show-model-plaza-entry="showModelPlazaEntry" />
    <main :class="variant === 'home' ? 'home-main' : 'public-page-main'">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import '@/styles/landing.css'
import LandingNavbar from './LandingNavbar.vue'
import { useLandingLightTheme } from '@/composables/useLandingTheme'

useLandingLightTheme()

withDefaults(
  defineProps<{
    variant?: 'home' | 'page'
    showModelPlazaEntry?: boolean
  }>(),
  {
    variant: 'page',
    showModelPlazaEntry: false
  }
)
</script>
