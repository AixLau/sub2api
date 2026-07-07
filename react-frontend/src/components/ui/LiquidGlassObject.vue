<template>
  <div ref="objectRef" class="liquid-object" aria-label="LumaAPI 统一服务层视觉对象">
    <div class="liquid-object__halo" />
    <div class="liquid-object__aura liquid-object__aura--peach" />
    <div class="liquid-object__aura liquid-object__aura--lavender" />
    <div class="liquid-object__petal liquid-object__petal--top" />
    <div class="liquid-object__petal liquid-object__petal--bottom" />

    <div class="liquid-object__body">
      <div class="liquid-object__pearl-core">
        <span />
      </div>
      <div class="liquid-object__shine" />
      <div class="liquid-object__inner">
        <p class="liquid-object__eyebrow">LumaAPI</p>
        <p class="liquid-object__title">统一服务层</p>
        <div class="liquid-object__thread" />
        <div class="liquid-object__labels">
          <span>模型能力</span>
          <span>请求更平滑</span>
          <span>用量更清晰</span>
        </div>
      </div>
    </div>

    <div class="liquid-object__chip liquid-object__chip--one">模型能力</div>
    <div class="liquid-object__chip liquid-object__chip--two">统一服务层</div>
    <div class="liquid-object__chip liquid-object__chip--three">用量更清晰</div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import gsap from 'gsap'

const objectRef = ref<HTMLElement | null>(null)
let ctx: gsap.Context | undefined

onMounted(() => {
  const root = objectRef.value
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

  if (!root || reduceMotion) {
    return
  }

  ctx = gsap.context(() => {
    gsap.to(root, {
      y: -24,
      rotate: 1.8,
      scale: 1.026,
      duration: 6.8,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__pearl-core', {
      scale: 1.12,
      rotate: 9,
      filter: 'blur(0px) brightness(1.12) saturate(1.12)',
      duration: 5.6,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__pearl-core span', {
      xPercent: 18,
      yPercent: -12,
      scale: 1.16,
      duration: 4.8,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__halo', {
      scale: 1.18,
      opacity: 1,
      duration: 5.2,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__aura--peach', {
      xPercent: 12,
      yPercent: -8,
      scale: 1.18,
      opacity: 0.88,
      duration: 7.6,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__aura--lavender', {
      xPercent: -10,
      yPercent: 10,
      scale: 1.16,
      opacity: 0.78,
      duration: 8.2,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__body', {
      filter: 'brightness(1.08) saturate(1.16)',
      boxShadow:
        'inset 0 1px 0 rgba(255, 249, 239, 0.78), inset -28px -34px 90px rgba(15, 14, 12, 0.17), inset 30px 34px 80px rgba(255, 249, 239, 0.22), 0 52px 130px rgba(15, 14, 12, 0.34), 0 26px 90px rgba(240, 184, 157, 0.32)',
      duration: 6.4,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__shine', {
      xPercent: 9,
      opacity: 0.82,
      duration: 6.8,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__thread', {
      scaleX: 1.14,
      opacity: 0.9,
      duration: 4.8,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
      transformOrigin: 'center center',
    })

    gsap.to('.liquid-object__chip--one', {
      x: 22,
      y: -22,
      rotate: -5,
      duration: 6.6,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__chip--two', {
      x: -22,
      y: 18,
      rotate: 5,
      duration: 7.4,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })

    gsap.to('.liquid-object__chip--three', {
      x: -18,
      y: -20,
      rotate: -4,
      duration: 7,
      ease: 'sine.inOut',
      yoyo: true,
      repeat: -1,
    })
  }, root)
})

onBeforeUnmount(() => {
  ctx?.revert()
})
</script>

<style scoped>
.liquid-object {
  position: relative;
  width: min(38rem, 86vw);
  aspect-ratio: 1 / 1.08;
  margin-inline: auto;
  filter: drop-shadow(0 42px 82px rgba(15, 14, 12, 0.38));
  will-change: transform;
}

.liquid-object__halo {
  position: absolute;
  inset: 12% 7% 7%;
  border-radius: 46% 54% 52% 48% / 42% 45% 55% 58%;
  background:
    radial-gradient(circle at 30% 22%, rgba(255, 249, 239, 0.9), transparent 13%),
    radial-gradient(circle at 60% 36%, rgba(240, 184, 157, 0.48), transparent 30%),
    radial-gradient(circle at 52% 64%, rgba(232, 180, 198, 0.46), transparent 36%),
    radial-gradient(circle at 74% 76%, rgba(189, 173, 235, 0.36), transparent 32%);
  filter: blur(30px);
  opacity: 0.86;
  transform-origin: center center;
  will-change: transform, opacity;
}

