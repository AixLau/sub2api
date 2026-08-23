<template>
  <article
    v-if="!hidden"
    :class="rootClass"
    :data-stat-theme="theme"
    data-testid="stat-card"
  >
    <div class="dashboard-stat-card__content">
      <span class="dashboard-stat-card__icon" aria-hidden="true">
        <Icon :name="icon" size="lg" :stroke-width="1.9" />
      </span>

      <div class="dashboard-stat-card__copy">
        <p class="dashboard-stat-card__title">{{ title }}</p>
        <p
          :class="['dashboard-stat-card__value', valueClass]"
          :title="displayValue"
        >
          {{ displayValue }}
        </p>
        <p
          v-if="description"
          class="dashboard-stat-card__description"
          :title="description"
        >
          {{ description }}
        </p>
      </div>
    </div>

    <svg
      v-if="chartKind === 'wave'"
      class="dashboard-stat-card__decoration dashboard-stat-card__wave"
      viewBox="0 0 180 64"
      preserveAspectRatio="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        class="dashboard-stat-card__wave-fill"
        d="M0 64V58C18 58 25 45 42 45s23 12 39 12c18 0 24-31 45-31 18 0 20 18 32 18 10 0 13-18 22-18v38H0Z"
      />
      <path
        class="dashboard-stat-card__wave-line"
        d="M0 58C18 58 25 45 42 45s23 12 39 12c18 0 24-31 45-31 18 0 20 18 32 18 10 0 13-18 22-18"
      />
    </svg>

    <svg
      v-else-if="chartKind === 'bars'"
      class="dashboard-stat-card__decoration dashboard-stat-card__bars"
      viewBox="0 0 150 64"
      preserveAspectRatio="xMaxYMax meet"
      aria-hidden="true"
      focusable="false"
    >
      <rect x="10" y="51" width="9" height="7" rx="4.5" />
      <rect x="25" y="45" width="9" height="13" rx="4.5" />
      <rect x="40" y="39" width="9" height="19" rx="4.5" />
      <rect x="55" y="30" width="9" height="28" rx="4.5" />
      <rect x="70" y="18" width="9" height="40" rx="4.5" />
      <rect x="85" y="27" width="9" height="31" rx="4.5" />
      <rect x="100" y="10" width="9" height="48" rx="4.5" />
      <rect x="115" y="2" width="9" height="56" rx="4.5" />
      <rect x="130" y="18" width="9" height="40" rx="4.5" />
    </svg>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type {
  DashboardStatAccent,
  DashboardStatDecoration,
  DashboardStatIcon,
  DashboardStatTheme,
} from './dashboardStatThemes'

const props = withDefaults(defineProps<{
  title: string
  value: string | number | null | undefined
  description?: string | null
  icon: DashboardStatIcon
  theme?: DashboardStatTheme
  accent?: DashboardStatAccent
  decorativeChart?: DashboardStatDecoration
  valueClass?: string
  hidden?: boolean
}>(), {
  description: '',
  theme: 'blue',
  accent: false,
  decorativeChart: 'wave',
  valueClass: '',
  hidden: false,
})

const displayValue = computed(() => {
  if (props.value === null || props.value === undefined || props.value === '') return '—'
  if (typeof props.value === 'number' && !Number.isFinite(props.value)) return '—'
  return String(props.value)
})

const chartKind = computed<'wave' | 'bars' | null>(() => {
  if (props.decorativeChart === false || props.decorativeChart === 'none') return null
  return props.decorativeChart === 'bars' ? 'bars' : 'wave'
})

const rootClass = computed(() => [
  'dashboard-stat-card',
  `dashboard-stat-card--${props.theme}`,
  {
    'dashboard-stat-card--accent-value':
      props.accent === true || props.accent === 'value' || props.accent === 'both',
    'dashboard-stat-card--accent-description':
      props.accent === 'description' || props.accent === 'both',
  },
])
</script>

