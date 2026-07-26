<template>
  <div ref="containerRef" class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
    <canvas ref="canvasRef" class="block h-full w-full"></canvas>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { usePrefersReducedMotion } from '@/composables/usePrefersReducedMotion'

interface Props {
  squareSize?: number
  gridGap?: number
  /** Probability per square per second of re-rolling its opacity */
  flickerChance?: number
  color?: string
  maxOpacity?: number
}

const props = withDefaults(defineProps<Props>(), {
  squareSize: 4,
  gridGap: 6,
  flickerChance: 0.3,
  color: '#14b8a6',
  maxOpacity: 0.2
})

const containerRef = ref<HTMLDivElement | null>(null)
const canvasRef = ref<HTMLCanvasElement | null>(null)

let ctx: CanvasRenderingContext2D | null = null
let rafId: number | null = null
let resizeObserver: ResizeObserver | null = null
let intersectionObserver: IntersectionObserver | null = null
let running = false
let cols = 0
let rows = 0
let dpr = 1
let opacities = new Float32Array(0)
let lastTime = 0
let intersecting = false
const { prefersReducedMotion } = usePrefersReducedMotion()

function hexToRgb(hex: string): string {
  const match = /^#?([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i.exec(hex.trim())
  if (!match) return '20, 184, 166'
  return `${parseInt(match[1], 16)}, ${parseInt(match[2], 16)}, ${parseInt(match[3], 16)}`
}

const colorRgb = hexToRgb(props.color)

function setupGrid() {
  const container = containerRef.value
  const canvas = canvasRef.value
  if (!container || !canvas || !ctx) return
  dpr = typeof window !== 'undefined' && window.devicePixelRatio ? window.devicePixelRatio : 1
  const width = container.clientWidth
  const height = container.clientHeight
  canvas.width = Math.max(1, Math.floor(width * dpr))
  canvas.height = Math.max(1, Math.floor(height * dpr))
  const cell = props.squareSize + props.gridGap
  cols = Math.ceil(width / cell) + 1
  rows = Math.ceil(height / cell) + 1
  opacities = new Float32Array(cols * rows)
  for (let i = 0; i < opacities.length; i++) {
    opacities[i] = Math.random() * props.maxOpacity
  }
  draw()
}

function draw() {
  const canvas = canvasRef.value
  if (!ctx || !canvas) return
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  const cell = (props.squareSize + props.gridGap) * dpr
  const size = props.squareSize * dpr
  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < cols; col++) {
      const opacity = opacities[row * cols + col]
      if (opacity <= 0.005) continue
      ctx.fillStyle = `rgba(${colorRgb}, ${opacity})`
      ctx.fillRect(col * cell, row * cell, size, size)
    }
  }
}

function tick(now: number) {
  const dt = Math.min((now - lastTime) / 1000, 0.1)
  lastTime = now
  for (let i = 0; i < opacities.length; i++) {
    if (Math.random() < props.flickerChance * dt) {
      opacities[i] = Math.random() * props.maxOpacity
    }
  }
  draw()
  rafId = requestAnimationFrame(tick)
}

function start() {
  if (running || !ctx) return
  if (prefersReducedMotion.value || typeof requestAnimationFrame !== 'function') return
  running = true
  lastTime = typeof performance !== 'undefined' ? performance.now() : 0
  rafId = requestAnimationFrame(tick)
}

function stop() {
  running = false
  if (rafId !== null && typeof cancelAnimationFrame === 'function') {
    cancelAnimationFrame(rafId)
  }
  rafId = null
}

onMounted(() => {
  const canvas = canvasRef.value
  const container = containerRef.value
  if (!canvas || !container) return
  try {
    ctx = canvas.getContext('2d')
  } catch {
    ctx = null
  }
  if (!ctx) return

  setupGrid()

  if (typeof ResizeObserver === 'function') {
    resizeObserver = new ResizeObserver(() => setupGrid())
    resizeObserver.observe(container)
  }

  if (typeof IntersectionObserver === 'function') {
    intersectionObserver = new IntersectionObserver((entries) => {
      const entry = entries[0]
      if (entry && entry.isIntersecting) {
        intersecting = true
        start()
      } else {
        intersecting = false
        stop()
      }
    })
    intersectionObserver.observe(container)
  } else {
    intersecting = true
    start()
  }
})

watch(prefersReducedMotion, (reduced) => {
  if (reduced) {
    stop()
  } else if (intersecting) {
    start()
  }
})

onBeforeUnmount(() => {
  stop()
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
})
</script>
