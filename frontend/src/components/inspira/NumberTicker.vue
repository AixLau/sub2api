<template>
  <span>{{ displayText }}</span>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

interface Props {
  value: number
  /** Animation duration in ms */
  duration?: number
  decimalPlaces?: number
  prefix?: string
  suffix?: string
  /** Overrides decimalPlaces-based formatting when provided */
  formatFn?: (n: number) => string
}

const props = withDefaults(defineProps<Props>(), {
  duration: 1500,
  decimalPlaces: 0,
  prefix: '',
  suffix: ''
})

const displayed = ref(0)
let rafId: number | null = null

function prefersReducedMotion(): boolean {
  try {
    return (
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    )
  } catch {
    return false
  }
}

const displayText = computed(() => {
  const body = props.formatFn
    ? props.formatFn(displayed.value)
    : displayed.value.toLocaleString('en-US', {
        minimumFractionDigits: props.decimalPlaces,
        maximumFractionDigits: props.decimalPlaces
      })
  return `${props.prefix}${body}${props.suffix}`
})

function stopAnimation() {
  if (rafId !== null && typeof cancelAnimationFrame === 'function') {
    cancelAnimationFrame(rafId)
  }
  rafId = null
}

function animateTo(target: number) {
  stopAnimation()
  const canAnimate =
    typeof requestAnimationFrame === 'function' &&
    typeof performance !== 'undefined' &&
    props.duration > 0 &&
    !prefersReducedMotion()
  if (!canAnimate) {
    displayed.value = target
    return
  }
  const from = displayed.value
  const startAt = performance.now()
  const step = (now: number) => {
    const progress = Math.min(1, (now - startAt) / props.duration)
    const eased = 1 - Math.pow(1 - progress, 3)
    displayed.value = from + (target - from) * eased
    if (progress < 1) {
      rafId = requestAnimationFrame(step)
    } else {
      displayed.value = target
      rafId = null
    }
  }
  rafId = requestAnimationFrame(step)
}

watch(() => props.value, animateTo, { immediate: true })

onBeforeUnmount(stopAnimation)
</script>
