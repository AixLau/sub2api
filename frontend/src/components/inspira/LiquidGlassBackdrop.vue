<template>
  <div
    class="liquid-glass-backdrop pointer-events-none absolute inset-0 overflow-hidden"
    data-testid="liquid-glass-backdrop"
    aria-hidden="true"
  ></div>
</template>

<style scoped>
.liquid-glass-backdrop {
  background: rgb(var(--color-surface-raised) / 0.02);
  -webkit-backdrop-filter: blur(18px) saturate(1.08);
  backdrop-filter: blur(18px) saturate(1.08);
  transform: translateZ(0);
  will-change: opacity, backdrop-filter;
}
</style>
