<template>
  <div
    class="liquid-glass-backdrop pointer-events-none absolute inset-0 overflow-hidden"
    data-testid="liquid-glass-backdrop"
    aria-hidden="true"
  >
    <span class="liquid-glass-backdrop__dots"></span>
  </div>
</template>

<style scoped>
.liquid-glass-backdrop__dots {
  position: absolute;
  inset: 0;
  background-image: radial-gradient(circle, rgb(30 41 59 / 0.72) 1.4px, transparent 1.6px);
  background-size: 18px 18px;
  mask-image: radial-gradient(ellipse at center, black 10%, transparent 72%);
  -webkit-mask-image: radial-gradient(ellipse at center, black 10%, transparent 72%);
}
</style>
