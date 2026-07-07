<script setup lang="ts">
import { ref } from 'vue'
import { features } from '../../data/homepage'
import { useGsapReveal } from '../../composables/useGsapReveal'
import GlassCard from '../ui/GlassCard.vue'
import SectionHeading from '../ui/SectionHeading.vue'

const sectionRef = ref<HTMLElement | null>(null)

useGsapReveal(sectionRef, [
  { selector: '.section-motion-heading', start: 'top 84%', duration: 0.9 },
  { selector: '.section-motion-card', start: 'top 82%', y: 42, stagger: 0.12 },
])
</script>

<template>
  <section ref="sectionRef" id="services" class="relative overflow-hidden bg-cream px-5 py-20 sm:px-6 sm:py-28">
    <div class="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-ink/10 to-transparent" />
    <div class="absolute -left-32 top-12 h-72 w-72 rounded-full bg-rose/18 blur-3xl" aria-hidden="true" />
    <div class="absolute -right-36 bottom-10 h-80 w-80 rounded-full bg-sage/18 blur-3xl" aria-hidden="true" />

    <div class="relative mx-auto max-w-7xl">
      <div class="section-motion-heading">
        <SectionHeading
          eyebrow="服务"
          title="一层更优雅的模型 API 服务。"
          description="不把复杂性推给产品团队，把模型接入、用量理解和服务协作整理成更自然的使用体验。"
        />
      </div>

      <div id="advantages" class="mt-14 grid gap-4 md:grid-cols-2">
        <GlassCard
          v-for="feature in features"
          :key="feature.title"
          class="section-motion-card min-h-[17rem] p-6 sm:p-8"
        >
          <p class="text-xs font-bold uppercase tracking-[0.16em] text-charcoal/46">{{ feature.eyebrow }}</p>
          <div class="mt-8 h-1.5 w-24 rounded-full bg-gradient-to-r from-peach via-rose to-lavender shadow-[0_0_28px_rgba(240,184,157,0.34)]" />
          <h3 class="mt-8 text-[clamp(1.45rem,2.4vw,2.15rem)] font-semibold leading-tight text-ink">
            {{ feature.title }}
          </h3>
          <p class="mt-5 text-base leading-8 text-charcoal/66">{{ feature.description }}</p>
        </GlassCard>
      </div>
    </div>
  </section>
</template>
