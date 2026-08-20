<template>
  <section
    data-testid="openai-oauth-usage-summary"
    data-layout="toolbar-compact"
    class="usage-summary-root"
    aria-live="polite"
    :aria-busy="loading"
  >
    <button
      v-if="error && !summary"
      type="button"
      data-testid="usage-summary-error"
      class="usage-summary-shell usage-summary-error"
      @click="emit('retry')"
    >
      <span class="usage-summary-frame usage-summary-error-frame">
        <Icon name="exclamationCircle" size="sm" aria-hidden="true" />
        <span class="truncate">{{ t('admin.accounts.openaiUsageSummary.loadFailedCompact') }}</span>
        <Icon name="refresh" size="xs" aria-hidden="true" />
      </span>
    </button>

    <div
      v-else-if="!summary"
      data-testid="usage-summary-skeleton"
      class="usage-summary-shell usage-summary-skeleton"
      aria-hidden="true"
    >
      <div class="usage-summary-frame">
        <div v-for="key in ['five-hour', 'seven-day']" :key="key" class="usage-skeleton-window">
          <div class="usage-skeleton-heading">
            <Skeleton data-testid="usage-summary-skeleton-shimmer" width="42px" height="10px" />
            <Skeleton width="48px" height="18px" />
          </div>
          <Skeleton class="usage-skeleton-amount" width="118px" height="9px" />
          <Skeleton class="usage-skeleton-progress" width="100%" height="3px" />
        </div>
      </div>
    </div>

    <div v-else class="usage-summary-shell usage-summary-shell-ready">
      <div class="usage-summary-frame">
        <button
          v-for="window in windows"
          :key="window.key"
          type="button"
          :data-testid="`usage-window-${window.key}`"
          :class="['usage-window', `usage-window-${window.key}`]"
          :title="window.title"
          :aria-label="window.title"
          @click="showDetails = true"
        >
          <span class="usage-window-content">
            <span class="usage-window-heading">
              <span class="usage-window-label usage-window-label-full">{{ window.label }}</span>
              <span class="usage-window-label usage-window-label-short">{{ window.shortLabel }}</span>
              <strong class="usage-window-percent">{{ formatPercent(window.data.usage_percent) }}</strong>
            </span>
            <span class="usage-window-amount">
              <span class="usage-window-used">{{ t('admin.accounts.openaiUsageSummary.usedShort') }} {{ formatCompactCurrency(window.data.used) }}</span>
              <span class="usage-window-separator">·</span>
              <span>{{ t('admin.accounts.openaiUsageSummary.remainingShort') }} {{ formatCompactEstimate(window.data.estimated_remaining) }}</span>
            </span>
            <span class="usage-window-progress-track">
              <span
                data-testid="usage-progress"
                :class="['usage-window-progress', progressColor(window.key, window.data.usage_percent)]"
                :style="{ width: `${clampPercent(window.data.usage_percent)}%` }"
              />
            </span>
          </span>
          <span
            v-if="window.key === 'five-hour' || window.key === 'seven-day'"
            :class="['usage-toy', window.key === 'five-hour' ? 'usage-toy-clock' : 'usage-toy-calendar']"
            aria-hidden="true"
          >
            <Icon :name="window.key === 'five-hour' ? 'clock' : 'calendar'" size="sm" />
          </span>
        </button>
      </div>
    </div>

    <OpenAIOAuthUsageDetailsDialog
      :show="showDetails"
      :summary="summary"
      :error="error"
      @close="showDetails = false"
      @retry="emit('retry')"
      @view-accounts="emit('view-accounts')"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import OpenAIOAuthUsageDetailsDialog from './OpenAIOAuthUsageDetailsDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import { formatCompactCurrency, formatCurrency } from '@/utils/format'
import type { OpenAIOAuthUsageSummary } from '@/types'

const props = defineProps<{
  summary: OpenAIOAuthUsageSummary | null
  loading: boolean
  error: string | null
}>()

const emit = defineEmits<{
  (event: 'retry'): void
  (event: 'view-accounts'): void
}>()

const { t } = useI18n()
const showDetails = ref(false)

const clampPercent = (value: number) => Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0))
const formatPercent = (value: number) => `${clampPercent(value).toFixed(1)}%`
const formatCompactEstimate = (value: number | null) => value == null
  ? t('admin.accounts.openaiUsageSummary.pendingEstimateShort')
  : formatCompactCurrency(value)

const progressColor = (key: string, value: number) => {
  const percent = clampPercent(value)
  if (percent >= 95) return 'bg-red-500'
  if (percent >= 85) return 'bg-orange-500'
  if (percent >= 70) return 'bg-amber-500'
  return key === 'seven-day'
    ? 'bg-gradient-to-r from-violet-600 to-purple-500'
    : 'bg-gradient-to-r from-blue-600 to-indigo-500'
}

