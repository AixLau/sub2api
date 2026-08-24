<template>
  <div
    class="purchase-decorations"
    data-testid="purchase-decorations"
    aria-hidden="true"
  >
    <img
      class="purchase-decoration purchase-decoration--robot"
      src="/assets/purchase/energy-gacha-robot-direct.webp"
      width="1536"
      height="1024"
      alt=""
      aria-hidden="true"
      draggable="false"
      decoding="async"
      fetchpriority="high"
    />
    <img
      class="purchase-decoration purchase-decoration--power-up"
      src="/assets/purchase/power-up-badge.webp"
      width="71"
      height="104"
      alt=""
      aria-hidden="true"
      draggable="false"
      decoding="async"
    />
  </div>
</template>

<style scoped>
.purchase-decorations {
  position: absolute;
  inset: 0;
  z-index: 6;
  overflow: visible;
  pointer-events: none;
  user-select: none;
}

.purchase-decorations::before {
  position: absolute;
  right: clamp(12rem, 27%, 18rem);
  bottom: -2.15rem;
  width: 11rem;
  height: 5.75rem;
  z-index: 0;
  background-image:
    radial-gradient(ellipse at 54% 52%, rgb(34 211 238 / 18%), transparent 68%),
    radial-gradient(circle, rgb(6 182 212 / 82%) 1.4px, transparent 1.9px);
  background-repeat: no-repeat, repeat;
  background-size: 100% 100%, 0.45rem 0.45rem;
  content: '';
  mask-image: radial-gradient(ellipse at 54% 52%, #000 9%, rgb(0 0 0 / 88%) 36%, transparent 78%);
  opacity: 0.78;
  transform: rotate(-6deg) skewX(-8deg);
}

.purchase-decorations::after {
  position: absolute;
  right: -0.2rem;
  bottom: -0.85rem;
  color: #dfff00;
  content: '✦  ✧';
  font-size: 1.15rem;
  font-weight: 950;
  letter-spacing: 1.9rem;
  line-height: 1;
  text-shadow: 0 0 0.7rem rgb(190 242 100 / 70%);
}

.purchase-decoration {
  position: absolute;
  display: block;
  max-width: none;
  object-fit: contain;
  pointer-events: none;
  user-select: none;
  -webkit-user-drag: none;
}

.purchase-decoration--robot {
  right: -0.35rem;
  bottom: -3rem;
  left: auto;
  width: clamp(23rem, 50%, 26rem);
  height: auto;
  filter: drop-shadow(0 1.25rem 1.5rem rgb(55 48 163 / 20%));
  animation: purchase-robot-float 8s ease-in-out infinite;
}

.purchase-decoration--power-up {
  top: 1.15rem;
  right: 20rem;
  left: auto;
  width: clamp(3.2rem, 7%, 3.8rem);
  height: auto;
  filter: drop-shadow(0 0.45rem 0.6rem rgb(190 242 100 / 28%));
  transform: rotate(-7deg);
}

@keyframes purchase-robot-float {
  0%, 100% {
    transform: translate3d(0, 0, 0);
  }

  50% {
    transform: translate3d(0, -5px, 0);
  }
}

@media (max-width: 1279px) {
  .purchase-decoration--robot {
    right: -0.25rem;
    bottom: -2.85rem;
    left: auto;
    width: clamp(22rem, 50%, 25rem);
  }

  .purchase-decoration--power-up {
    top: 1.25rem;
    right: 19.5rem;
    left: auto;
    width: 3.15rem;
  }
}

@media (max-width: 1023px) {
  .purchase-decoration--robot {
    right: -0.5rem;
    bottom: -1.25rem;
    left: auto;
    width: clamp(20rem, 42vw, 23rem);
    opacity: 0.94;
  }

  .purchase-decoration--power-up {
    top: auto;
    bottom: 1rem;
    right: 17rem;
    left: auto;
    width: 3rem;
  }

}

@media (max-width: 767px) {
  .purchase-decoration {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .purchase-decoration {
    animation: none !important;
  }
}
</style>