.liquid-object__aura {
  position: absolute;
  border-radius: 999px;
  filter: blur(18px);
  mix-blend-mode: screen;
  pointer-events: none;
  will-change: transform, opacity;
}

.liquid-object__aura--peach {
  inset: 20% auto auto 2%;
  width: 36%;
  height: 30%;
  background: radial-gradient(circle at 50% 50%, rgba(240, 184, 157, 0.58), transparent 70%);
  opacity: 0.58;
}

.liquid-object__aura--lavender {
  inset: auto 7% 16% auto;
  width: 34%;
  height: 30%;
  background: radial-gradient(circle at 50% 50%, rgba(189, 173, 235, 0.48), transparent 70%);
  opacity: 0.52;
}

.liquid-object__petal {
  position: absolute;
  border-radius: 999px;
  filter: blur(0.3px);
  opacity: 0.85;
}

.liquid-object__petal--top {
  inset: 4% 21% auto 17%;
  height: 20%;
  background:
    linear-gradient(110deg, rgba(255, 249, 239, 0.62), rgba(232, 210, 167, 0.14)),
    radial-gradient(circle at 22% 42%, rgba(240, 184, 157, 0.4), transparent 48%);
  transform: rotate(-8deg);
}

.liquid-object__petal--bottom {
  inset: auto 14% 6% 26%;
  height: 18%;
  background:
    linear-gradient(105deg, rgba(175, 200, 161, 0.22), rgba(255, 249, 239, 0.5)),
    radial-gradient(circle at 72% 40%, rgba(189, 173, 235, 0.35), transparent 42%);
  transform: rotate(-12deg);
}

.liquid-object__body {
  position: absolute;
  inset: 14% 13% 13% 13%;
  overflow: hidden;
  border: 1px solid rgba(255, 249, 239, 0.46);
  border-radius: 42% 58% 50% 50% / 42% 42% 58% 58%;
  background:
    radial-gradient(circle at 28% 22%, rgba(255, 249, 239, 0.82), transparent 18%),
    radial-gradient(circle at 68% 28%, rgba(232, 210, 167, 0.5), transparent 30%),
    radial-gradient(circle at 34% 74%, rgba(232, 180, 198, 0.34), transparent 34%),
    radial-gradient(circle at 76% 74%, rgba(189, 173, 235, 0.26), transparent 28%),
    linear-gradient(145deg, rgba(255, 249, 239, 0.36), rgba(255, 249, 239, 0.08));
  box-shadow:
    inset 0 1px 0 rgba(255, 249, 239, 0.72),
    inset -28px -34px 90px rgba(15, 14, 12, 0.18),
    inset 30px 34px 80px rgba(255, 249, 239, 0.18),
    0 48px 120px rgba(15, 14, 12, 0.34),
    0 22px 80px rgba(240, 184, 157, 0.22);
  backdrop-filter: blur(28px) saturate(130%);
  -webkit-backdrop-filter: blur(28px) saturate(130%);
  will-change: filter, box-shadow;
}

.liquid-object__pearl-core {
  position: absolute;
  inset: 17% 19% 20%;
  z-index: 0;
  border-radius: 45% 55% 52% 48% / 42% 45% 55% 58%;
  background:
    radial-gradient(circle at 32% 26%, rgba(255, 249, 239, 0.92), transparent 18%),
    radial-gradient(circle at 63% 32%, rgba(232, 210, 167, 0.78), transparent 30%),
    radial-gradient(circle at 38% 72%, rgba(232, 180, 198, 0.56), transparent 34%),
    radial-gradient(circle at 75% 72%, rgba(189, 173, 235, 0.5), transparent 36%),
    linear-gradient(145deg, rgba(255, 249, 239, 0.48), rgba(255, 249, 239, 0.08));
  box-shadow:
    inset 0 1px 0 rgba(255, 249, 239, 0.66),
    inset -20px -24px 62px rgba(15, 14, 12, 0.18),
    0 0 60px rgba(255, 249, 239, 0.22);
  opacity: 0.82;
  will-change: transform, filter;
}

.liquid-object__pearl-core span {
  position: absolute;
  inset: 13% auto auto 18%;
  width: 36%;
  height: 34%;
  border-radius: 999px;
  background: radial-gradient(circle at 40% 35%, rgba(255, 249, 239, 0.98), rgba(255, 249, 239, 0.2) 54%, transparent 70%);
  filter: blur(4px);
  opacity: 0.92;
  will-change: transform;
}