const windows = computed(() => {
  if (!props.summary) return []
  return [
    {
      key: 'five-hour',
      label: t('admin.accounts.openaiUsageSummary.fiveHour'),
      shortLabel: '5h',
      data: props.summary.five_hour,
      title: `${t('admin.accounts.openaiUsageSummary.fiveHour')}: ${t('admin.accounts.openaiUsageSummary.used')} ${formatCurrency(props.summary.five_hour.used)}, ${t('admin.accounts.openaiUsageSummary.remaining')} ${props.summary.five_hour.estimated_remaining == null ? t('admin.accounts.openaiUsageSummary.pendingEstimate') : formatCurrency(props.summary.five_hour.estimated_remaining)}`
    },
    {
      key: 'seven-day',
      label: t('admin.accounts.openaiUsageSummary.sevenDay'),
      shortLabel: '7d',
      data: props.summary.seven_day,
      title: `${t('admin.accounts.openaiUsageSummary.sevenDay')}: ${t('admin.accounts.openaiUsageSummary.used')} ${formatCurrency(props.summary.seven_day.used)}, ${t('admin.accounts.openaiUsageSummary.remaining')} ${props.summary.seven_day.estimated_remaining == null ? t('admin.accounts.openaiUsageSummary.pendingEstimate') : formatCurrency(props.summary.seven_day.estimated_remaining)}`
    }
  ]
})

</script>

<style scoped>
.usage-summary-root {
  display: block;
  min-width: 0;
  overflow: visible;
}

.usage-summary-shell {
  width: clamp(430px, 32vw, 520px);
  padding: 1px;
  border-radius: 15px;
  background: linear-gradient(
    90deg,
    rgb(59 130 246 / 0.28),
    rgb(139 92 246 / 0.26),
    rgb(236 72 153 / 0.2)
  );
  box-shadow:
    0 8px 24px rgb(99 102 241 / 0.08),
    0 1px 2px rgb(15 23 42 / 0.05);
}

.usage-summary-frame {
  position: relative;
  display: flex;
  align-items: stretch;
  height: 56px;
  overflow: visible;
  border-radius: 14px;
  background: rgb(255 255 255 / 0.92);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.9);
  backdrop-filter: blur(16px) saturate(145%);
  -webkit-backdrop-filter: blur(16px) saturate(145%);
}

.dark .usage-summary-shell {
  background: linear-gradient(
    90deg,
    rgb(96 165 250 / 0.32),
    rgb(167 139 250 / 0.28),
    rgb(244 114 182 / 0.24)
  );
  box-shadow:
    0 8px 24px rgb(0 0 0 / 0.2),
    0 1px 2px rgb(0 0 0 / 0.16);
}

.dark .usage-summary-frame {
  background: rgb(var(--color-navy-800) / 0.95);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.08);
}

.usage-window {
  position: relative;
  display: block;
  flex: 1 1 0%;
  min-width: 0;
  overflow: visible;
  border: 0;
  border-right: 1px solid rgb(148 163 184 / 0.16);
  background: transparent;
  color: rgb(var(--color-content-primary));
  text-align: left;
  transition:
    background-color 160ms ease,
    box-shadow 160ms ease,
    transform 160ms ease;
}

.usage-window:last-child {
  border-right: 0;
}

.usage-window:hover {
  z-index: 2;
  background: rgb(239 246 255 / 0.42);
  box-shadow: 0 5px 14px rgb(99 102 241 / 0.08);
  transform: translateY(-1px);
}

.dark .usage-window {
  border-right-color: rgb(148 163 184 / 0.18);
}

.dark .usage-window:hover {
  background: rgb(99 102 241 / 0.12);
  box-shadow: 0 5px 14px rgb(0 0 0 / 0.18);
}

.usage-window:focus-visible {
  z-index: 3;
  outline: 2px solid rgb(var(--color-line-focus));
  outline-offset: -2px;
}

.usage-window-content {
  position: relative;
  z-index: 1;
  display: flex;
  height: 100%;
  min-width: 0;
  flex-direction: column;
  justify-content: center;
  padding: 6px 42px 5px 12px;
}

.usage-window-heading {
  display: flex;
  min-width: 0;
  align-items: baseline;
  justify-content: space-between;
  gap: 6px;
  line-height: 1;
}

