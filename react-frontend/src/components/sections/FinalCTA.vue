<script setup lang="ts">
import { ref } from 'vue'
import { useGsapReveal } from '../../composables/useGsapReveal'
import CTAButton from '../ui/CTAButton.vue'
import LiquidBloomBackground from '../ui/LiquidBloomBackground.vue'

const sectionRef = ref<HTMLElement | null>(null)

useGsapReveal(sectionRef, [
  { selector: '.final-cta-motion', start: 'top 72%', y: 38, scale: 0.98, duration: 0.9, stagger: 0.1 },
])
</script>

<template>
  <section ref="sectionRef" class="relative overflow-hidden bg-ink px-5 py-20 text-pearl sm:px-6 sm:py-28">
    <LiquidBloomBackground />
    <div class="absolute inset-0 bg-ink/22" aria-hidden="true" />

    <div class="relative z-10 mx-auto max-w-5xl text-center">
      <p class="final-cta-motion text-sm font-semibold uppercase tracking-[0.18em] text-pearl/58">LumaAPI</p>
      <h2 class="final-cta-motion mx-auto mt-5 max-w-4xl text-balance text-[clamp(2.5rem,7vw,6.6rem)] font-bold leading-[1.02] tracking-normal text-pearl">
        用一层更柔和的服务，承接模型能力。
      </h2>
      <p class="final-cta-motion mx-auto mt-7 max-w-2xl text-pretty text-base leading-8 text-pearl/68 sm:text-lg">
        把模型调用整理成稳定、清晰、可支持的体验，从原型验证到产品上线都更从容。
      </p>
      <div class="final-cta-motion mt-10 flex justify-center">
        <CTAButton href="#pricing">开始接入</CTAButton>
      </div>
    </div>
  </section>
</template>
