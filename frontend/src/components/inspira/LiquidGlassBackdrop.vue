<template>
  <div
    class="liquid-glass-backdrop pointer-events-none absolute inset-0 overflow-hidden"
    data-testid="liquid-glass-backdrop"
    aria-hidden="true"
  ></div>
</template>

<style scoped>
.liquid-glass-backdrop {
  background: rgb(255 255 255 / 0.04);
  -webkit-backdrop-filter: blur(18px) saturate(1.08);
  backdrop-filter: blur(18px) saturate(1.08);
}
</style>
