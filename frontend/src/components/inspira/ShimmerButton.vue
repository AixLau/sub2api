<template>
  <component
    :is="as"
    :type="as === 'button' ? 'button' : undefined"
    class="relative inline-flex items-center justify-center overflow-hidden whitespace-nowrap"
    :style="styleVars"
  >
    <span class="shimmer-sweep" aria-hidden="true"></span>
    <span class="relative z-10 inline-flex items-center gap-2">
      <slot />
    </span>
  </component>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  as?: string
  /** CSS background of the button surface */
  background?: string
  shimmerColor?: string
  /** Sweep period in seconds */
  shimmerDuration?: number
}

const props = withDefaults(defineProps<Props>(), {
  as: 'button',
  background: 'linear-gradient(to right, #2563eb, #0891b2)',
  shimmerColor: 'rgba(255, 255, 255, 0.6)',
  shimmerDuration: 3
})

const styleVars = computed(() => ({
  background: props.background,
  '--shimmer-color': props.shimmerColor,
  '--shimmer-duration': `${props.shimmerDuration}s`
}))
</script>

<style scoped>
.shimmer-sweep {
  position: absolute;
  inset: 0;
  transform: translateX(-100%) skewX(-15deg);
  background: linear-gradient(90deg, transparent 20%, var(--shimmer-color) 50%, transparent 80%);
  opacity: 0.7;
  animation: shimmer-sweep var(--shimmer-duration) ease-in-out infinite;
}

@keyframes shimmer-sweep {
  0% {
    transform: translateX(-100%) skewX(-15deg);
  }
  60%,
  100% {
    transform: translateX(100%) skewX(-15deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .shimmer-sweep {
    animation: none;
    display: none;
  }
}
</style>
