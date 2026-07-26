<template>
  <LandingShell>
    <FaqSection />
  </LandingShell>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import LandingShell from '@/components/landing/LandingShell.vue'
import FaqSection from '@/components/landing/FaqSection.vue'
import { applyLandingSeo } from '@/utils/landingSeo'

onMounted(() => {
  applyLandingSeo('/faq')
})
</script>