<style scoped>
.dashboard-stat-card {
  --stat-accent-rgb: 79 124 255;
  --stat-surface-alpha: 0.1;
  position: relative;
  isolation: isolate;
  min-width: 0;
  min-height: 7.25rem;
  overflow: hidden;
  border: 1px solid rgb(255 255 255 / 0.78);
  border-radius: 1.25rem;
  background:
    linear-gradient(118deg, rgb(var(--stat-accent-rgb) / var(--stat-surface-alpha)), transparent 46%),
    linear-gradient(145deg, rgb(255 255 255 / 0.96), rgb(250 251 255 / 0.88));
  box-shadow:
    0 12px 32px rgb(47 56 90 / 0.07),
    inset 0 1px 0 rgb(255 255 255 / 0.85);
  backdrop-filter: blur(16px);
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    box-shadow 180ms ease;
}

.dashboard-stat-card:hover {
  transform: translateY(-2px);
  border-color: rgb(var(--stat-accent-rgb) / 0.2);
  box-shadow:
    0 16px 36px rgb(47 56 90 / 0.1),
    inset 0 1px 0 rgb(255 255 255 / 0.9);
}

.dashboard-stat-card--pink {
  --stat-accent-rgb: 255 79 163;
  --stat-surface-alpha: 0.24;
  border-color: rgb(255 255 255 / 0.92);
  background:
    radial-gradient(circle at 13% 0%, rgb(255 255 255 / 0.78), transparent 38%),
    linear-gradient(128deg, rgb(255 241 248 / 0.98) 0%, rgb(255 222 238 / 0.97) 54%, rgb(255 213 235 / 0.94) 100%);
  box-shadow:
    0 14px 34px rgb(197 75 137 / 0.12),
    inset 0 1px 0 rgb(255 255 255 / 0.92);
}

.dashboard-stat-card--blue {
  --stat-accent-rgb: 79 124 255;
}

.dashboard-stat-card--green {
  --stat-accent-rgb: 42 199 116;
}

.dashboard-stat-card--purple {
  --stat-accent-rgb: 139 92 246;
  --stat-surface-alpha: 0.12;
}

.dashboard-stat-card--amber {
  --stat-accent-rgb: 255 159 67;
  --stat-surface-alpha: 0.12;
}

.dashboard-stat-card--indigo {
  --stat-accent-rgb: 82 99 255;
}

.dashboard-stat-card--violet {
  --stat-accent-rgb: 151 71 255;
  --stat-surface-alpha: 0.12;
}

.dashboard-stat-card--rose {
  --stat-accent-rgb: 255 65 125;
  --stat-surface-alpha: 0.12;
}

.dashboard-stat-card__content {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: flex-start;
  gap: 0.875rem;
  min-width: 0;
  padding: 1rem 1rem 0.9rem;
}

.dashboard-stat-card__icon {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 2.75rem;
  height: 2.75rem;
  margin-top: 0.1rem;
  border: 1px solid rgb(var(--stat-accent-rgb) / 0.12);
  border-radius: 0.75rem;
  color: rgb(var(--stat-accent-rgb));
  background: linear-gradient(145deg, rgb(255 255 255 / 0.72), rgb(var(--stat-accent-rgb) / 0.14));
  box-shadow:
    0 6px 14px rgb(var(--stat-accent-rgb) / 0.12),
    inset 0 1px 0 rgb(255 255 255 / 0.72);
  transition: transform 180ms ease;
}

.dashboard-stat-card:hover .dashboard-stat-card__icon {
  transform: scale(1.035);
}

.dashboard-stat-card__copy {
  min-width: 0;
  flex: 1 1 auto;
}

