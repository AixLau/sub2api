<template>
  <LandingShell>
    <ServicesSection />
  </LandingShell>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import LandingShell from '@/components/landing/LandingShell.vue'
import ServicesSection from '@/components/landing/ServicesSection.vue'
import { applyLandingSeo } from '@/utils/landingSeo'

onMounted(() => {
  applyLandingSeo('/services')
})
</script>
