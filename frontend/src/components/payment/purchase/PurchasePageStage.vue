<template>
  <div class="purchase-page-stage">
    <PurchaseHero class="purchase-page-stage__hero" />
    <PurchaseBalanceTicket
      class="purchase-page-stage__balance"
      :formatted-balance="formattedBalance"
    />
    <div class="purchase-page-stage__business">
      <slot />
    </div>
    <div class="purchase-page-stage__trust">
      <slot name="trust" />
    </div>
  </div>
</template>

<script setup lang="ts">
import PurchaseBalanceTicket from './PurchaseBalanceTicket.vue'
import PurchaseHero from './PurchaseHero.vue'

defineProps<{
  formattedBalance: string
}>()
</script>

<style scoped>
.purchase-page-stage {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.75rem;
}

.purchase-page-stage__hero,
.purchase-page-stage__balance,
.purchase-page-stage__business,
.purchase-page-stage__trust {
  min-width: 0;
}

.purchase-page-stage__business > :deep(.purchase-business-panel),
.purchase-page-stage__trust > :deep(.recharge-trust-bar) {
  width: 100%;
}

@media (min-width: 1024px) {
  .purchase-page-stage {
    grid-template-columns: minmax(0, 1fr) var(--purchase-summary-width, 17rem);
    grid-template-areas:
      "hero balance"
      "business business"
      "trust trust";
    column-gap: var(--purchase-column-gap, 0.9rem);
    row-gap: 0.65rem;
  }

  .purchase-page-stage__hero {
    z-index: auto;
    grid-area: hero;
  }

  .purchase-page-stage__balance {
    z-index: 2;
    grid-area: balance;
    align-self: start;
    margin-top: 0.45rem;
  }

  .purchase-page-stage__business {
    position: relative;
    z-index: 5;
    grid-area: business;
  }

  .purchase-page-stage__trust {
    grid-area: trust;
  }

}

@media (max-width: 1023px) {
  .purchase-page-stage__hero {
    order: 1;
  }

  .purchase-page-stage__balance {
    order: 2;
  }

  .purchase-page-stage__business {
    order: 3;
  }

  .purchase-page-stage__trust {
    order: 4;
  }

}
</style>
