<template>
  <div
    ref="containerRef"
    class="relative"
    :class="enabled ? 'cursor-zoom-in' : ''"
    :style="rootStyle"
    @mouseenter="onMouseEnter"
    @mouseleave="onMouseLeave"
    @mousemove="onMouseMove"
  >
    <slot />
    <!--
      放大层:插槽渲染第二份,整层用 clip-path 裁成圆形镜面;
      内容以鼠标点为 transform-origin 做 scale,鼠标下的像素保持不动,周围被放大。
    -->
    <div
      v-if="enabled"
      class="lens-layer"
      :class="active ? 'opacity-100' : 'opacity-0'"
      aria-hidden="true"
    >
      <div class="lens-content">
        <slot />
      </div>
      <div class="lens-rim"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Lens — 圆形放大镜包装组件(默认插槽)
 *
 * 用法:
 *   <Lens :zoom="1.8" :size="140">
 *     <img src="..." />
 *   </Lens>
 *
 * 实现:插槽渲染两份。第二份放在 absolute inset-0 的放大层内,
 * 放大层用 clip-path: circle(r at x y) 裁出跟随鼠标的圆形镜面,
 * 层内内容 transform: scale(zoom) 且 transform-origin 定在鼠标点,
 * 因此镜面中心与底层像素精确对准。鼠标离开时 opacity 淡出。
 *
 * 不启用的场景(此时只渲染一份插槽,无任何副作用):
 * - 触屏(pointer: coarse):没有 hover 语义。
 * - prefers-reduced-motion:二选一里选择「干脆不启用」——放大镜的价值
 *   就在于跟随鼠标,这本身就是持续运动;静态放大意义很小,且内容在
 *   基础尺寸下本就完整可读,属纯装饰增强,直接关闭最符合该偏好。
 *
 * 性能:mouseenter 时缓存一次 getBoundingClientRect,
 * mousemove 只做减法,不触发布局读取(与 CardSpotlight 同策略)。
 */
import { computed, ref, watch } from 'vue'
import { usePrefersReducedMotion } from '@/composables/usePrefersReducedMotion'

interface Props {
  /** 放大倍数 */
  zoom?: number
  /** 镜面直径 px */
  size?: number
}

const props = withDefaults(defineProps<Props>(), {
  zoom: 1.8,
  size: 140
})

function matchesMedia(query: string): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia(query).matches
}

const { prefersReducedMotion } = usePrefersReducedMotion()
const coarsePointer = matchesMedia('(pointer: coarse)')
const enabled = computed(() => !coarsePointer && !prefersReducedMotion.value)

const containerRef = ref<HTMLDivElement | null>(null)
const active = ref(false)
const x = ref(0)
const y = ref(0)

// mouseenter 缓存布局,mousemove 不读 getBoundingClientRect
let cachedRect: DOMRect | null = null

function updatePosition(event: MouseEvent) {
  if (!cachedRect) return
  x.value = event.clientX - cachedRect.left
  y.value = event.clientY - cachedRect.top
}

function onMouseEnter(event: MouseEvent) {
  if (!enabled.value) return
  cachedRect = containerRef.value?.getBoundingClientRect() ?? null
  updatePosition(event)
  active.value = true
}

function onMouseMove(event: MouseEvent) {
  if (!enabled.value) return
  updatePosition(event)
}

function onMouseLeave() {
  if (!enabled.value) return
  active.value = false
  cachedRect = null
}

const rootStyle = computed(() => ({
  '--lens-x': `${x.value}px`,
  '--lens-y': `${y.value}px`,
  '--lens-r': `${props.size / 2}px`,
  '--lens-zoom': String(props.zoom)
}))

watch(enabled, (isEnabled) => {
  if (!isEnabled) {
    active.value = false
    cachedRect = null
  }
})
</script>

<style scoped>
.lens-layer {
  position: absolute;
  inset: 0;
  z-index: 20;
  pointer-events: none;
  clip-path: circle(var(--lens-r) at var(--lens-x) var(--lens-y));
  transition: opacity 0.25s ease;
}

.lens-content {
  width: 100%;
  height: 100%;
  transform: scale(var(--lens-zoom));
  transform-origin: var(--lens-x) var(--lens-y);
  will-change: transform;
}

/* 镜面边缘:内阴影 + 细描边,增强"玻璃镜片"质感;位于放大层内,随 clip-path 一起裁剪 */
.lens-rim {
  position: absolute;
  left: var(--lens-x);
  top: var(--lens-y);
  width: calc(var(--lens-r) * 2);
  height: calc(var(--lens-r) * 2);
  transform: translate(-50%, -50%);
  border-radius: 9999px;
  box-shadow:
    inset 0 0 1px 1px rgb(var(--color-shadow) / 0.18),
    inset 0 0 10px 2px rgb(var(--color-shadow) / 0.08);
}

.dark .lens-rim {
  box-shadow:
    inset 0 0 1px 1px rgb(var(--color-surface-inverse) / 0.22),
    inset 0 0 10px 2px rgb(var(--color-shadow) / 0.25);
}
</style>
