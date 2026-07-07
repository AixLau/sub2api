<script setup lang="ts">
import SiteFooter from './components/layout/SiteFooter.vue'
import SiteHeader from './components/layout/SiteHeader.vue'
import FAQSection from './components/sections/FAQSection.vue'
import FeatureSection from './components/sections/FeatureSection.vue'
import FinalCTA from './components/sections/FinalCTA.vue'
import HeroSection from './components/sections/HeroSection.vue'
import HowItWorksSection from './components/sections/HowItWorksSection.vue'
import PricingTeaser from './components/sections/PricingTeaser.vue'
import ValuePills from './components/sections/ValuePills.vue'
</script>

<template>
  <div class="min-h-screen overflow-hidden bg-cream text-ink">
    <SiteHeader />
    <main>
      <HeroSection />
      <ValuePills />
      <FeatureSection />
      <HowItWorksSection />
      <PricingTeaser />
      <FAQSection />
      <FinalCTA />
    </main>
    <SiteFooter />
  </div>
</template>
