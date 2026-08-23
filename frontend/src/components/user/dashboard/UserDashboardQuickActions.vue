<template>
  <section
    class="card quick-actions-panel"
    data-testid="dashboard-quick-actions"
    :aria-labelledby="headingId"
  >
    <header class="quick-actions-panel__header">
      <h2 :id="headingId" class="quick-actions-panel__title">
        {{ t('dashboard.quickActions') }}
      </h2>
    </header>

    <div class="quick-actions-panel__body">
      <nav class="quick-actions-primary" :aria-label="t('dashboard.quickActions')">
        <RouterLink
          v-for="action in primaryActions"
          :key="action.id"
          :to="action.to"
          :class="['quick-action-card', `quick-action-card--${action.tone}`]"
          :data-testid="`quick-action-${action.id}`"
        >
          <span class="quick-action-card__top">
            <span class="quick-action-card__icon" aria-hidden="true">
              <Icon :name="action.icon" size="lg" />
            </span>
            <Icon name="chevronRight" size="sm" class="quick-action-card__arrow" aria-hidden="true" />
          </span>
          <span class="quick-action-card__copy">
            <span class="quick-action-card__title">{{ t(action.titleKey) }}</span>
            <span class="quick-action-card__description">{{ t(action.descriptionKey) }}</span>
          </span>
        </RouterLink>
      </nav>

      <div class="quick-actions-panel__lower">
        <nav class="quick-actions-secondary" :aria-label="t('dashboard.quickActions')">
          <RouterLink
            v-for="action in secondaryActions"
            :key="action.id"
            :to="action.to"
            :class="['quick-action-compact', `quick-action-compact--${action.tone}`]"
            :data-testid="`quick-action-${action.id}`"
          >
            <span class="quick-action-compact__icon" aria-hidden="true">
              <Icon :name="action.icon" size="md" />
            </span>
            <span class="quick-action-compact__copy">
              <span class="quick-action-compact__title">{{ t(action.titleKey) }}</span>
              <span class="quick-action-compact__description">{{ t(action.descriptionKey) }}</span>
            </span>
            <Icon name="chevronRight" size="sm" class="quick-action-compact__arrow" aria-hidden="true" />
          </RouterLink>
        </nav>

        <div
          class="quick-actions-panel__mascot"
          data-testid="dashboard-quick-actions-bunny"
          aria-hidden="true"
        >
          <span class="quick-actions-panel__mascot-glow"></span>
          <img
            src="/assets/dashboard/mascot-game-bunny.png"
            alt=""
            aria-hidden="true"
            draggable="false"
            loading="lazy"
            decoding="async"
            width="180"
            height="180"
          />
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

type QuickActionIcon = 'key' | 'chart' | 'grid' | 'sparkles' | 'gift'
type QuickActionTone = 'blue' | 'green' | 'purple' | 'cyan' | 'amber'

interface QuickAction {
  id: string
  to: RouteLocationRaw
  icon: QuickActionIcon
  titleKey: string
  descriptionKey: string
  tone: QuickActionTone
}

const { t } = useI18n()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
const headingId = 'user-dashboard-quick-actions-title'
const showModelPlaza = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))

const primaryActions = computed<QuickAction[]>(() => [
  {
    id: 'keys',
    to: '/keys',
    icon: 'key',
    titleKey: 'dashboard.createApiKey',
    descriptionKey: 'dashboard.generateNewKey',
    tone: 'blue',
  },
  {
    id: 'usage',
    to: '/usage',
    icon: 'chart',
    titleKey: 'dashboard.viewUsage',
    descriptionKey: 'dashboard.checkDetailedLogs',
    tone: 'green',
  },
  ...(showModelPlaza.value
    ? [{
        id: 'model-plaza',
        to: { path: '/model-plaza', query: { embedded: '1' } },
        icon: 'grid' as const,
        titleKey: 'dashboard.modelPlaza',
        descriptionKey: 'dashboard.openModelPlaza',
        tone: 'purple' as const,
      }]
    : []),
])