.liquid-object__body::before,
.liquid-object__body::after {
  content: '';
  position: absolute;
  pointer-events: none;
}

.liquid-object__body::before {
  inset: 9% 11% auto auto;
  width: 39%;
  height: 42%;
  border-radius: 999px;
  background: radial-gradient(circle at 42% 32%, rgba(255, 249, 239, 0.76), transparent 55%);
  filter: blur(12px);
  opacity: 0.9;
}

.liquid-object__body::after {
  inset: -20% auto -20% -46%;
  width: 42%;
  background: linear-gradient(90deg, transparent, rgba(255, 249, 239, 0.44), transparent);
  transform: rotate(17deg);
  animation: object-shimmer 6.5s ease-in-out infinite;
}

.liquid-object__shine {
  position: absolute;
  z-index: 1;
  inset: 10% 14% auto;
  height: 26%;
  border-radius: 999px;
  background: linear-gradient(180deg, rgba(255, 249, 239, 0.34), rgba(255, 249, 239, 0.02));
  transform: rotate(-8deg);
  will-change: transform, opacity;
}

.liquid-object__inner {
  position: absolute;
  z-index: 2;
  inset: 24% 18% 20%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #fff9ef;
  text-align: center;
  text-shadow: 0 1px 24px rgba(15, 14, 12, 0.28);
}

.liquid-object__eyebrow {
  margin: 0;
  font-size: clamp(0.72rem, 1vw, 0.82rem);
  font-weight: 700;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: rgba(255, 249, 239, 0.72);
}

.liquid-object__title {
  margin: 0.7rem 0 0;
  font-size: clamp(1.75rem, 4vw, 3rem);
  font-weight: 700;
  letter-spacing: 0;
  line-height: 1;
}

.liquid-object__thread {
  width: min(12rem, 62%);
  height: 0.44rem;
  margin-block: 1.25rem 1.1rem;
  border-radius: 999px;
  background:
    linear-gradient(90deg, transparent, rgba(255, 249, 239, 0.82), rgba(232, 210, 167, 0.72), transparent),
    linear-gradient(90deg, rgba(240, 184, 157, 0.2), rgba(189, 173, 235, 0.28));
  box-shadow: 0 0 34px rgba(255, 249, 239, 0.42);
  transform-origin: center center;
  will-change: transform, opacity;
}

.liquid-object__labels {
  display: flex;
  max-width: 18rem;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.45rem;
}

.liquid-object__labels span,
.liquid-object__chip {
  border: 1px solid rgba(255, 249, 239, 0.32);
  background: rgba(15, 14, 12, 0.16);
  box-shadow: inset 0 1px 0 rgba(255, 249, 239, 0.2);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
}

.liquid-object__labels span {
  border-radius: 999px;
  padding: 0.42rem 0.72rem;
  font-size: clamp(0.72rem, 1.5vw, 0.82rem);
  color: rgba(255, 249, 239, 0.78);
}

.liquid-object__chip {
  position: absolute;
  z-index: 4;
  border-radius: 999px;
  padding: 0.66rem 0.9rem;
  color: rgba(255, 249, 239, 0.86);
  font-size: clamp(0.76rem, 1.6vw, 0.9rem);
  font-weight: 600;
  white-space: nowrap;
}

.liquid-object__chip--one {
  left: 0;
  top: 29%;
  will-change: transform;
}

.liquid-object__chip--two {
  right: -1%;
  top: 20%;
  will-change: transform;
}

.liquid-object__chip--three {
  bottom: 21%;
  right: 5%;
  will-change: transform;
}

@keyframes object-shimmer {
  0% {
    transform: translateX(-20%) rotate(17deg);
    opacity: 0;
  }
  36%,
  60% {
    opacity: 0.72;
  }
  100% {
    transform: translateX(350%) rotate(17deg);
    opacity: 0;
  }
}

@media (max-width: 767px) {
  .liquid-object {
    width: min(27rem, 88vw);
  }

  .liquid-object__body {
    inset: 13% 10% 13%;
  }

  .liquid-object__inner {
    inset: 25% 14% 22%;
  }

  .liquid-object__chip {
    padding: 0.54rem 0.72rem;
  }

  .liquid-object__chip--one {
    left: -2%;
    top: 24%;
  }

  .liquid-object__chip--two {
    right: -4%;
    top: 22%;
  }

  .liquid-object__chip--three {
    right: 0;
    bottom: 18%;
  }
}

@media (prefers-reduced-motion: reduce) {
  .liquid-object__body::after {
    animation: none;
  }
}
</style>
