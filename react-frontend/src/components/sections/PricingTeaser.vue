<script setup lang="ts">
import { ref } from 'vue'
import { pricingPlans } from '../../data/homepage'
import { useGsapReveal } from '../../composables/useGsapReveal'
import CTAButton from '../ui/CTAButton.vue'
import GlassCard from '../ui/GlassCard.vue'
import SectionHeading from '../ui/SectionHeading.vue'

const sectionRef = ref<HTMLElement | null>(null)

useGsapReveal(sectionRef, [
  { selector: '.section-motion-heading', start: 'top 84%', duration: 0.9 },
  { selector: '.section-motion-card', start: 'top 82%', y: 42, stagger: 0.12 },
  { selector: '.section-motion-cta', trigger: '.section-motion-cta', start: 'top 86%', y: 20, scale: 0.96, duration: 0.68 },
])
</script>

<template>
  <section ref="sectionRef" id="pricing" class="relative overflow-hidden bg-ink px-5 py-20 text-pearl sm:px-6 sm:py-28">
    <div class="absolute inset-0 bg-[radial-gradient(circle_at_20%_20%,rgba(240,184,157,0.18),transparent_32%),radial-gradient(circle_at_80%_68%,rgba(189,173,235,0.14),transparent_34%)]" aria-hidden="true" />
    <div class="pricing-grain absolute inset-0" aria-hidden="true" />

    <div class="relative mx-auto max-w-7xl">
      <div class="section-motion-heading flex flex-col justify-between gap-8 md:flex-row md:items-end">
        <SectionHeading
          eyebrow="价格"
          title="先用占位方案表达服务层级。"
          description="方案用于展示信息层级，正式上线前应替换为真实额度、计费方式和服务边界。"
          tone="dark"
        />
        <p class="max-w-md text-sm leading-7 text-pearl/56">
          以下价格与额度均为原型占位，正式上线前应替换为真实计费规则、服务范围和合同信息。
        </p>
      </div>

      <div class="mt-14 grid gap-4 lg:grid-cols-3">
        <GlassCard
          v-for="plan in pricingPlans"
          :key="plan.name"
          class="section-motion-card p-6 sm:p-8"
          :tone="plan.featured ? 'featured' : 'dark'"
        >
          <p
            class="text-sm font-semibold"
            :class="plan.featured ? 'text-charcoal/56' : 'text-pearl/52'"
          >
            {{ plan.label }}
          </p>
          <h3
            class="mt-4 text-2xl font-semibold"
            :class="plan.featured ? 'text-ink' : 'text-pearl'"
          >
            {{ plan.name }}
          </h3>
          <p
            class="mt-6 text-[clamp(2.2rem,4vw,3.5rem)] font-semibold leading-none"
            :class="plan.featured ? 'text-ink' : 'text-pearl'"
          >
            {{ plan.price }}
          </p>
          <p
            class="mt-5 text-sm leading-7"
            :class="plan.featured ? 'text-charcoal/66' : 'text-pearl/62'"
          >
            {{ plan.description }}
          </p>
          <ul class="mt-8 space-y-3">
            <li
              v-for="feature in plan.features"
              :key="feature"
              class="flex items-start gap-3 text-sm leading-6"
              :class="plan.featured ? 'text-charcoal/66' : 'text-pearl/62'"
            >
              <span
                class="mt-2 h-1.5 w-1.5 flex-none rounded-full"
                :class="plan.featured ? 'bg-peach' : 'bg-champagne'"
                aria-hidden="true"
              />
              <span>{{ feature }}</span>
            </li>
          </ul>
        </GlassCard>
      </div>

      <div class="section-motion-cta mt-12 flex justify-center">
        <CTAButton href="#faq">查看服务细节</CTAButton>
      </div>
    </div>
  </section>
</template>

<style scoped>
.pricing-grain {
  opacity: 0.13;
  background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 180 180' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='180' height='180' filter='url(%23n)' opacity='0.5'/%3E%3C/svg%3E");
  mix-blend-mode: soft-light;
}
</style>