const secondaryActions = computed<QuickAction[]>(() => [
  ...(canUseBatchImage.value
    ? [{
        id: 'batch-image',
        to: '/batch-image',
        icon: 'sparkles' as const,
        titleKey: 'dashboard.batchImageAgent',
        descriptionKey: 'dashboard.batchImageAgentDesc',
        tone: 'cyan' as const,
      }]
    : []),
  {
    id: 'redeem',
    to: '/redeem',
    icon: 'gift',
    titleKey: 'dashboard.redeemCode',
    descriptionKey: 'dashboard.addBalanceWithCode',
    tone: 'amber',
  },
])

onMounted(() => {
  void refreshBatchImageAccess()
})
</script>

<style scoped>
.quick-actions-panel {
  position: relative;
  isolation: isolate;
  display: flex;
  min-width: 0;
  min-height: 100%;
  flex-direction: column;
  overflow: visible;
  border: 1px solid rgba(132, 149, 190, 0.15);
  border-radius: 22px;
  background: transparent;
  box-shadow: 0 18px 50px rgba(80, 94, 142, 0.09);
  backdrop-filter: blur(20px) saturate(118%);
  -webkit-backdrop-filter: blur(20px) saturate(118%);
}

.quick-actions-panel::before {
  position: absolute;
  inset: 0;
  z-index: -1;
  border-radius: inherit;
  background:
    radial-gradient(circle at 95% 95%, rgba(139, 92, 255, 0.1), transparent 34%),
    linear-gradient(145deg, rgba(255, 255, 255, 0.96), rgba(251, 253, 255, 0.82));
  content: '';
  pointer-events: none;
}

.quick-actions-panel__header {
  display: flex;
  min-height: 62px;
  align-items: center;
  padding: 0.9rem clamp(1rem, 2vw, 1.5rem);
  border-bottom: 1px solid rgba(132, 149, 190, 0.12);
}

.quick-actions-panel__title {
  margin: 0;
  color: #1a2030;
  font-size: 1.0625rem;
  font-weight: 750;
  letter-spacing: -0.018em;
  line-height: 1.35;
}

.quick-actions-panel__body {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  padding: clamp(0.875rem, 1.5vw, 1.25rem);
}

.quick-actions-primary {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 7.5rem), 1fr));
  gap: 0.625rem;
}

.quick-action-card {
  --action-accent: #4978f5;
  --action-icon-bg: rgba(255, 255, 255, 0.68);
  --action-background: linear-gradient(145deg, rgba(232, 241, 255, 0.98), rgba(242, 247, 255, 0.82));
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 8.5rem;
  flex-direction: column;
  justify-content: space-between;
  gap: 0.9rem;
  padding: 0.875rem;
  overflow: hidden;
  border: 1px solid rgba(86, 124, 230, 0.13);
  border-radius: 17px;
  background: var(--action-background);
  color: inherit;
  text-decoration: none;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.8),
    0 8px 18px rgba(77, 94, 145, 0.05);
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    box-shadow 180ms ease;
}

.quick-action-card::after {
  position: absolute;
  right: -1.6rem;
  bottom: -2.2rem;
  width: 5rem;
  height: 5rem;
  border: 0.8rem solid color-mix(in srgb, var(--action-accent) 9%, transparent);
  border-radius: 50%;
  content: '';
  pointer-events: none;
}

.quick-action-card--green {
  --action-accent: #20ad68;
  --action-background: linear-gradient(145deg, rgba(229, 252, 238, 0.98), rgba(243, 253, 246, 0.84));
  border-color: rgba(37, 181, 105, 0.13);
}

.quick-action-card--purple {
  --action-accent: #8b5cff;
  --action-background: linear-gradient(145deg, rgba(242, 235, 255, 0.98), rgba(249, 245, 255, 0.84));
  border-color: rgba(139, 92, 255, 0.13);
}

.quick-action-card__top {
  position: relative;
  z-index: 1;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.5rem;
}

