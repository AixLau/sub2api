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

/**
 * DirectionAwareHover — 方向感知悬停高光
 *
 * 包装容器(默认插槽):mouseenter 时根据鼠标进入位置(经典 hover-dir 算法,
 * 相对中心的角度分四象限)让一层淡色高光从该方向滑入,mouseleave 时向离开方向滑出。
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

const prefersReducedMotion = () => safeMatchMedia('(prefers-reduced-motion: reduce)')
/** 触屏 / 无 hover 能力设备:整个效果不生效 */
const isTouchLike = () => safeMatchMedia('(hover: none)')

/**
 * 经典方向判定:鼠标相对元素中心的坐标按宽高比归一化后取角度,
 * 每 90° 一个象限 → 0=top / 1=right / 2=bottom / 3=left。
 */
function getDirection(event: MouseEvent): Direction {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return 'top'
  const w = rect.width
  const h = rect.height
  const x = (event.clientX - rect.left - w / 2) * (w > h && h > 0 ? h / w : 1)
  const y = (event.clientY - rect.top - h / 2) * (h > w && w > 0 ? w / h : 1)
  const index = (Math.round((Math.atan2(y, x) * (180 / Math.PI) + 180) / 90) + 3) % 4
  return DIRECTIONS[index]
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

  if (prefersReducedMotion()) {
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

  if (fadeOnly.value || prefersReducedMotion()) {
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
