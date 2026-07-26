<template>
  <div class="curve-lines curve-lines--left" aria-hidden="true">
    <span
      v-for="(line, index) in sideLines"
      :key="`left-${line.width}`"
      class="curve-line curve-line--left"
      :style="{
        width: `${line.width}px`,
        animationDelay: `${line.delay}s`,
        top: `${Math.max(0, 48 - index * 1.6)}%`
      }"
    />
  </div>

  <div class="curve-lines curve-lines--right" aria-hidden="true">
    <span
      v-for="(line, index) in sideLines"
      :key="`right-${line.width}`"
      class="curve-line curve-line--right"
      :style="{
        width: `${line.width}px`,
        animationDelay: `${line.delay}s`,
        top: `${Math.max(0, 48 - index * 1.6)}%`
      }"
    />
  </div>

  <div class="curve-lines curve-lines--top" aria-hidden="true">
    <span
      v-for="line in topLines"
      :key="`top-${line.width}`"
      class="curve-line curve-line--top"
      :style="{
        width: `${line.width}px`,
        animationDelay: `${line.delay}s`
      }"
    />
  </div>
</template>

<script setup lang="ts">
const sideLines = Array.from({ length: 20 }, (_, index) => ({
  width: 60 + index * 10,
  delay: index * 0.25
}))

const topLines = Array.from({ length: 20 }, (_, index) => ({
  width: 72 + index * 12,
  delay: index * 0.25
}))
</script>
