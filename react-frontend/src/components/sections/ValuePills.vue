<script setup lang="ts">
import { ref } from 'vue'
import { valuePills } from '../../data/homepage'
import { useGsapReveal } from '../../composables/useGsapReveal'

const sectionRef = ref<HTMLElement | null>(null)

useGsapReveal(sectionRef, [
  { selector: '.value-pill-card', start: 'top 82%', y: 28, scale: 0.96, duration: 0.7, stagger: 0.08 },
])
</script>

<template>
  <section ref="sectionRef" class="relative bg-cream px-5 pb-16 pt-8 sm:px-6 sm:pb-24">
    <div class="mx-auto grid max-w-7xl gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <article
        v-for="item in valuePills"
        :key="item.title"
        class="value-pill-card group relative overflow-hidden rounded-full border border-ink/8 bg-pearl/70 px-5 py-4 shadow-[0_14px_42px_rgba(77,58,31,0.08)] transition duration-500 hover:-translate-y-1 hover:bg-pearl"
      >
        <div class="absolute inset-y-0 left-0 w-20 bg-gradient-to-r from-peach/20 to-transparent opacity-0 transition duration-500 group-hover:opacity-100" />
        <div class="relative">
          <h2 class="text-sm font-semibold text-ink">{{ item.title }}</h2>
          <p class="mt-1 text-sm leading-6 text-charcoal/60">{{ item.description }}</p>
        </div>
      </article>
    </div>
  </section>
</template>
