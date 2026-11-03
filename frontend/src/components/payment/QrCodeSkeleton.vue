<template>
  <div
    data-test="payment-qr-skeleton"
    class="qr-skeleton relative overflow-hidden"
    :style="{ width: `${size}px`, height: `${size}px` }"
    aria-hidden="true"
  >
    <svg
      :viewBox="`0 0 ${GRID} ${GRID}`"
      :width="size"
      :height="size"
      shape-rendering="crispEdges"
      class="block text-gray-200"
    >
      <!-- Finder patterns: the three corner squares every QR code has. -->
      <g v-for="corner in FINDERS" :key="`${corner.x}-${corner.y}`" fill="currentColor">
        <path :d="finderPath(corner.x, corner.y)" fill-rule="evenodd" />
        <rect :x="corner.x + 2" :y="corner.y + 2" width="3" height="3" />
      </g>
      <rect
        v-for="cell in MODULES"
        :key="`${cell.x}-${cell.y}`"
        :x="cell.x"
        :y="cell.y"
        width="1"
        height="1"
        fill="currentColor"
        class="opacity-70"
      />
    </svg>
    <!-- Shimmer sweep; disabled for users who prefer reduced motion. -->
    <div class="qr-skeleton-shimmer pointer-events-none absolute inset-0 motion-reduce:hidden"></div>
    <div v-if="icon" class="pointer-events-none absolute inset-0 flex items-center justify-center">
      <span class="flex h-11 w-11 items-center justify-center rounded-xl bg-white shadow-md ring-1 ring-black/5">
        <img :src="icon" alt="" class="h-7 w-7 opacity-60" />
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  size?: number
  icon?: string
}>(), {
  size: 196,
  icon: '',
})

const GRID = 25
const FINDERS = [
  { x: 0, y: 0 },
  { x: GRID - 7, y: 0 },
  { x: 0, y: GRID - 7 },
]

function finderPath(x: number, y: number): string {
  // 7x7 outer square with a 5x5 hole (even-odd fill).
  return `M${x} ${y}h7v7h-7z M${x + 1} ${y + 1}v5h5v-5z`
}

function inFinderZone(x: number, y: number): boolean {
  return FINDERS.some((f) => x >= f.x - 1 && x <= f.x + 7 && y >= f.y - 1 && y <= f.y + 7)
}

function inLogoZone(x: number, y: number): boolean {
  const mid = Math.floor(GRID / 2)
  return Math.abs(x - mid) <= 3 && Math.abs(y - mid) <= 3
}

// Deterministic pseudo-random pattern so the placeholder looks like a QR code
// but stays identical between renders (and in snapshots).
function buildModules(): Array<{ x: number; y: number }> {
  const cells: Array<{ x: number; y: number }> = []
  let seed = 20260
  for (let y = 0; y < GRID; y++) {
    for (let x = 0; x < GRID; x++) {
      seed = (seed * 1103515245 + 12345) % 2147483648
      if (inFinderZone(x, y) || inLogoZone(x, y)) continue
      if (seed % 100 < 42) cells.push({ x, y })
    }
  }
  return cells
}

const MODULES = buildModules()
</script>

<style scoped>
.qr-skeleton-shimmer {
  background: linear-gradient(
    100deg,
    transparent 20%,
    rgba(255, 255, 255, 0.75) 50%,
    transparent 80%
  );
  transform: translateX(-100%);
  animation: qr-skeleton-sweep 1.4s ease-in-out infinite;
}

@keyframes qr-skeleton-sweep {
  to {
    transform: translateX(100%);
  }
}
</style>
