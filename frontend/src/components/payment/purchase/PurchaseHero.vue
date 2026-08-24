<template>
  <section
    class="purchase-hero"
    data-testid="purchase-hero"
    aria-labelledby="purchase-hero-title"
  >
    <div class="purchase-hero__copy">
      <h1 id="purchase-hero-title" class="sr-only">
        <span>{{ t('payment.purchaseHero.lineOne') }}</span>
        <span>{{ t('payment.purchaseHero.lineTwoLead') }}{{ t('payment.purchaseHero.lineTwoAccent') }}</span>
      </h1>
      <img
        data-testid="purchase-headline-art"
        class="purchase-hero__headline-art"
        src="/assets/purchase/purchase-headline-transparent.webp"
        width="1899"
        height="828"
        alt=""
        aria-hidden="true"
        draggable="false"
        decoding="async"
        fetchpriority="high"
      />
      <p class="purchase-hero__security-line">
        {{ t('payment.purchaseHero.securityLine') }}
      </p>
    </div>

    <PurchaseDecorations />
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PurchaseDecorations from './PurchaseDecorations.vue'

const { t } = useI18n()
</script>

<style scoped>
.purchase-hero {
  position: relative;
  isolation: auto;
  min-height: 15rem;
  overflow: visible;
  border: 0;
  border-radius: 1rem;
  background:
    radial-gradient(circle at 56% 32%, rgb(224 231 255 / 82%), transparent 27%),
    radial-gradient(circle at 95% 0%, rgb(244 114 182 / 10%), transparent 22%),
    linear-gradient(105deg, rgb(255 255 255 / 96%), rgb(248 250 252 / 90%) 53%, rgb(238 242 255 / 84%));
  box-shadow: none;
}

.purchase-hero::before {
  position: absolute;
  inset: 0;
  z-index: 0;
  background-image:
    linear-gradient(rgb(99 102 241 / 6%) 1px, transparent 1px),
    linear-gradient(90deg, rgb(99 102 241 / 6%) 1px, transparent 1px);
  background-size: 2.35rem 2.35rem;
  content: '';
  mask-image: linear-gradient(90deg, #000, transparent 72%);
}

.purchase-hero::after {
  position: absolute;
  right: -2.5rem;
  top: -2.75rem;
  z-index: 0;
  width: 13rem;
  height: 13rem;
  border-radius: 999px;
  background-image: radial-gradient(circle, rgb(79 70 229 / 38%) 1.3px, transparent 1.5px);
  background-size: 0.75rem 0.75rem;
  content: '';
  opacity: 0.62;
  transform: rotate(12deg);
}

.purchase-hero__copy {
  position: absolute;
  top: 0.85rem;
  left: clamp(1.5rem, 3.8vw, 3.25rem);
  z-index: 3;
  width: min(61%, 28rem);
}

.purchase-hero__headline-art {
  display: block;
  width: 100%;
  height: auto;
  max-width: 100%;
  object-fit: contain;
  pointer-events: none;
  user-select: none;
  -webkit-user-drag: none;
}

.purchase-hero__security-line {
  position: absolute;
  top: 12.6rem;
  left: 7rem;
  display: inline-flex;
  align-items: center;
  min-height: 1.6rem;
  margin: 0;
  padding: 0.28rem 0.9rem;
  border-right: 0.35rem solid #6048ff;
  background: #0b0b10;
  color: #f8fafc;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.66rem;
  font-weight: 800;
  letter-spacing: 0.16em;
  line-height: 1.1;
  text-transform: uppercase;
  transform: skewX(-8deg);
}

:global(.dark) .purchase-hero {
  background:
    radial-gradient(circle at 54% 24%, rgb(67 56 202 / 18%), transparent 29%),
    radial-gradient(circle at 94% 4%, rgb(190 24 93 / 15%), transparent 23%),
    linear-gradient(135deg, rgb(24 24 40 / 98%), rgb(20 24 43 / 98%) 48%, rgb(29 25 54 / 96%));
  box-shadow: none;
}

:global(.dark) .purchase-hero::before {
  background-image:
    linear-gradient(rgb(165 180 252 / 8%) 1px, transparent 1px),
    linear-gradient(90deg, rgb(165 180 252 / 8%) 1px, transparent 1px);
}

@media (max-width: 1279px) {
  .purchase-hero {
    min-height: 15rem;
  }

  .purchase-hero__copy {
    top: 0.75rem;
    width: min(61%, 26rem);
  }
}

@media (max-width: 1023px) {
  .purchase-hero {
    min-height: 14rem;
    overflow: hidden;
    border-radius: 1rem;
  }

  .purchase-hero__copy {
    top: 1.25rem;
    width: 52%;
  }

  .purchase-hero__security-line {
    position: static;
    inset: auto;
    margin: 1.05rem 0 0 3rem;
    padding-inline: 0.65rem;
    font-size: 0.58rem;
    letter-spacing: 0.11em;
  }

}

@media (max-width: 767px) {
  .purchase-hero {
    display: grid;
    min-height: 0;
    gap: 1.5rem;
    padding: 1.75rem 1rem 1.25rem;
    border-radius: 1.4rem;
  }

  .purchase-hero::before {
    mask-image: none;
    opacity: 0.65;
  }

  .purchase-hero::after {
    width: 9rem;
    height: 9rem;
  }

  .purchase-hero__copy {
    position: relative;
    inset: auto;
    width: 100%;
  }

  .purchase-hero__copy {
    padding-inline: 0.4rem;
  }

  .purchase-hero__headline-art {
    width: 100%;
  }

  .purchase-hero__security-line {
    margin: 1rem 0 0;
    font-size: clamp(0.55rem, 2.4vw, 0.66rem);
  }

}

@media (prefers-reduced-motion: reduce) {
  .purchase-hero *,
  .purchase-hero *::before,
  .purchase-hero *::after {
    scroll-behavior: auto !important;
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