.quick-action-card__icon {
  display: inline-flex;
  width: 2.625rem;
  height: 2.625rem;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(255, 255, 255, 0.78);
  border-radius: 12px;
  background: var(--action-icon-bg);
  color: var(--action-accent);
  box-shadow: 0 5px 14px color-mix(in srgb, var(--action-accent) 10%, transparent);
  transition: transform 180ms ease;
}

.quick-action-card__arrow {
  flex: 0 0 auto;
  color: color-mix(in srgb, var(--action-accent) 64%, #97a0b2);
  transition: transform 180ms ease;
}

.quick-action-card__copy {
  position: relative;
  z-index: 1;
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.25rem;
}

.quick-action-card__title,
.quick-action-card__description {
  display: block;
  overflow-wrap: anywhere;
}

.quick-action-card__title {
  color: #222938;
  font-size: 0.8125rem;
  font-weight: 750;
  line-height: 1.35;
}

.quick-action-card__description {
  color: #6f7a91;
  font-size: 0.6875rem;
  font-weight: 500;
  line-height: 1.4;
}

.quick-actions-panel__lower {
  display: grid;
  min-width: 0;
  flex: 1;
  grid-template-columns: minmax(0, 1fr) clamp(7rem, 11vw, 11.25rem);
  align-items: end;
  gap: 0.625rem;
  margin-top: 0.75rem;
}

.quick-actions-secondary {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 7rem), 1fr));
  gap: 0.5rem;
}

.quick-action-compact {
  --compact-accent: #159fbe;
  display: grid;
  min-width: 0;
  min-height: 4.25rem;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 0.5rem;
  padding: 0.625rem;
  border: 1px solid rgba(83, 139, 183, 0.1);
  border-radius: 14px;
  background: rgba(245, 249, 253, 0.82);
  color: inherit;
  text-decoration: none;
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    background-color 180ms ease;
}

.quick-action-compact--amber {
  --compact-accent: #d88716;
}

.quick-action-compact__icon {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 10px;
  background: color-mix(in srgb, var(--compact-accent) 11%, white);
  color: var(--compact-accent);
}

.quick-action-compact__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.1rem;
}

.quick-action-compact__title,
.quick-action-compact__description {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quick-action-compact__title {
  color: #2a3140;
  font-size: 0.75rem;
  font-weight: 700;
}

.quick-action-compact__description {
  color: #7b8498;
  font-size: 0.625rem;
}

.quick-action-compact__arrow {
  color: #a3aec1;
  transition: transform 180ms ease;
}

.quick-actions-panel__mascot {
  position: relative;
  display: flex;
  width: 100%;
  max-width: 11.25rem;
  aspect-ratio: 1;
  align-self: end;
  align-items: flex-end;
  justify-content: center;
  justify-self: end;
  pointer-events: none;
  user-select: none;
}

.quick-actions-panel__mascot-glow {
  position: absolute;
  inset: 37% 3% 2%;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(139, 92, 255, 0.2), rgba(255, 79, 163, 0.08) 48%, transparent 72%);
  filter: blur(12px);
  pointer-events: none;
}

.quick-actions-panel__mascot img {
  position: relative;
  z-index: 1;
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  filter: drop-shadow(0 14px 18px rgba(80, 65, 145, 0.14));
  pointer-events: none;
  user-select: none;
}

.quick-action-card:focus-visible,
.quick-action-compact:focus-visible {
  outline: 3px solid rgba(79, 124, 255, 0.28);
  outline-offset: 2px;
}

@media (hover: hover) {
  .quick-action-card:hover,
  .quick-action-compact:hover {
    border-color: color-mix(in srgb, var(--action-accent, var(--compact-accent)) 25%, transparent);
    box-shadow: 0 10px 22px rgba(69, 87, 139, 0.09);
    transform: translateY(-2px);
  }

  .quick-action-card:hover .quick-action-card__icon {
    transform: scale(1.05);
  }

  .quick-action-card:hover .quick-action-card__arrow,
  .quick-action-compact:hover .quick-action-compact__arrow {
    transform: translateX(2px);
  }
}

