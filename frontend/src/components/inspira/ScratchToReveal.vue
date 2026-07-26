<template>
  <div ref="containerRef" class="relative">
    <slot />
    <canvas
      v-if="coverActive"
      ref="canvasRef"
      class="scratch-cover absolute inset-0 z-10 h-full w-full cursor-pointer rounded-[inherit]"
      :class="{ 'scratch-cover--revealed': revealed }"
      tabindex="0"
      role="button"
      :aria-label="coverText || 'Reveal'"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerUp"
      @pointerleave="onPointerUp"
      @dblclick="reveal"
      @keydown.enter.prevent="reveal"
      @keydown.space.prevent="reveal"
      @transitionend="onCoverTransitionEnd"
    ></canvas>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { usePrefersReducedMotion } from '@/composables/usePrefersReducedMotion'

interface Props {
  /** 覆盖层颜色 */
  coverColor?: string
  /** 覆盖层提示文案 */
  coverText?: string
  /** 刮开面积比例阈值（0~1），达到后自动揭示 */
  threshold?: number
  /** 笔刷半径（CSS 像素） */
  radius?: number
}

const props = withDefaults(defineProps<Props>(), {
  // 缺省时按亮/暗色模式自动取值（见 drawCover）
  coverColor: '',
  threshold: 0.5,
  radius: 24
})

const emit = defineEmits<{ (e: 'complete'): void }>()

const containerRef = ref<HTMLDivElement | null>(null)
const canvasRef = ref<HTMLCanvasElement | null>(null)

/** 覆盖层是否仍在 DOM 中（jsdom / reduced-motion 时为 false，内容直接可见） */
const coverActive = ref(true)
/** 达到阈值后置 true，触发整层淡出 */
const revealed = ref(false)

let ctx: CanvasRenderingContext2D | null = null
let resizeObserver: ResizeObserver | null = null
let scratching = false
let hasScratched = false
let completed = false
let dpr = 1
let lastX = 0
let lastY = 0
/** 采样节流：每擦除若干笔采样一次透明比例 */
let strokesSinceSample = 0
const { prefersReducedMotion } = usePrefersReducedMotion()

function isDarkMode(): boolean {
  try {
    return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
  } catch {
    return false
  }
}

function drawCover() {
  const canvas = canvasRef.value
  const container = containerRef.value
  if (!canvas || !container || !ctx) return
  dpr = typeof window !== 'undefined' && window.devicePixelRatio ? window.devicePixelRatio : 1
  const width = Math.max(1, container.clientWidth)
  const height = Math.max(1, container.clientHeight)
  canvas.width = Math.floor(width * dpr)
  canvas.height = Math.floor(height * dpr)
  const dark = isDarkMode()
  ctx.globalCompositeOperation = 'source-over'
  ctx.fillStyle = props.coverColor || (dark ? '#334155' : '#cbd5e1')
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  if (props.coverText) {
    ctx.fillStyle = dark ? 'rgba(226, 232, 240, 0.9)' : 'rgba(71, 85, 105, 0.9)'
    ctx.font = `600 ${Math.round(14 * dpr)}px system-ui, sans-serif`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText(props.coverText, canvas.width / 2, canvas.height / 2)
  }
}

function scratchAt(x: number, y: number, connect: boolean) {
  const canvas = canvasRef.value
  if (!canvas || !ctx) return
  const rect = canvas.getBoundingClientRect()
  const px = (x - rect.left) * dpr
  const py = (y - rect.top) * dpr
  const r = props.radius * dpr
  ctx.globalCompositeOperation = 'destination-out'
  ctx.beginPath()
  ctx.arc(px, py, r, 0, Math.PI * 2)
  ctx.fill()
  if (connect) {
    // 连接上一点，避免快速拖动时出现断点
    ctx.lineWidth = r * 2
    ctx.lineCap = 'round'
    ctx.beginPath()
    ctx.moveTo(lastX, lastY)
    ctx.lineTo(px, py)
    ctx.stroke()
  }
  lastX = px
  lastY = py
}

/** 采样透明像素比例，达到阈值后揭示 */
function sampleAndCheck() {
  const canvas = canvasRef.value
  if (!canvas || !ctx || completed) return
  let data: Uint8ClampedArray
  try {
    data = ctx.getImageData(0, 0, canvas.width, canvas.height).data
  } catch {
    return
  }
  // 按步长采样 alpha 通道，降低开销
  const stride = 16 * 4
  let transparent = 0
  let total = 0
  for (let i = 3; i < data.length; i += stride) {
    total++
    if (data[i] === 0) transparent++
  }
  if (total > 0 && transparent / total >= props.threshold) {
    reveal()
  }
}

function reveal() {
  if (completed) return
  completed = true
  revealed.value = true
  emit('complete')
}

function onCoverTransitionEnd() {
  if (revealed.value) coverActive.value = false
}

function onPointerDown(event: PointerEvent) {
  if (completed) return
  scratching = true
  hasScratched = true
  try {
    (event.target as HTMLElement | null)?.setPointerCapture?.(event.pointerId)
  } catch {
    /* jsdom 下 setPointerCapture 可能不可用 */
  }
  scratchAt(event.clientX, event.clientY, false)
}

function onPointerMove(event: PointerEvent) {
  if (!scratching || completed) return
  scratchAt(event.clientX, event.clientY, true)
  strokesSinceSample++
  if (strokesSinceSample >= 8) {
    strokesSinceSample = 0
    sampleAndCheck()
  }
}

function onPointerUp() {
  if (!scratching) return
  scratching = false
  sampleAndCheck()
}

onMounted(async () => {
  if (prefersReducedMotion.value) {
    // 减少动效偏好：不覆盖，直接显示内容并视为已揭示
    coverActive.value = false
    reveal()
    return
  }
  await nextTick()
  const canvas = canvasRef.value
  if (!canvas) {
    coverActive.value = false
    reveal()
    return
  }
  try {
    ctx = canvas.getContext('2d')
  } catch {
    ctx = null
  }
  if (!ctx) {
    // jsdom / canvas 不可用：不渲染覆盖层，内容直接可见
    coverActive.value = false
    reveal()
    return
  }
  drawCover()
  if (typeof ResizeObserver === 'function' && containerRef.value) {
    resizeObserver = new ResizeObserver(() => {
      if (completed) return
      // 重画会清掉已刮进度；已经开刮时尺寸变化直接揭示,避免进度归零的挫败感
      if (hasScratched) {
        reveal()
      } else {
        drawCover()
      }
    })
    resizeObserver.observe(containerRef.value)
  }
})

watch(prefersReducedMotion, (reduced) => {
  if (reduced && coverActive.value) {
    coverActive.value = false
    reveal()
  }
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  resizeObserver = null
})
</script>

<style scoped>
.scratch-cover {
  touch-action: none;
  opacity: 1;
  transition: opacity 0.5s ease;
}

.scratch-cover--revealed {
  opacity: 0;
  pointer-events: none;
}
</style>
