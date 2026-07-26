<template>
  <div
    ref="cardRef"
    class="relative overflow-hidden"
    @mousemove="onMouseMove"
    @mouseenter="visible = true"
    @mouseleave="visible = false"
  >
    <div
      class="spotlight-layer"
      :class="visible ? 'opacity-100' : 'opacity-0'"
      :style="layerStyle"
      aria-hidden="true"
    ></div>
    <div class="relative z-10">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'

interface Props {
  /** Spotlight radius in px */
  gradientSize?: number
  /** Overrides the theme-aware default spotlight color */
  gradientColor?: string
}

const props = withDefaults(defineProps<Props>(), {
  gradientSize: 280,
  gradientColor: ''
})

const cardRef = ref<HTMLDivElement | null>(null)
const visible = ref(false)
const x = ref(0)
const y = ref(0)

function onMouseMove(event: MouseEvent) {
  const rect = cardRef.value?.getBoundingClientRect()
  if (!rect) return
  x.value = event.clientX - rect.left
  y.value = event.clientY - rect.top
}

const layerStyle = computed(() => {
  const style: Record<string, string> = {
    '--spot-x': `${x.value}px`,
    '--spot-y': `${y.value}px`,
    '--spot-size': `${props.gradientSize}px`
  }
  if (props.gradientColor) {
    style['--spot-color'] = props.gradientColor
  }
  return style
})
</script>

<style scoped>
.spotlight-layer {
  position: absolute;
  inset: 0;
  pointer-events: none;
  transition: opacity 0.3s ease;
  background: radial-gradient(
    circle var(--spot-size) at var(--spot-x) var(--spot-y),
    var(--spot-color, rgba(20, 184, 166, 0.1)),
    transparent 70%
  );
}

.dark .spotlight-layer {
  background: radial-gradient(
    circle var(--spot-size) at var(--spot-x) var(--spot-y),
    var(--spot-color, rgba(45, 212, 191, 0.14)),
    transparent 70%
  );
}
</style>