:global(.dark .quick-actions-panel) {
  border-color: rgba(155, 174, 218, 0.14);
  box-shadow: 0 18px 50px rgba(1, 7, 20, 0.22);
}

:global(.dark .quick-actions-panel::before) {
  background:
    radial-gradient(circle at 95% 95%, rgba(139, 92, 255, 0.11), transparent 34%),
    linear-gradient(145deg, rgba(22, 33, 55, 0.94), rgba(17, 27, 47, 0.82));
}

:global(.dark .quick-actions-panel__header) {
  border-bottom-color: rgba(155, 174, 218, 0.12);
}

:global(.dark .quick-actions-panel__title),
:global(.dark .quick-action-card__title),
:global(.dark .quick-action-compact__title) {
  color: #f4f7ff;
}

:global(.dark .quick-action-card) {
  --action-icon-bg: rgba(25, 38, 64, 0.66);
  --action-background: linear-gradient(145deg, rgba(38, 62, 105, 0.54), rgba(27, 42, 69, 0.46));
  border-color: rgba(107, 143, 238, 0.15);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);
}

:global(.dark .quick-action-card--green) {
  --action-background: linear-gradient(145deg, rgba(28, 91, 65, 0.4), rgba(25, 55, 54, 0.42));
  border-color: rgba(62, 203, 132, 0.14);
}

:global(.dark .quick-action-card--purple) {
  --action-background: linear-gradient(145deg, rgba(76, 53, 126, 0.48), rgba(48, 40, 81, 0.42));
  border-color: rgba(157, 113, 255, 0.16);
}

:global(.dark .quick-action-card__description),
:global(.dark .quick-action-compact__description) {
  color: #a4aec2;
}

:global(.dark .quick-action-compact) {
  border-color: rgba(140, 162, 205, 0.1);
  background: rgba(30, 44, 69, 0.62);
}

:global(.dark .quick-action-compact__icon) {
  background: color-mix(in srgb, var(--compact-accent) 18%, #1a2944);
}

:global(.dark .quick-actions-panel__mascot img) {
  filter: brightness(0.9) saturate(0.9) drop-shadow(0 14px 18px rgba(1, 5, 20, 0.24));
}

@media (max-width: 639px) {
  .quick-actions-panel__header {
    min-height: 56px;
  }

  .quick-actions-primary {
    grid-template-columns: minmax(0, 1fr);
  }

  .quick-action-card {
    min-height: 6.75rem;
  }

  .quick-actions-panel__lower {
    grid-template-columns: minmax(0, 1fr);
  }

  .quick-actions-panel__mascot {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .quick-action-card,
  .quick-action-card__icon,
  .quick-action-card__arrow,
  .quick-action-compact,
  .quick-action-compact__arrow {
    transition: none;
  }

  .quick-action-card:hover,
  .quick-action-card:hover .quick-action-card__icon,
  .quick-action-card:hover .quick-action-card__arrow,
  .quick-action-compact:hover,
  .quick-action-compact:hover .quick-action-compact__arrow {
    transform: none;
  }
}

:global(html.reduce-motion) .quick-action-card,
:global(html.reduce-motion) .quick-action-card__icon,
:global(html.reduce-motion) .quick-action-card__arrow,
:global(html.reduce-motion) .quick-action-compact,
:global(html.reduce-motion) .quick-action-compact__arrow {
  transition: none;
}

:global(html.reduce-motion) .quick-action-card:hover,
:global(html.reduce-motion) .quick-action-card:hover .quick-action-card__icon,
:global(html.reduce-motion) .quick-action-card:hover .quick-action-card__arrow,
:global(html.reduce-motion) .quick-action-compact:hover,
:global(html.reduce-motion) .quick-action-compact:hover .quick-action-compact__arrow {
  transform: none;
}
</style>