.usage-window-label {
  min-width: 0;
  overflow: hidden;
  color: rgb(var(--color-content-brand));
  font-size: 11px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.usage-window-seven-day .usage-window-label {
  color: rgb(124 58 237);
}

.dark .usage-window-seven-day .usage-window-label {
  color: rgb(196 181 253);
}

.usage-window-label-short {
  display: none;
}

.usage-window-percent {
  flex-shrink: 0;
  color: rgb(15 23 42);
  font-size: 19px;
  font-weight: 750;
  letter-spacing: -0.01em;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.dark .usage-window-percent {
  color: rgb(248 250 252);
}

.usage-window-amount {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 5px;
  overflow: hidden;
  margin-top: 3px;
  color: rgb(71 85 105);
  font-size: 10px;
  line-height: 1;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.dark .usage-window-amount {
  color: rgb(203 213 225);
}

.usage-window-used {
  color: rgb(100 116 139);
}

.dark .usage-window-used {
  color: rgb(148 163 184);
}

.usage-window-separator {
  color: rgb(148 163 184 / 0.7);
}

.usage-window-progress-track {
  display: block;
  width: 100%;
  height: 3px;
  margin-top: auto;
  overflow: hidden;
  border-radius: 999px;
  background: rgb(226 232 240 / 0.9);
}

.dark .usage-window-progress-track {
  background: rgb(71 85 105 / 0.72);
}

.usage-window-progress {
  display: block;
  height: 100%;
  min-width: 0;
  border-radius: inherit;
  transition: width 300ms ease, background-color 300ms ease;
}

.usage-toy {
  position: absolute;
  z-index: 0;
  right: 9px;
  bottom: 7px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 29px;
  height: 29px;
  border-radius: 10px;
  color: white;
  pointer-events: none;
  user-select: none;
  transform: rotate(-7deg);
}

.usage-toy::before,
.usage-toy::after {
  position: absolute;
  content: '';
}

.usage-toy-clock {
  background: linear-gradient(145deg, rgb(37 99 235), rgb(79 70 229));
  box-shadow:
    inset 2px 2px 0 rgb(255 255 255 / 0.42),
    inset -3px -4px 0 rgb(30 64 175 / 0.3),
    0 5px 9px rgb(37 99 235 / 0.24);
}

.usage-toy-clock::before,
.usage-toy-clock::after {
  top: -3px;
  width: 8px;
  height: 5px;
  border-radius: 5px 5px 2px 2px;
  background: rgb(29 78 216);
}

.usage-toy-clock::before {
  left: 4px;
  transform: rotate(-18deg);
}

.usage-toy-clock::after {
  right: 4px;
  transform: rotate(18deg);
}

.usage-toy-calendar {
  background: linear-gradient(145deg, rgb(167 139 250), rgb(236 72 153));
  box-shadow:
    inset 2px 2px 0 rgb(255 255 255 / 0.44),
    inset -3px -4px 0 rgb(126 34 206 / 0.24),
    0 5px 9px rgb(168 85 247 / 0.2);
  transform: rotate(6deg);
}

.usage-toy-calendar::before,
.usage-toy-calendar::after {
  top: -3px;
  width: 4px;
  height: 8px;
  border: 2px solid rgb(126 34 206);
  border-radius: 4px;
  background: rgb(255 255 255 / 0.28);
}

.usage-toy-calendar::before {
  left: 7px;
}

.usage-toy-calendar::after {
  right: 7px;
}

.usage-summary-error {
  display: block;
  border: 0;
  background: transparent;
  padding: 0;
  text-align: left;
}

.usage-summary-error-frame {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 12px;
  color: rgb(var(--color-status-danger));
  font-size: 11px;
}

.usage-summary-error:focus-visible {
  outline: 2px solid rgb(var(--color-line-focus));
  outline-offset: 2px;
}

.usage-skeleton-window {
  display: flex;
  min-width: 0;
  flex: 1 1 0%;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  border-right: 1px solid rgb(148 163 184 / 0.16);
  padding: 6px 12px 5px;
}

.usage-skeleton-window:last-child {
  border-right: 0;
}

.usage-skeleton-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.usage-skeleton-amount {
  margin-top: 1px;
}

.usage-skeleton-progress {
  margin-top: auto;
}

@media (min-width: 1280px) and (max-width: 1535px) {
  .usage-summary-shell {
    width: 320px;
  }

  .usage-summary-frame {
    height: 54px;
  }

  .usage-window-content {
    padding: 6px 28px 5px 10px;
  }

  .usage-window-label-full {
    display: none;
  }

  .usage-window-label-short {
    display: inline;
  }

  .usage-window-percent {
    font-size: 18px;
  }

  .usage-window-used,
  .usage-window-separator {
    display: none;
  }

  .usage-window-amount {
    font-size: 10px;
  }

  .usage-toy {
    right: 6px;
    bottom: 6px;
    width: 23px;
    height: 23px;
    border-radius: 8px;
  }

  .usage-skeleton-amount {
    width: 72px !important;
  }
}

@media (max-width: 1279px) {
  .usage-summary-shell {
    width: 210px;
  }

  .usage-summary-frame {
    height: 44px;
  }

  .usage-window-content {
    padding: 4px 8px;
  }

  .usage-window-label-full {
    display: none;
  }

  .usage-window-label-short {
    display: inline;
  }

  .usage-window-percent {
    font-size: 15px;
  }

  .usage-window-amount,
  .usage-toy,
  .usage-skeleton-amount {
    display: none;
  }

  .usage-window-progress-track {
    height: 2px;
  }

  .usage-skeleton-window {
    padding: 4px 8px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .usage-window,
  .usage-window-progress {
    transition: none;
  }
}
</style>
