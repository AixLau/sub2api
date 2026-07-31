<template>
  <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
    <div class="aurora-blob aurora-blob-a"></div>
    <div class="aurora-blob aurora-blob-b"></div>
    <div class="aurora-blob aurora-blob-c"></div>
  </div>
</template>

<style scoped>
.aurora-blob {
  position: absolute;
  border-radius: 9999px;
  filter: blur(80px);
  opacity: 0.3;
  will-change: transform;
}

.dark .aurora-blob {
  opacity: 0.22;
}

.aurora-blob-a {
  top: -25%;
  left: -10%;
  width: 55%;
  height: 65%;
  background: radial-gradient(circle at center, rgb(var(--color-brand-500) / 0.42), transparent 65%);
  animation: aurora-a 16s ease-in-out infinite alternate;
}

.aurora-blob-b {
  top: -10%;
  right: -15%;
  width: 60%;
  height: 70%;
  background: radial-gradient(
    circle at center,
    rgb(var(--color-brand-500) / 0.34),
    transparent 65%
  );
  animation: aurora-b 20s ease-in-out infinite alternate;
}

.aurora-blob-c {
  bottom: -30%;
  left: 25%;
  width: 55%;
  height: 65%;
  background: radial-gradient(
    circle at center,
    rgb(var(--color-accent-400) / 0.32),
    transparent 65%
  );
  animation: aurora-c 24s ease-in-out infinite alternate;
}

@keyframes aurora-a {
  to {
    transform: translate(18%, 12%) scale(1.15);
  }
}

@keyframes aurora-b {
  to {
    transform: translate(-15%, 18%) scale(0.9);
  }
}

@keyframes aurora-c {
  to {
    transform: translate(-20%, -15%) scale(1.2);
  }
}

@media (prefers-reduced-motion: reduce) {
  .aurora-blob {
    animation: none;
  }
}
</style>
