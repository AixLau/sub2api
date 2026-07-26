<template>
  <LandingShell>
    <ModelCatalogSection />
  </LandingShell>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import LandingShell from '@/components/landing/LandingShell.vue'
import ModelCatalogSection from '@/components/landing/ModelCatalogSection.vue'
import { applyLandingSeo } from '@/utils/landingSeo'

onMounted(() => {
  applyLandingSeo('/model-market')
})
</script>
