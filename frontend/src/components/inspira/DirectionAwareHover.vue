<template>
  <div
    ref="rootRef"
    class="dah-root relative overflow-hidden"
    @mouseenter="onMouseEnter"
    @mouseleave="onMouseLeave"
  >
    <slot />
    <div class="dah-layer" :class="layerClasses" :style="layerStyle" aria-hidden="true"></div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { isReducedMotionPreferred } from '@/composables/usePrefersReducedMotion'

/**
 * DirectionAwareHover — 方向感知悬停高光
 *
 * 包装容器(默认插槽):mouseenter 时根据鼠标到四条边的距离让一层淡色高光
 * 从最近的方向滑入,mouseleave 时向最近边缘方向滑出。
 *
 * - 高光层 pointer-events:none,渲染在插槽内容之上(半透明色 wash),不影响交互。
 * - 触屏((hover: none))无副作用;reduced-motion 时高光只淡入淡出、不滑动。
 * - 父级圆角请直接加在本组件上(如 class="rounded-xl"),内部 overflow-hidden 裁剪。
 */

interface Props {
  /** 高光颜色(任意 CSS color)。缺省为 teal 淡色,自动适配亮/暗模式 */
  color?: string
}

const props = withDefaults(defineProps<Props>(), {
  color: ''
})

type Direction = 'top' | 'right' | 'bottom' | 'left'
const DIRECTIONS: Direction[] = ['top', 'right', 'bottom', 'left']

const rootRef = ref<HTMLDivElement | null>(null)
const direction = ref<Direction>('top')
const visible = ref(false)
/** true 时高光层无过渡(用于进场前瞬时归位到进入方向) */
const snap = ref(false)
/** reduced-motion 降级:只做透明度淡入淡出 */
const fadeOnly = ref(false)

function safeMatchMedia(query: string): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia(query)?.matches ?? false
}

/** 触屏 / 无 hover 能力设备:整个效果不生效 */
const isTouchLike = () => safeMatchMedia('(hover: none)')

/**
 * 方向判定使用鼠标到四条边的最短距离。mouseenter/mouseleave 的坐标会落在
 * 元素边界附近，这种算法不受卡片宽高比影响，也避免角落处的象限舍入误判。
 */
function getDirection(event: MouseEvent): Direction {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return 'top'
  const distances: Record<Direction, number> = {
    top: Math.abs(event.clientY - rect.top),
    right: Math.abs(rect.right - event.clientX),
    bottom: Math.abs(rect.bottom - event.clientY),
    left: Math.abs(event.clientX - rect.left)
  }
  return DIRECTIONS.reduce((closest, candidate) =>
    distances[candidate] < distances[closest] ? candidate : closest
  )
}

let rafId: number | null = null

function cancelPendingFrame() {
  if (rafId !== null && typeof cancelAnimationFrame === 'function') {
    cancelAnimationFrame(rafId)
  }
  rafId = null
}

function onMouseEnter(event: MouseEvent) {
  if (isTouchLike()) return

  if (isReducedMotionPreferred()) {
    fadeOnly.value = true
    visible.value = true
    return
  }

  fadeOnly.value = false
  cancelPendingFrame()
  // 1. 无过渡地把高光层归位到进入方向的容器外
  direction.value = getDirection(event)
  snap.value = true
  visible.value = false

  const show = () => {
    snap.value = false
    visible.value = true
  }
  if (typeof requestAnimationFrame === 'function') {
    // 双 rAF 确保 snap 位置先被应用,再开启过渡滑入
    rafId = requestAnimationFrame(() => {
      rafId = requestAnimationFrame(() => {
        rafId = null
        show()
      })
    })
  } else {
    show()
  }
}

function onMouseLeave(event: MouseEvent) {
  if (isTouchLike()) return
  cancelPendingFrame()

  if (fadeOnly.value || isReducedMotionPreferred()) {
    visible.value = false
    return
  }

  snap.value = false
  // 向离开方向滑出
  direction.value = getDirection(event)
  visible.value = false
}

onBeforeUnmount(cancelPendingFrame)

const layerClasses = computed(() => {
  if (fadeOnly.value) {
    return { 'dah-fade': true, 'dah-visible': visible.value }
  }
  return {
    [`dah-from-${direction.value}`]: true,
    'dah-visible': visible.value,
    'dah-snap': snap.value
  }
})

const layerStyle = computed(() => (props.color ? { '--dah-color': props.color } : undefined))
</script>

<style scoped>
.dah-layer {
  position: absolute;
  inset: 0;
  z-index: 10;
  pointer-events: none;
  opacity: 0;
  background: var(--dah-color, rgba(20, 184, 166, 0.1));
  transition:
    transform 0.35s ease,
    opacity 0.35s ease;
  will-change: transform, opacity;
}

.dark .dah-layer {
  background: var(--dah-color, rgba(45, 212, 191, 0.14));
}

.dah-visible {
  opacity: 1;
  transform: translate3d(0, 0, 0);
}

.dah-from-top:not(.dah-visible) {
  transform: translate3d(0, -101%, 0);
}

.dah-from-right:not(.dah-visible) {
  transform: translate3d(101%, 0, 0);
}

.dah-from-bottom:not(.dah-visible) {
  transform: translate3d(0, 101%, 0);
}

.dah-from-left:not(.dah-visible) {
  transform: translate3d(-101%, 0, 0);
}

.dah-snap {
  transition: none;
}

/* reduced-motion 降级:不滑动,只淡入淡出 */
.dah-fade {
  transform: none !important;
  transition: opacity 0.3s ease;
}
</style>
