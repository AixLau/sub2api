<template>
  <div
    class="relative inline-flex items-center justify-center"
    :style="{ width: `${size}px`, height: `${size}px` }"
    role="progressbar"
    aria-valuemin="0"
    aria-valuemax="100"
    :aria-valuenow="Math.round(cappedValue)"
  >
    <svg
      :width="size"
      :height="size"
      :viewBox="`0 0 ${size} ${size}`"
      class="-rotate-90"
      aria-hidden="true"
    >
      <!-- Track -->
      <circle
        :cx="center"
        :cy="center"
        :r="radius"
        fill="none"
        stroke="currentColor"
        :stroke-width="strokeWidth"
        class="text-gray-200 dark:text-dark-600"
      />
      <!-- Progress arc -->
      <circle
        :cx="center"
        :cy="center"
        :r="radius"
        fill="none"
        :stroke="resolvedColor"
        :stroke-width="strokeWidth"
        stroke-linecap="round"
        :stroke-dasharray="circumference"
        :stroke-dashoffset="dashOffset"
      />
    </svg>
    <span
      v-if="showValue"
      class="absolute inset-0 flex items-center justify-center text-xs font-semibold tabular-nums text-gray-900 dark:text-white"
    >
      <NumberTicker :value="Math.round(props.value)" suffix="%" :duration="duration" />
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { usePrefersReducedMotion } from '@/composables/usePrefersReducedMotion'
import NumberTicker from './NumberTicker.vue'

interface Props {
  /** Percentage, 0-100 (values above 100 render a full ring) */
  value: number
  /** Outer size in px */
  size?: number
  strokeWidth?: number
  /** Stroke color; when omitted, auto by threshold: <70% teal, 70-90% amber, >=90% rose */
  color?: string
  showValue?: boolean
  /** Animation duration in ms */
  duration?: number
}

const props = withDefaults(defineProps<Props>(), {
  size: 64,
  strokeWidth: 6,
  color: '',
  showValue: true,
  duration: 500
})

const center = computed(() => props.size / 2)
const radius = computed(() => Math.max(1, (props.size - props.strokeWidth) / 2))
const circumference = computed(() => 2 * Math.PI * radius.value)

/** Ring is capped at 100% even if value overflows */
const cappedValue = computed(() => Math.min(100, Math.max(0, props.value)))

const resolvedColor = computed(() => {
  if (props.color) return props.color
  const v = Math.max(0, props.value)
  if (v >= 90) return '#f43f5e' // rose-500
  if (v >= 70) return '#f59e0b' // amber-500
  return '#14b8a6' // teal-500
})

/** Animated fraction 0..1 driven by rAF */
const animatedFraction = ref(0)
let rafId: number | null = null
const { prefersReducedMotion } = usePrefersReducedMotion()

const dashOffset = computed(
  () => circumference.value * (1 - animatedFraction.value)
)

function stopAnimation() {
  if (rafId !== null && typeof cancelAnimationFrame === 'function') {
    cancelAnimationFrame(rafId)
  }
  rafId = null
}

function animateTo(targetPercent: number) {
  stopAnimation()
  const target = Math.min(100, Math.max(0, targetPercent)) / 100
  const canAnimate =
    typeof requestAnimationFrame === 'function' &&
    typeof performance !== 'undefined' &&
    props.duration > 0 &&
    !prefersReducedMotion.value
  if (!canAnimate) {
    animatedFraction.value = target
    return
  }
  const from = animatedFraction.value
  const startAt = performance.now()
  const step = (now: number) => {
    const progress = Math.min(1, (now - startAt) / props.duration)
    const eased = 1 - Math.pow(1 - progress, 3)
    animatedFraction.value = from + (target - from) * eased
    if (progress < 1) {
      rafId = requestAnimationFrame(step)
    } else {
      animatedFraction.value = target
      rafId = null
    }
  }
  rafId = requestAnimationFrame(step)
}

watch([() => props.value, prefersReducedMotion], ([value]) => animateTo(value), {
  immediate: true
})

onBeforeUnmount(stopAnimation)
</script>
