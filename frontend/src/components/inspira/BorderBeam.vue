<template>
  <div class="border-beam pointer-events-none absolute inset-0" :style="styleVars" aria-hidden="true"></div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { accentColors, brandColors } from '@/theme/designTokens'

interface Props {
  /** Beam length in px */
  size?: number
  /** Seconds for one full loop around the border */
  duration?: number
  delay?: number
  borderWidth?: number
  colorFrom?: string
  colorTo?: string
}

const props = withDefaults(defineProps<Props>(), {
  size: 200,
  duration: 12,
  delay: 0,
  borderWidth: 1.5,
  colorFrom: brandColors['500'],
  colorTo: accentColors['400']
})

const styleVars = computed(() => ({
  '--beam-size': `${props.size}px`,
  '--beam-duration': `${props.duration}s`,
  '--beam-delay': `${props.delay}s`,
  '--beam-border-width': `${props.borderWidth}px`,
  '--beam-color-from': props.colorFrom,
  '--beam-color-to': props.colorTo
}))
</script>

<style scoped>
/* Parent must be `position: relative` with a border-radius; the beam is
   clipped to the border ring via the padding-box/border-box mask trick. */
.border-beam {
  border-radius: inherit;
  padding: var(--beam-border-width);
  -webkit-mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  -webkit-mask-composite: xor;
  mask:
    linear-gradient(#fff 0 0) content-box,
    linear-gradient(#fff 0 0);
  mask-composite: exclude;
}

@supports (offset-path: rect(0px auto auto 0px)) {
  .border-beam::before {
    content: '';
    position: absolute;
    aspect-ratio: 1;
    width: var(--beam-size);
    background: linear-gradient(to left, var(--beam-color-from), var(--beam-color-to), transparent);
    offset-anchor: 90% 50%;
    offset-path: rect(0 auto auto 0 round var(--beam-size));
    animation: border-beam-move var(--beam-duration) linear infinite;
    animation-delay: var(--beam-delay);
  }
}

@keyframes border-beam-move {
  to {
    offset-distance: 100%;
  }
}

@media (prefers-reduced-motion: reduce) {
  .border-beam::before {
    content: none;
  }
}
</style>
