<template>
  <aside class="purchase-right-rail" data-testid="purchase-right-rail" aria-hidden="true">
    <img
      class="purchase-right-rail__art"
      src="/assets/purchase/purchase-right-rail-transparent.webp"
      width="797"
      height="1973"
      alt=""
      aria-hidden="true"
      draggable="false"
      decoding="async"
      loading="lazy"
    />
  </aside>
</template>

<style scoped>
.purchase-right-rail {
  min-width: 0;
  padding-top: clamp(4.75rem, 7vw, 6.5rem);
  pointer-events: none;
  user-select: none;
}

.purchase-right-rail__art {
  display: block;
  width: 100%;
  height: auto;
  max-width: none;
  object-fit: contain;
  pointer-events: none;
  user-select: none;
  -webkit-user-drag: none;
}

@media (max-width: 1179px) {
  .purchase-right-rail {
    display: none;
  }
}
</style>
