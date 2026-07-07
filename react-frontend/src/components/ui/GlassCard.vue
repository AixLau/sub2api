<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import gsap from 'gsap'

withDefaults(
  defineProps<{
    tone?: 'light' | 'dark' | 'featured'
  }>(),
  {
    tone: 'light',
  },
)

const cardRef = ref<HTMLElement | null>(null)
let cleanupHover: (() => void) | undefined

onMounted(() => {
  const card = cardRef.value
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

  if (!card || reduceMotion) {
    return
  }

  const enter = () => {
    gsap.to(card, {
      y: -7,
      scale: 1.01,
      duration: 0.42,
      ease: 'power3.out',
    })
  }

  const leave = () => {
    gsap.to(card, {
      y: 0,
      scale: 1,
      duration: 0.5,
      ease: 'power3.out',
    })
  }

  card.addEventListener('pointerenter', enter)
  card.addEventListener('pointerleave', leave)
  card.addEventListener('blur', leave, true)
  cleanupHover = () => {
    card.removeEventListener('pointerenter', enter)
    card.removeEventListener('pointerleave', leave)
    card.removeEventListener('blur', leave, true)
    gsap.killTweensOf(card)
  }
})

onBeforeUnmount(() => {
  cleanupHover?.()
})
</script>

<template>
  <article
    ref="cardRef"
    class="glass-card"
    :class="{
      'glass-card--light': tone === 'light',
      'glass-card--dark': tone === 'dark',
      'glass-card--featured': tone === 'featured',
    }"
  >
    <slot />
  </article>
</template>

<style scoped>
.glass-card {
  position: relative;
  overflow: hidden;
  border-radius: 0.5rem;
  transition:
    border-color 500ms ease,
    background 500ms ease,
    box-shadow 500ms ease;
  will-change: transform;
}

.glass-card::before {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    radial-gradient(circle at 22% 0%, rgba(255, 249, 239, 0.48), transparent 26%),
    linear-gradient(135deg, rgba(255, 249, 239, 0.18), transparent 42%);
  opacity: 0.7;
}

.glass-card--light {
  border: 1px solid rgba(26, 23, 20, 0.08);
  background:
    linear-gradient(145deg, rgba(255, 249, 239, 0.82), rgba(246, 239, 229, 0.52)),
    radial-gradient(circle at 90% 0%, rgba(232, 180, 198, 0.18), transparent 32%);
  box-shadow: 0 24px 70px rgba(77, 58, 31, 0.11);
}

.glass-card--light:hover {
  border-color: rgba(26, 23, 20, 0.14);
  box-shadow: 0 30px 82px rgba(77, 58, 31, 0.15);
}

.glass-card--dark {
  border: 1px solid rgba(255, 249, 239, 0.14);
  background:
    linear-gradient(145deg, rgba(255, 249, 239, 0.12), rgba(255, 249, 239, 0.04)),
    radial-gradient(circle at 85% 0%, rgba(240, 184, 157, 0.18), transparent 34%);
  box-shadow: 0 28px 80px rgba(15, 14, 12, 0.24);
  color: #fff9ef;
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
}

.glass-card--featured {
  border: 1px solid rgba(240, 184, 157, 0.34);
  background:
    radial-gradient(circle at 22% 0%, rgba(255, 249, 239, 0.72), transparent 26%),
    radial-gradient(circle at 100% 12%, rgba(240, 184, 157, 0.32), transparent 36%),
    linear-gradient(145deg, rgba(255, 249, 239, 0.92), rgba(246, 239, 229, 0.62));
  box-shadow: 0 30px 86px rgba(240, 184, 157, 0.22);
}

.glass-card > :deep(*) {
  position: relative;
  z-index: 1;
}
</style>
