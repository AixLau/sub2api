<template>
  <div
    ref="cardRef"
    class="relative overflow-hidden"
    @mousemove="onMouseMove"
    @mouseenter="onMouseEnter"
    @mouseleave="onMouseLeave"
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

// 每次 mousemove 都读 getBoundingClientRect 会触发布局读取,
// 在 mouseenter 时缓存一次(悬停期间滚动导致的偏差可接受)。
let cachedRect: DOMRect | null = null

function onMouseEnter() {
  visible.value = true
  cachedRect = cardRef.value?.getBoundingClientRect() ?? null
}

function onMouseLeave() {
  visible.value = false
  cachedRect = null
}

function onMouseMove(event: MouseEvent) {
  const rect = cachedRect ?? cardRef.value?.getBoundingClientRect() ?? null
  if (!rect) return
  cachedRect = rect
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
    var(--spot-color, rgb(var(--color-brand-500) / 0.1)),
    transparent 70%
  );
}

.dark .spotlight-layer {
  background: radial-gradient(
    circle var(--spot-size) at var(--spot-x) var(--spot-y),
    var(--spot-color, rgb(var(--color-brand-500) / 0.14)),
    transparent 70%
  );
}
</style>
