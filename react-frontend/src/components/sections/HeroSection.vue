<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import gsap from 'gsap'
import { heroValuePills } from '../../data/homepage'
import CTAButton from '../ui/CTAButton.vue'
import FloatingValuePill from '../ui/FloatingValuePill.vue'
import LiquidBloomBackground from '../ui/LiquidBloomBackground.vue'
import LiquidGlassObject from '../ui/LiquidGlassObject.vue'

const heroRef = ref<HTMLElement | null>(null)
let ctx: gsap.Context | undefined
const pointerCleanups: Array<() => void> = []

onMounted(() => {
  const root = heroRef.value
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

  if (!root || reduceMotion) {
    return
  }

  ctx = gsap.context(() => {
    gsap.set(['.hero-kicker', '.hero-title-line', '.hero-copy', '.hero-cta', '.hero-pill', '.hero-visual'], {
      transformPerspective: 900,
    })

    const timeline = gsap.timeline({ defaults: { ease: 'power3.out' } })

    timeline
      .from('.hero-stage-bloom', {
        autoAlpha: 0,
        scale: 0.55,
        duration: 1.15,
        ease: 'power4.out',
      })
      .from('.hero-kicker', {
        autoAlpha: 0,
        y: 24,
        scale: 0.96,
        duration: 0.75,
      }, '-=0.72')
      .from(
        '.hero-title-line',
        {
          autoAlpha: 0,
          yPercent: 115,
          rotateX: -28,
          skewY: 2.5,
          duration: 1.08,
          stagger: 0.18,
          ease: 'expo.out',
        },
        '-=0.38',
      )
      .from(
        '.hero-copy',
        {
          autoAlpha: 0,
          y: 30,
          filter: 'blur(10px)',
          duration: 0.86,
        },
        '-=0.36',
      )
      .from(
        '.hero-cta',
        {
          autoAlpha: 0,
          y: 26,
          scale: 0.88,
          duration: 0.72,
          stagger: 0.08,
          ease: 'back.out(1.7)',
        },
        '-=0.12',
      )
      .from(
        '.hero-pill',
        {
          autoAlpha: 0,
          y: 24,
          scale: 0.82,
          rotate: -3,
          duration: 0.7,
          stagger: 0.09,
          ease: 'back.out(1.6)',
        },
        '-=0.24',
      )
      .from(
        '.hero-visual',
        {
          autoAlpha: 0,
          y: 72,
          scale: 0.74,
          rotate: -9,
          filter: 'blur(16px)',
          duration: 1.35,
          ease: 'expo.out',
        },
        '-=0.56',
      )
      .to(
        '.hero-stage-bloom',
        {
          scale: 1.18,
          opacity: 0.88,
          duration: 1.2,
          ease: 'sine.inOut',
        },
        '-=1.1',
      )

    gsap.to('.hero-pill', {
      y: -12,
      rotate: 1.5,
      duration: 3.2,
      stagger: {
        each: 0.24,
        repeat: -1,
        yoyo: true,
      },
      ease: 'sine.inOut',
    })

    const canMagnet = window.matchMedia('(pointer: fine)').matches
    if (canMagnet) {
      const buttons = gsap.utils.toArray<HTMLElement>('.hero-cta')
      buttons.forEach((button) => {
        const moveX = gsap.quickTo(button, 'x', { duration: 0.42, ease: 'power3.out' })
        const moveY = gsap.quickTo(button, 'y', { duration: 0.42, ease: 'power3.out' })
        const scaleTo = gsap.quickTo(button, 'scale', { duration: 0.34, ease: 'power3.out' })

        const handlePointerMove = (event: PointerEvent) => {
          const rect = button.getBoundingClientRect()
          const x = (event.clientX - rect.left - rect.width / 2) * 0.22
          const y = (event.clientY - rect.top - rect.height / 2) * 0.24
          moveX(x)
          moveY(y)
          scaleTo(1.045)
        }

        const handlePointerLeave = () => {
          moveX(0)
          moveY(0)
          scaleTo(1)
        }

        button.addEventListener('pointermove', handlePointerMove)
        button.addEventListener('pointerleave', handlePointerLeave)

        pointerCleanups.push(() => {
          button.removeEventListener('pointermove', handlePointerMove)
          button.removeEventListener('pointerleave', handlePointerLeave)
          gsap.killTweensOf(button)
        })
      })
    }
  }, root)
})

