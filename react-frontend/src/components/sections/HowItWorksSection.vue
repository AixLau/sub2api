<script setup lang="ts">
import { ref } from 'vue'
import { steps } from '../../data/homepage'
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
  <section
    ref="sectionRef"
    id="process"
    class="relative overflow-hidden bg-[linear-gradient(180deg,#F6EFE5_0%,#FFF9EF_50%,#EDE0CF_100%)] px-5 py-20 sm:px-6 sm:py-28"
  >
    <div class="absolute left-1/2 top-20 h-[30rem] w-[72rem] -translate-x-1/2 rounded-full bg-[radial-gradient(circle,rgba(232,210,167,0.32),transparent_62%)] blur-2xl" aria-hidden="true" />

    <div class="relative mx-auto max-w-7xl">
      <div class="section-motion-heading">
        <SectionHeading
          eyebrow="接入流程"
          title="从选择能力，到稳定上线。"
          description="流程保持轻盈清楚，让模型能力像服务层一样自然融入产品。"
          align="center"
        />
      </div>

      <div class="mt-14 grid gap-4 lg:grid-cols-3">
        <GlassCard
          v-for="(step, index) in steps"
          :key="step.title"
          class="section-motion-card p-6 sm:p-8"
        >
          <div class="flex items-center justify-between gap-6">
            <span class="text-xs font-bold uppercase tracking-[0.16em] text-charcoal/44">Step</span>
            <span class="text-5xl font-semibold leading-none text-ink/12">0{{ index + 1 }}</span>
          </div>
          <h3 class="mt-8 text-2xl font-semibold leading-tight text-ink">{{ step.title }}</h3>
          <p class="mt-5 text-base leading-8 text-charcoal/66">{{ step.description }}</p>
        </GlassCard>
      </div>
    </div>
  </section>
</template>
