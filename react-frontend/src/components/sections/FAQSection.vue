<script setup lang="ts">
import { ref } from 'vue'
import { faqs } from '../../data/homepage'
import { useGsapReveal } from '../../composables/useGsapReveal'
import SectionHeading from '../ui/SectionHeading.vue'

const openIndex = ref(0)
const sectionRef = ref<HTMLElement | null>(null)

function toggleFAQ(index: number) {
  openIndex.value = openIndex.value === index ? -1 : index
}

useGsapReveal(sectionRef, [
  { selector: '.section-motion-heading', start: 'top 84%', duration: 0.9 },
  { selector: '.section-motion-card', start: 'top 82%', y: 34, stagger: 0.08, duration: 0.72 },
])
</script>

<template>
  <section ref="sectionRef" id="faq" class="relative overflow-hidden bg-pearl px-5 py-20 sm:px-6 sm:py-28">
    <div class="absolute -left-28 bottom-0 h-80 w-80 rounded-full bg-peach/18 blur-3xl" aria-hidden="true" />
    <div class="absolute right-0 top-14 h-80 w-80 rounded-full bg-lavender/16 blur-3xl" aria-hidden="true" />

    <div class="relative mx-auto grid max-w-7xl gap-10 lg:grid-cols-[0.72fr_1fr]">
      <div class="section-motion-heading">
        <SectionHeading
          eyebrow="FAQ"
          title="常见问题，保持真实。"
          description="不使用夸张承诺，也不暗示未经验证的官方合作关系。"
        />
      </div>

      <div class="space-y-3">
        <article
          v-for="(item, index) in faqs"
          :key="item.question"
          class="section-motion-card overflow-hidden rounded-lg border border-ink/8 bg-cream/66 shadow-[0_18px_56px_rgba(77,58,31,0.1)] backdrop-blur-xl transition duration-500 hover:border-ink/14 hover:bg-cream/88"
        >
          <button
            type="button"
            class="flex w-full items-center justify-between gap-4 px-5 py-5 text-left focus:outline-none focus:ring-2 focus:ring-ink/18 sm:px-6"
            :aria-expanded="openIndex === index"
            :aria-controls="`faq-panel-${index}`"
            @click="toggleFAQ(index)"
          >
            <span class="text-base font-semibold leading-7 text-ink">{{ item.question }}</span>
            <span class="flex h-8 w-8 flex-none items-center justify-center rounded-full bg-ink text-lg leading-none text-pearl">
              {{ openIndex === index ? '-' : '+' }}
            </span>
          </button>
          <div
            v-show="openIndex === index"
            :id="`faq-panel-${index}`"
            class="px-5 pb-6 text-sm leading-7 text-charcoal/68 sm:px-6"
          >
            {{ item.answer }}
          </div>
        </article>
      </div>
    </div>
  </section>
</template>
