<template>
  <div
    class="marquee relative flex overflow-hidden"
    :class="{ 'marquee-hover-pause': pauseOnHover }"
    :style="{ '--marquee-duration': `${duration}s` }"
  >
    <div class="marquee-track" :class="{ 'marquee-reverse': reverse }">
      <slot />
    </div>
    <div class="marquee-track" :class="{ 'marquee-reverse': reverse }" aria-hidden="true">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
interface Props {
  reverse?: boolean
  pauseOnHover?: boolean
  /** Seconds for one full loop */
  duration?: number
}

withDefaults(defineProps<Props>(), {
  reverse: false,
  pauseOnHover: true,
  duration: 30
})
</script>

<style scoped>
.marquee {
  gap: 1rem;
  mask-image: linear-gradient(to right, transparent, black 10%, black 90%, transparent);
  -webkit-mask-image: linear-gradient(to right, transparent, black 10%, black 90%, transparent);
}

.marquee-track {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-around;
  gap: 1rem;
  min-width: 100%;
  animation: marquee-scroll var(--marquee-duration) linear infinite;
}

.marquee-reverse {
  animation-direction: reverse;
}

.marquee-hover-pause:hover .marquee-track {
  animation-play-state: paused;
}

@keyframes marquee-scroll {
  from {
    transform: translateX(0);
  }
  to {
    transform: translateX(calc(-100% - 1rem));
  }
}

@media (prefers-reduced-motion: reduce) {
  .marquee-track {
    animation: none;
  }
  .marquee-track[aria-hidden='true'] {
    display: none;
  }
}
</style>
