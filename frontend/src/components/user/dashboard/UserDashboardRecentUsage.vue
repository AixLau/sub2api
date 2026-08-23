<template>
  <section
    class="card recent-usage-panel"
    data-testid="dashboard-recent-usage"
    :aria-labelledby="headingId"
    :aria-busy="loading"
  >
    <header class="recent-usage-panel__header">
      <h2 :id="headingId" class="recent-usage-panel__title">
        {{ t('dashboard.recentUsage') }}
      </h2>
      <span class="recent-usage-panel__period">{{ t('dashboard.last7Days') }}</span>
    </header>

    <div class="recent-usage-panel__body">
      <div
        v-if="loading && data.length === 0"
        class="recent-usage-panel__state"
        data-testid="recent-usage-loading"
      >
        <LoadingSpinner size="lg" />
      </div>

      <div
        v-else-if="data.length === 0"
        class="recent-usage-panel__empty"
        data-testid="recent-usage-empty"
      >
        <EmptyState
          :title="t('dashboard.noUsageRecords')"
          :description="t('dashboard.startUsingApi')"
        />
      </div>

      <template v-else>
        <AnimatedList
          tag="ul"
          :stagger="60"
          class="recent-usage-list"
          data-testid="recent-usage-list"
        >
          <li
            v-for="log in data"
            :key="log.id"
            class="recent-usage-list__item"
            data-testid="recent-usage-item"
          >
            <RouterLink to="/usage" class="recent-usage-list__link">
              <span class="recent-usage-list__icon" aria-hidden="true">
                <Icon name="beaker" size="md" />
              </span>

              <span class="recent-usage-list__details">
                <span class="recent-usage-list__model" :title="log.model">
                  {{ log.model }}
                </span>
                <time class="recent-usage-list__time" :datetime="log.created_at">
                  {{ formatDateTime(log.created_at) }}
                </time>
              </span>

              <span class="recent-usage-list__metrics">
                <span class="recent-usage-list__cost">${{ formatCost(log.actual_cost) }}</span>
                <span class="recent-usage-list__tokens">
                  {{ formatTokens(log) }} {{ t('dashboard.tokens') }}
                </span>
              </span>

              <Icon
                name="chevronRight"
                size="sm"
                class="recent-usage-list__arrow"
                aria-hidden="true"
              />
            </RouterLink>
          </li>
        </AnimatedList>

        <RouterLink
          to="/usage"
          class="recent-usage-panel__view-all"
          data-testid="recent-usage-view-all"
        >
          <span>{{ t('dashboard.viewAllUsage') }}</span>
          <Icon name="arrowRight" size="sm" aria-hidden="true" />
        </RouterLink>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import AnimatedList from '@/components/inspira/AnimatedList.vue'
import { formatDateTime } from '@/utils/format'
import type { UsageLog } from '@/types'

defineProps<{
  data: UsageLog[]
  loading: boolean
}>()

const { t } = useI18n()
const headingId = 'user-dashboard-recent-usage-title'

const formatCost = (cost: number) => (Number.isFinite(cost) ? cost : 0).toFixed(4)
const formatTokens = (log: UsageLog) => (
  log.input_tokens + log.output_tokens
).toLocaleString()
</script>

<style scoped>
.recent-usage-panel {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 100%;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid rgba(132, 149, 190, 0.15);
  border-radius: 22px;
  background: linear-gradient(145deg, rgba(255, 255, 255, 0.96), rgba(251, 253, 255, 0.82));
  box-shadow: 0 18px 50px rgba(80, 94, 142, 0.09);
  backdrop-filter: blur(20px) saturate(118%);
  -webkit-backdrop-filter: blur(20px) saturate(118%);
}

.recent-usage-panel::before {
  position: absolute;
  top: -5rem;
  left: -4rem;
  width: 13rem;
  height: 10rem;
  border-radius: 999px;
  background: radial-gradient(circle, rgba(39, 199, 232, 0.09), transparent 70%);
  content: '';
  pointer-events: none;
}

.recent-usage-panel__header {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 62px;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.9rem clamp(1rem, 2vw, 1.5rem);
  border-bottom: 1px solid rgba(132, 149, 190, 0.12);
}

.recent-usage-panel__title {
  min-width: 0;
  margin: 0;
  color: #1a2030;
  font-size: 1.0625rem;
  font-weight: 750;
  letter-spacing: -0.018em;
  line-height: 1.35;
}

.recent-usage-panel__period {
  flex: 0 0 auto;
  padding: 0.4rem 0.75rem;
  border: 1px solid rgba(117, 134, 176, 0.12);
  border-radius: 999px;
  background: rgba(246, 248, 253, 0.82);
  color: #667089;
  font-size: 0.75rem;
  font-weight: 600;
  line-height: 1;
}

.recent-usage-panel__body {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  padding: clamp(0.875rem, 1.5vw, 1.25rem);
}

.recent-usage-panel__state,
.recent-usage-panel__empty {
  display: flex;
  min-height: 15rem;
  flex: 1;
  align-items: center;
  justify-content: center;
}

.recent-usage-panel__empty {
  padding-block: 1rem;
}

