<template>
  <section
    class="purchase-balance-ticket"
    data-testid="purchase-balance-ticket"
    :aria-label="t('payment.rechargeUi.availableBalance')"
  >
    <img
      class="purchase-balance-ticket__art"
      src="/assets/purchase/balance-ticket-direct.webp"
      width="1678"
      height="937"
      alt=""
      aria-hidden="true"
      draggable="false"
      decoding="async"
    />
    <div class="purchase-balance-ticket__content">
      <div class="purchase-balance-ticket__heading">
        <span class="purchase-balance-ticket__kicker">BALANCE</span>
        <span class="purchase-balance-ticket__number" aria-hidden="true">01</span>
      </div>
      <p class="purchase-balance-ticket__label">
        {{ t('payment.rechargeUi.availableBalance') }}
      </p>
      <p class="purchase-balance-ticket__value">{{ displayBalance }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  formattedBalance: string
}>()

const { t } = useI18n()
const displayBalance = computed(() => props.formattedBalance.trim() || '—')
</script>

<style scoped>
.purchase-balance-ticket {
  position: relative;
  box-sizing: border-box;
  width: calc(100% + 1.25rem);
  max-width: 22rem;
  aspect-ratio: 1678 / 937;
  justify-self: center;
  min-width: 0;
  min-height: 0;
  padding: 1.15rem 1.35rem 1rem;
  overflow: visible;
  border: 0;
  background: transparent;
  box-shadow: none;
  color: #09090b;
  pointer-events: none;
  transform: none;
}

.purchase-balance-ticket__art {
  position: absolute;
  inset: 0;
  z-index: 0;
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  pointer-events: none;
  user-select: none;
  -webkit-user-drag: none;
}

.purchase-balance-ticket__content {
  position: absolute;
  inset: 17% 15% 19%;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  transform: rotate(4deg);
  transform-origin: 50% 32%;
}

.purchase-balance-ticket__heading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.6rem;
}

.purchase-balance-ticket__kicker {
  font-family: "Arial Black", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 950;
  letter-spacing: 0.12em;
}

.purchase-balance-ticket__number {
  display: inline-grid;
  width: 1.9rem;
  height: 1.9rem;
  place-items: center;
  border-radius: 0.42rem;
  background: linear-gradient(145deg, #4338ca, #7659ff);
  box-shadow: inset 0 0 0 1px rgb(255 255 255 / 48%);
  color: white;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.85rem;
  font-weight: 950;
  transform: rotate(2deg);
}

.purchase-balance-ticket__label {
  margin: 0.08rem 0 0;
  font-size: 0.72rem;
  font-weight: 650;
  letter-spacing: 0.01em;
}

.purchase-balance-ticket__value {
  max-width: 100%;
  margin: 0.12rem 0 0;
  overflow: hidden;
  font-family: Impact, "Arial Black", Inter, ui-sans-serif, system-ui, sans-serif;
  font-size: clamp(1.65rem, 2.15vw, 2.15rem);
  font-variant-numeric: tabular-nums lining-nums;
  font-weight: 900;
  letter-spacing: -0.025em;
  line-height: 1.05;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.dark) .purchase-balance-ticket {
  color: #111827;
}

@media (max-width: 1023px) {
  .purchase-balance-ticket {
    max-width: 22rem;
  }
}

@media (max-width: 767px) {
  .purchase-balance-ticket {
    width: 100%;
  }

  .purchase-balance-ticket__value {
    font-size: clamp(1.85rem, 10vw, 2.35rem);
  }
}
</style>