onBeforeUnmount(() => {
  pointerCleanups.splice(0).forEach((cleanup) => cleanup())
  ctx?.revert()
})
</script>

<template>
  <section ref="heroRef" class="relative min-h-[100dvh] overflow-hidden px-5 pb-12 pt-24 text-pearl sm:px-6 sm:pt-32 lg:flex lg:items-center lg:pb-16 lg:pt-28">
    <LiquidBloomBackground />
    <div class="hero-stage-bloom pointer-events-none absolute left-1/2 top-[48%] z-[1] h-[30rem] w-[30rem] -translate-x-1/2 -translate-y-1/2 rounded-full bg-[radial-gradient(circle,rgba(255,249,239,0.22),rgba(240,184,157,0.16)_30%,rgba(189,173,235,0.08)_52%,transparent_70%)] blur-3xl lg:left-[62%]" aria-hidden="true" />

    <div class="absolute inset-x-0 bottom-0 z-[1] h-36 bg-gradient-to-b from-transparent to-cream" aria-hidden="true" />

    <div class="relative z-10 mx-auto grid w-full max-w-7xl items-center gap-7 lg:grid-cols-[0.96fr_1.04fr] lg:gap-12">
      <div class="max-w-3xl animate-fade-up">
        <p class="hero-kicker mb-6 inline-flex rounded-full border border-pearl/18 bg-pearl/8 px-4 py-2 text-sm font-medium text-pearl/72 shadow-[0_14px_46px_rgba(15,14,12,0.18)] backdrop-blur-2xl">
          Liquid Bloom API Service
        </p>
        <h1 class="max-w-[46rem] font-display text-[clamp(2.35rem,5.7vw,5.45rem)] font-bold leading-[1.04] tracking-normal text-pearl">
          <span class="block overflow-hidden pb-1 md:whitespace-nowrap">
            <span class="hero-title-line block">让模型&nbsp;API&nbsp;接入，</span>
          </span>
          <span class="block overflow-hidden pb-1 md:whitespace-nowrap">
            <span class="hero-title-line block">像光一样自然。</span>
          </span>
        </h1>
        <p class="hero-copy mt-6 max-w-2xl text-pretty text-[clamp(1rem,1.7vw,1.25rem)] leading-8 text-pearl/70 sm:mt-7 sm:leading-9">
          通过一套稳定的 API 服务，接入 GPT 等模型能力，减少复杂配置，让团队把注意力留给产品体验。
        </p>

        <div class="mt-8 flex flex-row gap-3 sm:mt-9">
          <CTAButton href="#pricing" class="hero-cta flex-1 px-4 sm:flex-none sm:px-7">开始接入</CTAButton>
          <CTAButton href="#services" variant="secondary" class="hero-cta flex-1 px-4 sm:flex-none sm:px-7">查看服务能力</CTAButton>
        </div>

        <div class="mt-8 flex max-w-2xl flex-wrap gap-2 sm:mt-9 sm:gap-3">
          <span v-for="(pill, index) in heroValuePills" :key="pill.label" class="hero-pill inline-flex">
            <FloatingValuePill
              :label="pill.label"
              :tone="index === 1 ? 'sage' : index === 2 ? 'rose' : index === 3 ? 'warm' : 'pearl'"
            />
          </span>
        </div>
      </div>

      <div class="hero-visual relative mx-auto w-full max-w-[42rem] pb-4 lg:max-w-none">
        <LiquidGlassObject />
      </div>
    </div>
  </section>
</template>