.recent-usage-list {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.625rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.recent-usage-list__item {
  min-width: 0;
}

.recent-usage-list__link {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: clamp(0.625rem, 1.2vw, 0.875rem);
  padding: 0.75rem clamp(0.75rem, 1.5vw, 1rem);
  border: 1px solid rgba(115, 145, 184, 0.06);
  border-radius: 15px;
  background: linear-gradient(100deg, rgba(244, 248, 253, 0.96), rgba(247, 250, 255, 0.8));
  color: inherit;
  text-decoration: none;
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    background-color 180ms ease,
    box-shadow 180ms ease;
}

.recent-usage-list__link:hover {
  border-color: rgba(39, 199, 232, 0.15);
  background: linear-gradient(100deg, rgba(248, 252, 255, 1), rgba(242, 249, 255, 0.94));
  box-shadow: 0 8px 20px rgba(77, 104, 150, 0.07);
  transform: translateY(-1px);
}

.recent-usage-list__link:focus-visible,
.recent-usage-panel__view-all:focus-visible {
  outline: 3px solid rgba(39, 199, 232, 0.28);
  outline-offset: 2px;
}

.recent-usage-list__icon {
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  flex: 0 0 2.75rem;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(39, 199, 232, 0.14);
  border-radius: 13px;
  background: linear-gradient(145deg, rgba(220, 249, 255, 0.96), rgba(232, 247, 255, 0.8));
  color: #159fbe;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.9);
}

.recent-usage-list__details {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 0.2rem;
}

.recent-usage-list__model,
.recent-usage-list__time,
.recent-usage-list__cost,
.recent-usage-list__tokens {
  display: block;
}

.recent-usage-list__model {
  overflow: hidden;
  color: #222938;
  font-size: 0.875rem;
  font-weight: 700;
  line-height: 1.3;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-usage-list__time {
  overflow: hidden;
  color: #7b8498;
  font-size: 0.75rem;
  font-weight: 500;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recent-usage-list__metrics {
  display: flex;
  min-width: 5.75rem;
  flex: 0 0 auto;
  flex-direction: column;
  gap: 0.2rem;
  text-align: right;
}

.recent-usage-list__cost {
  color: #17a65b;
  font-size: 0.875rem;
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  line-height: 1.3;
}

.recent-usage-list__tokens {
  color: #7b8498;
  font-size: 0.75rem;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 1.35;
  white-space: nowrap;
}

.recent-usage-list__arrow {
  flex: 0 0 auto;
  color: #a3aec1;
  transition:
    color 180ms ease,
    transform 180ms ease;
}

.recent-usage-list__link:hover .recent-usage-list__arrow {
  color: #18a8c7;
  transform: translateX(2px);
}

.recent-usage-panel__view-all {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  align-self: center;
  justify-content: center;
  gap: 0.5rem;
  margin-top: auto;
  padding: 0.9rem 1rem 0.25rem;
  color: #366ee8;
  font-size: 0.8125rem;
  font-weight: 700;
  text-decoration: none;
  transition: color 180ms ease;
}

.recent-usage-panel__view-all:hover {
  color: #2257cb;
}

:global(.dark .recent-usage-panel) {
  border-color: rgba(155, 174, 218, 0.14);
  background: linear-gradient(145deg, rgba(22, 33, 55, 0.94), rgba(17, 27, 47, 0.82));
  box-shadow: 0 18px 50px rgba(1, 7, 20, 0.22);
}

:global(.dark .recent-usage-panel__header) {
  border-bottom-color: rgba(155, 174, 218, 0.12);
}

:global(.dark .recent-usage-panel__title),
:global(.dark .recent-usage-list__model) {
  color: #f4f7ff;
}

:global(.dark .recent-usage-panel__period) {
  border-color: rgba(155, 174, 218, 0.12);
  background: rgba(41, 54, 80, 0.56);
  color: #aeb9ce;
}

:global(.dark .recent-usage-list__link) {
  border-color: rgba(141, 164, 207, 0.06);
  background: linear-gradient(100deg, rgba(30, 44, 70, 0.72), rgba(24, 38, 62, 0.56));
}

:global(.dark .recent-usage-list__link:hover) {
  border-color: rgba(54, 205, 234, 0.18);
  background: linear-gradient(100deg, rgba(35, 51, 80, 0.86), rgba(26, 43, 70, 0.72));
  box-shadow: 0 8px 22px rgba(0, 5, 18, 0.18);
}

:global(.dark .recent-usage-list__icon) {
  border-color: rgba(46, 202, 229, 0.16);
  background: linear-gradient(145deg, rgba(21, 133, 158, 0.22), rgba(37, 90, 135, 0.16));
  color: #58dbf2;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);
}

:global(.dark .recent-usage-list__time),
:global(.dark .recent-usage-list__tokens) {
  color: #94a1b8;
}

:global(.dark .recent-usage-list__cost) {
  color: #54dc94;
}

:global(.dark .recent-usage-panel__view-all) {
  color: #8bb0ff;
}

:global(.dark .recent-usage-panel__view-all:hover) {
  color: #b2c9ff;
}

@media (max-width: 479px) {
  .recent-usage-panel__header {
    min-height: 56px;
  }

  .recent-usage-list__link {
    gap: 0.625rem;
    padding-inline: 0.625rem;
  }

  .recent-usage-list__icon {
    width: 2.5rem;
    height: 2.5rem;
    flex-basis: 2.5rem;
  }

  .recent-usage-list__metrics {
    min-width: 4.75rem;
  }

  .recent-usage-list__tokens {
    max-width: 5.5rem;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .recent-usage-list__arrow {
    width: 0.875rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .recent-usage-list__link,
  .recent-usage-list__arrow,
  .recent-usage-panel__view-all {
    transition: none;
  }

  .recent-usage-list__link:hover,
  .recent-usage-list__link:hover .recent-usage-list__arrow {
    transform: none;
  }
}

:global(html.reduce-motion) .recent-usage-list__link,
:global(html.reduce-motion) .recent-usage-list__arrow,
:global(html.reduce-motion) .recent-usage-panel__view-all {
  transition: none;
}

:global(html.reduce-motion) .recent-usage-list__link:hover,
:global(html.reduce-motion) .recent-usage-list__link:hover .recent-usage-list__arrow {
  transform: none;
}
</style>
