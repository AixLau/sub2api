<template>
  <section class="user-dashboard__hero" aria-labelledby="user-dashboard-hero-title">
    <div class="user-dashboard__hero-copy">
      <h1 id="user-dashboard-hero-title" class="user-dashboard__hero-title">
        {{ t('dashboard.hero.welcome', { name: resolvedDisplayName }) }}
      </h1>
      <p class="user-dashboard__hero-subtitle">
        {{ t('dashboard.hero.subtitle') }}
      </p>
    </div>

    <div class="user-dashboard__camera" aria-hidden="true">
      <span class="user-dashboard__camera-glow"></span>
      <img
        class="user-dashboard__camera-image"
        src="/assets/dashboard/camera-fun.webp"
        alt=""
        aria-hidden="true"
        draggable="false"
        width="1254"
        height="1254"
        loading="eager"
        fetchpriority="high"
        decoding="async"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  displayName: string
}>()

const { t } = useI18n()

const resolvedDisplayName = computed(() => props.displayName.trim() || t('dashboard.hero.user'))
</script>
