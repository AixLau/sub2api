<template>
  <div ref="backgroundRef" class="liquid-bloom pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
    <div class="liquid-bloom__base" />
    <div class="liquid-bloom__mesh" />
    <div class="liquid-bloom__blob liquid-bloom__blob--peach" />
    <div class="liquid-bloom__blob liquid-bloom__blob--rose" />
    <div class="liquid-bloom__blob liquid-bloom__blob--sage" />
    <div class="liquid-bloom__glow" />
    <div class="liquid-bloom__sweep" />
    <div class="liquid-bloom__grain" />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import gsap from 'gsap'

const backgroundRef = ref<HTMLElement | null>(null)
let ctx: gsap.Context | undefined
let cleanupMouseMove: (() => void) | undefined

onMounted(() => {
  const root = backgroundRef.value
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

  if (!root || reduceMotion) {
    return
  }

  ctx = gsap.context(() => {
    gsap.to('.liquid-bloom__mesh', {
      rotate: 24,
      scale: 1.36,
      xPercent: 4,
      yPercent: -4,
      duration: 24,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-bloom__blob--peach', {
      xPercent: 12,
      yPercent: 9,
      scale: 1.16,
      rotate: 12,
      duration: 20,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-bloom__blob--rose', {
      xPercent: -11,
      yPercent: -8,
      scale: 1.13,
      rotate: -10,
      duration: 22,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-bloom__blob--sage', {
      xPercent: 9,
      yPercent: -13,
      scale: 1.18,
      duration: 25,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-bloom__glow', {
      opacity: 1,
      scale: 1.04,
      duration: 8,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.fromTo(
      '.liquid-bloom__sweep',
      { xPercent: -35, opacity: 0 },
      {
        xPercent: 255,
        opacity: 0.9,
        duration: 7.4,
        ease: 'sine.inOut',
        repeat: -1,
        repeatDelay: 1.2,
      },
    )
  }, root)

  const canParallax = window.matchMedia('(pointer: fine)').matches
  if (canParallax) {
    const setRootX = gsap.quickTo(root, 'x', { duration: 0.9, ease: 'power3.out' })
    const setRootY = gsap.quickTo(root, 'y', { duration: 0.9, ease: 'power3.out' })
    const mesh = root.querySelector('.liquid-bloom__mesh')
    const setMeshX = mesh ? gsap.quickTo(mesh, 'x', { duration: 1.1, ease: 'power3.out' }) : undefined
    const setMeshY = mesh ? gsap.quickTo(mesh, 'y', { duration: 1.1, ease: 'power3.out' }) : undefined

    const handleMouseMove = (event: MouseEvent) => {
      const x = event.clientX / window.innerWidth - 0.5
      const y = event.clientY / window.innerHeight - 0.5
      setRootX(x * 22)
      setRootY(y * 16)
      setMeshX?.(x * -32)
      setMeshY?.(y * -24)
    }

    window.addEventListener('mousemove', handleMouseMove, { passive: true })
    cleanupMouseMove = () => window.removeEventListener('mousemove', handleMouseMove)
  }
})

onBeforeUnmount(() => {
  cleanupMouseMove?.()
  ctx?.revert()
})
</script>

<style scoped>
.liquid-bloom {
  background: #0f0e0c;
  isolation: isolate;
}

.liquid-bloom__base,
.liquid-bloom__mesh,
.liquid-bloom__glow,
.liquid-bloom__sweep,
.liquid-bloom__grain,
.liquid-bloom__blob {
  position: absolute;
  inset: 0;
}

.liquid-bloom__base {
  background:
    radial-gradient(circle at 18% 16%, rgba(246, 239, 229, 0.18), transparent 30%),
    radial-gradient(circle at 72% 22%, rgba(232, 210, 167, 0.24), transparent 32%),
    radial-gradient(circle at 42% 80%, rgba(189, 173, 235, 0.18), transparent 30%),
    linear-gradient(135deg, #0f0e0c 0%, #1a1714 46%, #2a211b 100%);
  z-index: 0;
}

.liquid-bloom__mesh {
  background:
    conic-gradient(
      from 136deg at 58% 42%,
      rgba(240, 184, 157, 0.18),
      rgba(232, 180, 198, 0.19),
      rgba(189, 173, 235, 0.16),
      rgba(175, 200, 161, 0.12),
      rgba(232, 210, 167, 0.2),
      rgba(240, 184, 157, 0.18)
    );
  filter: blur(32px) saturate(124%);
  opacity: 0.72;
  transform: scale(1.18);
  mix-blend-mode: screen;
  will-change: transform;
  z-index: 1;
}

.liquid-bloom__blob {
  border-radius: 999px;
  filter: blur(42px);
  mix-blend-mode: screen;
  opacity: 0.66;
  will-change: transform;
  z-index: 2;
}

.liquid-bloom__blob--peach {
  inset: 10% auto auto -8%;
  width: min(54vw, 48rem);
  height: min(54vw, 46rem);
  background:
    radial-gradient(circle at 34% 35%, rgba(255, 249, 239, 0.34), transparent 16%),
    radial-gradient(circle at 50% 52%, rgba(240, 184, 157, 0.66), transparent 58%);
}

.liquid-bloom__blob--rose {
  inset: auto 2% 4% auto;
  width: min(48vw, 42rem);
  height: min(44vw, 38rem);
  background:
    radial-gradient(circle at 50% 36%, rgba(232, 180, 198, 0.54), transparent 48%),
    radial-gradient(circle at 55% 62%, rgba(189, 173, 235, 0.42), transparent 60%);
}

.liquid-bloom__blob--sage {
  inset: 42% auto auto 32%;
  width: min(38vw, 34rem);
  height: min(32vw, 28rem);
  background: radial-gradient(circle at 50% 50%, rgba(175, 200, 161, 0.36), transparent 62%);
  opacity: 0.48;
}

.liquid-bloom__glow {
  background:
    radial-gradient(ellipse at 64% 50%, rgba(255, 249, 239, 0.26), transparent 16%),
    radial-gradient(ellipse at 50% 112%, rgba(246, 239, 229, 0.18), transparent 38%);
  filter: blur(3px);
  opacity: 0.9;
  will-change: transform, opacity;
  z-index: 3;
}

.liquid-bloom__sweep {
  width: 58%;
  inset: -25% auto -25% -16%;
  background: linear-gradient(
    90deg,
    transparent,
    rgba(255, 249, 239, 0.04),
    rgba(255, 249, 239, 0.18),
    rgba(232, 210, 167, 0.12),
    transparent
  );
  filter: blur(16px);
  transform: rotate(18deg);
  opacity: 0;
  will-change: transform, opacity;
  z-index: 4;
}

.liquid-bloom__grain {
  opacity: 0.19;
  z-index: 5;
  mix-blend-mode: soft-light;
  background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 180 180' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.82' numOctaves='3' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='180' height='180' filter='url(%23n)' opacity='0.46'/%3E%3C/svg%3E");
}

@media (max-width: 767px) {
  .liquid-bloom__blob--peach {
    width: 34rem;
    height: 34rem;
    inset: 6% auto auto -18rem;
  }

  .liquid-bloom__blob--rose {
    width: 30rem;
    height: 30rem;
    inset: auto -14rem 4% auto;
  }

  .liquid-bloom__blob--sage {
    width: 24rem;
    height: 22rem;
    inset: 50% auto auto 18%;
  }
}

@media (prefers-reduced-motion: reduce) {
  .liquid-bloom__mesh,
  .liquid-bloom__blob,
  .liquid-bloom__glow,
  .liquid-bloom__sweep {
    animation: none;
  }
}
</style>
