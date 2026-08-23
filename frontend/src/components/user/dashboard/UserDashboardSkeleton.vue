<template>
  <div
    data-testid="user-dashboard-skeleton"
    class="user-dashboard-skeleton"
    aria-busy="true"
    aria-live="polite"
  >
    <div class="user-dashboard-skeleton__stats" aria-hidden="true">
      <div
        v-for="i in 8"
        :key="i"
        class="user-dashboard-skeleton__surface user-dashboard-skeleton__stat"
      >
        <Skeleton variant="circle" :width="44" :height="44" />
        <div class="min-w-0 flex-1 space-y-2">
          <Skeleton height="10px" width="60%" />
          <Skeleton height="26px" width="48%" />
          <Skeleton height="10px" width="70%" />
        </div>
      </div>
    </div>

    <div
      class="user-dashboard-skeleton__surface user-dashboard-skeleton__activity"
      data-testid="user-dashboard-skeleton-activity"
      aria-hidden="true"
    >
      <div class="flex items-center justify-between gap-4">
        <Skeleton height="18px" width="126px" />
        <div class="flex items-center gap-5">
          <Skeleton v-for="i in 3" :key="i" height="12px" width="44px" />
        </div>
      </div>
      <div class="user-dashboard-skeleton__activity-grid"></div>
    </div>

    <div
      class="user-dashboard-skeleton__surface user-dashboard-skeleton__filters"
      aria-hidden="true"
    >
      <div class="user-dashboard-skeleton__filter-start">
        <Skeleton height="12px" width="72px" />
        <Skeleton height="40px" width="248px" />
        <Skeleton height="40px" width="88px" />
      </div>
      <Skeleton height="40px" width="116px" />
    </div>

    <div class="user-dashboard-skeleton__analysis" aria-hidden="true">
      <div class="user-dashboard-skeleton__surface user-dashboard-skeleton__panel">
        <Skeleton class="mb-5" height="16px" width="132px" />
        <div class="user-dashboard-skeleton__distribution">
          <Skeleton variant="circle" :width="176" :height="176" />
          <div class="w-full min-w-0 flex-1 space-y-3">
            <Skeleton v-for="i in 5" :key="i" height="22px" />
          </div>
        </div>
      </div>
      <div class="user-dashboard-skeleton__surface user-dashboard-skeleton__panel">
        <div class="mb-5 flex items-center justify-between gap-4">
          <Skeleton height="24px" width="142px" />
          <Skeleton height="20px" width="88px" />
        </div>
        <Skeleton height="208px" />
      </div>
    </div>

    <div class="user-dashboard-skeleton__bottom" aria-hidden="true">
      <div class="user-dashboard-skeleton__surface user-dashboard-skeleton__panel space-y-3">
        <div class="mb-4 flex items-center justify-between gap-4">
          <Skeleton height="18px" width="120px" />
          <Skeleton height="22px" width="72px" />
        </div>
        <Skeleton v-for="i in 3" :key="i" height="56px" />
      </div>
      <div class="user-dashboard-skeleton__surface user-dashboard-skeleton__panel space-y-3">
        <Skeleton class="mb-4" height="18px" width="108px" />
        <div class="user-dashboard-skeleton__quick-grid">
          <Skeleton v-for="i in 3" :key="i" height="104px" />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <Skeleton v-for="i in 2" :key="i" height="38px" />
        </div>
      </div>
    </div>

  </div>
</template>

<script setup lang="ts">
import Skeleton from '@/components/common/Skeleton.vue'
</script>

<style scoped>
.user-dashboard-skeleton {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: clamp(1rem, 1.25vw, 1.25rem);
}

.user-dashboard-skeleton__stats {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
}

.user-dashboard-skeleton__surface {
  min-width: 0;
  border: 1px solid rgb(var(--color-line-subtle) / 0.78);
  border-radius: 20px;
  background: rgb(var(--color-surface-panel) / 0.82);
  box-shadow: 0 14px 34px rgb(var(--color-shadow) / 0.07);
  backdrop-filter: blur(18px);
}

.user-dashboard-skeleton__stat {
  display: flex;
  min-height: 116px;
  align-items: flex-start;
  gap: 14px;
  padding: 16px;
}

.user-dashboard-skeleton__filters {
  display: flex;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 9px 14px;
}

.user-dashboard-skeleton__filter-start {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.user-dashboard-skeleton__analysis {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
}

.user-dashboard-skeleton__panel {
  padding: 18px;
}

.user-dashboard-skeleton__distribution {
  display: flex;
  min-width: 0;
  min-height: 208px;
  align-items: center;
  gap: 18px;
}

.user-dashboard-skeleton__bottom {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
}

.user-dashboard-skeleton__quick-grid {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.user-dashboard-skeleton__activity {
  padding: 18px;
}

.user-dashboard-skeleton__activity-grid {
  height: 126px;
  margin-top: 18px;
  border-radius: 8px;
  background-image: radial-gradient(circle at center, rgb(var(--color-content-tertiary) / 0.18) 0 4px, transparent 4.5px);
  background-position: -1px -1px;
  background-size: 17px 17px;
}

@media (min-width: 640px) {
  .user-dashboard-skeleton__stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1024px) {
  .user-dashboard-skeleton__analysis {
    grid-template-columns: minmax(0, 0.85fr) minmax(0, 1.15fr);
  }

  .user-dashboard-skeleton__bottom {
    grid-template-columns: minmax(0, 2fr) minmax(19rem, 1fr);
  }
}

@media (min-width: 1280px) {
  .user-dashboard-skeleton__stats {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (max-width: 767px) {
  .user-dashboard-skeleton__filters,
  .user-dashboard-skeleton__filter-start {
    align-items: stretch;
    flex-direction: column;
  }

  .user-dashboard-skeleton__filters :deep(> *),
  .user-dashboard-skeleton__filter-start :deep(> *) {
    width: 100% !important;
  }
}

@media (max-width: 639px) {
  .user-dashboard-skeleton__surface {
    border-radius: 18px;
  }

  .user-dashboard-skeleton__distribution {
    align-items: center;
    flex-direction: column;
  }

  .user-dashboard-skeleton__quick-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .user-dashboard-skeleton__activity {
    overflow: hidden;
    padding: 15px;
  }

  .user-dashboard-skeleton__activity > div:first-child {
    align-items: flex-start;
    flex-direction: column;
  }

  .user-dashboard-skeleton__activity-grid {
    width: 620px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .user-dashboard-skeleton :deep(*) {
    animation: none !important;
  }
}

:global(html.reduce-motion) .user-dashboard-skeleton :deep(*) {
  animation: none !important;
}
</style>