.dashboard-stat-card__title {
  overflow: hidden;
  color: rgb(71 78 101);
  font-size: 0.75rem;
  font-weight: 600;
  line-height: 1.15rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-stat-card__value {
  max-width: 100%;
  overflow: hidden;
  color: rgb(17 24 39);
  font-size: clamp(1.35rem, 2vw, 1.8rem);
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.035em;
  line-height: 1.12;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-stat-card--accent-value .dashboard-stat-card__value {
  color: rgb(var(--stat-accent-rgb));
}

.dashboard-stat-card__description {
  max-width: 100%;
  margin-top: 0.25rem;
  overflow: hidden;
  color: rgb(103 112 137);
  font-size: 0.6875rem;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 1rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-stat-card--accent-description .dashboard-stat-card__description {
  color: rgb(var(--stat-accent-rgb));
  font-weight: 650;
}

.dashboard-stat-card__decoration {
  pointer-events: none;
  position: absolute;
  z-index: 1;
  right: -0.2rem;
  bottom: -0.05rem;
  width: 55%;
  height: 58%;
  color: rgb(var(--stat-accent-rgb));
  opacity: 0.82;
}

.dashboard-stat-card__wave-fill {
  fill: currentColor;
  opacity: 0.11;
}

.dashboard-stat-card__wave-line {
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-width: 1.35;
  vector-effect: non-scaling-stroke;
}

.dashboard-stat-card__bars {
  right: 0.6rem;
  bottom: 0.45rem;
  width: 43%;
  height: 52%;
  fill: currentColor;
  opacity: 0.58;
}

:global(.dark .dashboard-stat-card) {
  border-color: rgb(255 255 255 / 0.08);
  background:
    linear-gradient(118deg, rgb(var(--stat-accent-rgb) / 0.1), transparent 52%),
    linear-gradient(145deg, rgb(31 36 53 / 0.94), rgb(24 28 43 / 0.9));
  box-shadow:
    0 14px 34px rgb(0 0 0 / 0.2),
    inset 0 1px 0 rgb(255 255 255 / 0.04);
}

:global(.dark .dashboard-stat-card:hover) {
  border-color: rgb(var(--stat-accent-rgb) / 0.24);
  box-shadow:
    0 16px 38px rgb(0 0 0 / 0.26),
    inset 0 1px 0 rgb(255 255 255 / 0.06);
}

:global(.dark .dashboard-stat-card--pink) {
  border-color: rgb(255 118 182 / 0.14);
  background:
    radial-gradient(circle at 13% 0%, rgb(255 255 255 / 0.06), transparent 38%),
    linear-gradient(135deg, rgb(79 34 61 / 0.92), rgb(46 29 48 / 0.92));
}

:global(.dark .dashboard-stat-card__icon) {
  border-color: rgb(var(--stat-accent-rgb) / 0.18);
  background: linear-gradient(145deg, rgb(255 255 255 / 0.05), rgb(var(--stat-accent-rgb) / 0.16));
  box-shadow:
    0 7px 16px rgb(0 0 0 / 0.16),
    inset 0 1px 0 rgb(255 255 255 / 0.06);
}

:global(.dark .dashboard-stat-card__title),
:global(.dark .dashboard-stat-card__description) {
  color: rgb(171 180 203);
}

:global(.dark .dashboard-stat-card__value) {
  color: rgb(248 250 252);
}

:global(.dark .dashboard-stat-card--accent-value .dashboard-stat-card__value),
:global(.dark .dashboard-stat-card--accent-description .dashboard-stat-card__description) {
  color: rgb(var(--stat-accent-rgb));
}

@media (max-width: 639px) {
  .dashboard-stat-card {
    min-height: 6.9rem;
    border-radius: 1.125rem;
  }

  .dashboard-stat-card__content {
    gap: 0.75rem;
    padding: 0.9rem;
  }

  .dashboard-stat-card__icon {
    width: 2.55rem;
    height: 2.55rem;
  }

  .dashboard-stat-card__value {
    font-size: 1.4rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .dashboard-stat-card,
  .dashboard-stat-card__icon {
    transition-duration: 0.01ms;
  }

  .dashboard-stat-card:hover,
  .dashboard-stat-card:hover .dashboard-stat-card__icon {
    transform: none;
  }
}
</style>
