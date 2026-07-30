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
.liquid-glass-backdrop {
  background:
    radial-gradient(circle at 18% 18%, rgb(255 255 255 / 0.92), transparent 32%),
    radial-gradient(circle at 82% 78%, rgb(226 232 240 / 0.74), transparent 34%),
    linear-gradient(145deg, #e2e8f0 0%, #f1f5f9 48%, #e2e8f0 100%);
}

.liquid-glass-backdrop__dots {
  position: absolute;
  inset: 0;
  background-image: radial-gradient(circle, rgb(30 41 59 / 0.72) 1.4px, transparent 1.6px);
  background-size: 18px 18px;
  mask-image: radial-gradient(ellipse at center, black 10%, transparent 72%);
  -webkit-mask-image: radial-gradient(ellipse at center, black 10%, transparent 72%);
}

</style>
