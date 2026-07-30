<script lang="ts">
let liquidGlassInstance = 0
</script>

<script setup lang="ts">
import type { HTMLAttributes } from 'vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { cn } from '@/utils/cn'

interface Props {
  radius?: number
  border?: number
  lightness?: number
  displace?: number
  blend?: string
  xChannel?: 'R' | 'G' | 'B'
  yChannel?: 'R' | 'G' | 'B'
  alpha?: number
  blur?: number
  rOffset?: number
  gOffset?: number
  bOffset?: number
  scale?: number
  frost?: number
  class?: HTMLAttributes['class']
  containerClass?: HTMLAttributes['class']
}

const props = withDefaults(defineProps<Props>(), {
  radius: 16,
  border: 0.07,
  lightness: 50,
  displace: 0,
  blend: 'difference',
  xChannel: 'R',
  yChannel: 'B',
  alpha: 0.93,
  blur: 11,
  rOffset: 0,
  gOffset: 10,
  bOffset: 20,
  scale: -180,
  frost: 0.05,
  class: '',
  containerClass: ''
})

const liquidGlassRoot = ref<HTMLElement | null>(null)
const dimensions = reactive({ width: 1, height: 1 })
const supported = ref(false)
const filterId = `liquid-glass-${++liquidGlassInstance}`
let observer: ResizeObserver | null = null

const rootStyle = computed(() => ({
  borderRadius: `${props.radius}px`,
  backgroundColor: supported.value
    ? `rgba(255, 255, 255, ${props.frost})`
    : 'rgb(255, 255, 255)',
  WebkitBackdropFilter: supported.value ? `url(#${filterId})` : 'none',
  backdropFilter: supported.value ? `url(#${filterId})` : 'none'
}))

const displacementImage = computed(() => {
  const width = Math.max(1, dimensions.width)
  const height = Math.max(1, dimensions.height)
  const inset = Math.min(width, height) * (props.border * 0.5)

  return `
    <svg viewBox="0 0 ${width} ${height}" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="red" x1="100%" y1="0%" x2="0%" y2="0%">
          <stop offset="0%" stop-color="#0000"/>
          <stop offset="100%" stop-color="red"/>
        </linearGradient>
        <linearGradient id="blue" x1="0%" y1="0%" x2="0%" y2="100%">
          <stop offset="0%" stop-color="#0000"/>
          <stop offset="100%" stop-color="blue"/>
        </linearGradient>
      </defs>
      <rect width="${width}" height="${height}" fill="black"/>
      <rect width="${width}" height="${height}" rx="${props.radius}" fill="url(#red)"/>
      <rect width="${width}" height="${height}" rx="${props.radius}" fill="url(#blue)" style="mix-blend-mode:${props.blend}"/>
      <rect
        x="${inset}"
        y="${inset}"
        width="${Math.max(1, width - inset * 2)}"
        height="${Math.max(1, height - inset * 2)}"
        rx="${props.radius}"
        fill="hsl(0 0% ${props.lightness}% / ${props.alpha})"
        style="filter:blur(${props.blur}px)"
      />
    </svg>
  `
})

const displacementDataUri = computed(() =>
  `data:image/svg+xml,${encodeURIComponent(displacementImage.value)}`
)

function measure() {
  if (!liquidGlassRoot.value) return
  const rect = liquidGlassRoot.value.getBoundingClientRect()
  dimensions.width = Math.max(1, rect.width)
  dimensions.height = Math.max(1, rect.height)
}

function isChromiumBrowser() {
  if (typeof navigator === 'undefined') return false
  return /(?:Chrome|Chromium|Edg)\//.test(navigator.userAgent)
}

onMounted(() => {
  supported.value = isChromiumBrowser()
  if (!supported.value) return

  measure()
  if (typeof ResizeObserver !== 'function' || !liquidGlassRoot.value) return

  observer = new ResizeObserver((entries) => {
    const entry = entries[0]
    if (!entry) return

    const borderBox = entry.borderBoxSize?.[0]

    if (borderBox) {
      dimensions.width = Math.max(1, borderBox.inlineSize)
      dimensions.height = Math.max(1, borderBox.blockSize)
      return
    }

    dimensions.width = Math.max(1, entry.contentRect.width)
    dimensions.height = Math.max(1, entry.contentRect.height)
  })
  observer.observe(liquidGlassRoot.value)
})

onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div
    ref="liquidGlassRoot"
    :style="rootStyle"
    :class="cn('liquid-glass', supported ? 'liquid-glass--supported' : 'liquid-glass--fallback', props.containerClass)"
    :data-liquid-glass="supported ? 'supported' : 'fallback'"
  >
    <div :class="cn('liquid-glass__content', props.class)">
      <slot />
    </div>

    <svg
      v-if="supported"
      class="liquid-glass__filter"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
      focusable="false"
    >
      <defs>
        <filter :id="filterId" color-interpolation-filters="sRGB">
          <feImage x="0" y="0" width="100%" height="100%" :href="displacementDataUri" result="map" />
          <feDisplacementMap
            in="SourceGraphic"
            in2="map"
            :xChannelSelector="xChannel"
            :yChannelSelector="yChannel"
            :scale="scale + rOffset"
            result="dispRed"
          />
          <feColorMatrix
            in="dispRed"
            type="matrix"
            values="1 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 1 0"
            result="red"
          />
          <feDisplacementMap
            in="SourceGraphic"
            in2="map"
            :xChannelSelector="xChannel"
            :yChannelSelector="yChannel"
            :scale="scale + gOffset"
            result="dispGreen"
          />
          <feColorMatrix
            in="dispGreen"
            type="matrix"
            values="0 0 0 0 0  0 1 0 0 0  0 0 0 0 0  0 0 0 1 0"
            result="green"
          />
          <feDisplacementMap
            in="SourceGraphic"
            in2="map"
            :xChannelSelector="xChannel"
            :yChannelSelector="yChannel"
            :scale="scale + bOffset"
            result="dispBlue"
          />
          <feColorMatrix
            in="dispBlue"
            type="matrix"
            values="0 0 0 0 0  0 0 0 0 0  0 0 1 0 0  0 0 0 1 0"
            result="blue"
          />
          <feBlend in="red" in2="green" mode="screen" result="rg" />
          <feBlend in="rg" in2="blue" mode="screen" result="output" />
          <feGaussianBlur :stdDeviation="displace" />
        </filter>
      </defs>
    </svg>
  </div>
</template>

<style scoped>
.liquid-glass {
  position: relative;
  display: block;
  opacity: 1;
}

.liquid-glass--supported {
  border: 1px solid rgb(255 255 255 / 0.62);
  box-shadow:
    0 0 2px 1px rgb(15 23 42 / 0.15) inset,
    0 0 10px 4px rgb(15 23 42 / 0.10) inset,
    0 4px 16px rgb(17 17 26 / 0.05),
    0 8px 24px rgb(17 17 26 / 0.05),
    0 16px 56px rgb(17 17 26 / 0.05),
    0 4px 16px rgb(17 17 26 / 0.05) inset,
    0 8px 24px rgb(17 17 26 / 0.05) inset,
    0 16px 56px rgb(17 17 26 / 0.05) inset;
}

.liquid-glass--fallback {
  border: 1px solid rgb(0 0 0 / 0.05);
  box-shadow: 0 22px 60px rgb(15 23 42 / 0.22);
}

.liquid-glass__content {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
  border-radius: inherit;
}

.liquid-glass__filter {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}
</style>
