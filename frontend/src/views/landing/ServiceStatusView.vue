<template>
  <LandingShell>
    <StatusSection />
  </LandingShell>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import LandingShell from '@/components/landing/LandingShell.vue'
import StatusSection from '@/components/landing/StatusSection.vue'
import { applyLandingSeo } from '@/utils/landingSeo'

onMounted(() => {
  applyLandingSeo('/service-status')
})
</script>
