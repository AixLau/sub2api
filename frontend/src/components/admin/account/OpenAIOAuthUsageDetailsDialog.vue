<template>
  <Teleport to="body">
    <Transition name="openai-usage-drawer" appear>
      <div
        v-if="show"
        class="openai-usage-overlay"
        data-testid="openai-oauth-usage-drawer"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="dialogId"
        :aria-busy="!summary"
        @click.self="handleClose"
      >
        <button
          type="button"
          class="openai-usage-backdrop"
          data-testid="openai-usage-drawer-backdrop"
          :aria-label="t('admin.accounts.openaiUsageSummary.drawerClose')"
          tabindex="-1"
          @click="handleClose"
        />

        <aside
          ref="drawerRef"
          class="openai-usage-drawer"
          data-testid="openai-usage-drawer-panel"
          tabindex="-1"
          @click.stop
        >
          <header class="openai-usage-header">
            <div class="min-w-0 pr-14">
              <h2 :id="dialogId" class="text-lg font-extrabold tracking-tight text-gray-950 dark:text-white">
                {{ t('admin.accounts.openaiUsageSummary.detailsTitle') }}
              </h2>
              <p v-if="summary" class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                <span class="font-bold tabular-nums text-emerald-500 dark:text-emerald-400">{{ summary.account_count }}</span>
                {{ t('admin.accounts.openaiUsageSummary.mainAccountLabel') }}
              </p>
              <div v-else class="mt-2 h-4 w-28 animate-pulse rounded bg-gray-200 dark:bg-dark-700" aria-hidden="true" />
              <p v-if="summary" class="mt-1 text-xs tabular-nums text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.openaiUsageSummary.updatedAt') }}: {{ formatDateTime(summary.generated_at) }}
              </p>
              <div v-else class="mt-1 h-3 w-44 animate-pulse rounded bg-gray-100 dark:bg-dark-800" aria-hidden="true" />
            </div>

            <div class="openai-usage-ip" aria-hidden="true">
              <span class="openai-usage-ip-star">✦</span>
              <img src="/default-avatars/ai-cloud.webp" alt="" class="h-14 w-14 object-contain drop-shadow-md" />
              <span class="openai-usage-ip-label">Y2K</span>
            </div>

            <button
              ref="closeButtonRef"
              type="button"
              data-testid="openai-usage-drawer-close"
              class="absolute right-3 top-3 inline-flex h-10 w-10 items-center justify-center rounded-xl text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-950 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/50 dark:text-gray-400 dark:hover:bg-dark-800 dark:hover:text-white"
              :aria-label="t('admin.accounts.openaiUsageSummary.drawerClose')"
              @click="handleClose"
            >
              <Icon name="x" size="md" aria-hidden="true" />
            </button>
          </header>

          <div ref="bodyRef" class="openai-usage-body" data-testid="openai-oauth-usage-details">
            <div
              v-if="error && summary"
              class="mb-3 flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50/80 px-3 py-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
              data-testid="openai-usage-inline-error"
            >
              <span class="flex min-w-0 items-center gap-2">
                <Icon name="exclamationCircle" size="sm" aria-hidden="true" />
                <span class="truncate">{{ t('admin.accounts.openaiUsageSummary.loadFailedCompact') }}</span>
              </span>
              <button type="button" class="shrink-0 font-semibold hover:underline" @click="emit('retry')">
                {{ t('admin.accounts.openaiUsageSummary.retry') }}
              </button>
            </div>

            <template v-if="summary">
              <dl class="grid grid-cols-3 gap-2" data-testid="openai-usage-account-stats">
                <div
                  v-for="stat in accountStats"
                  :key="stat.key"
                  class="flex h-[68px] flex-col justify-center rounded-xl border border-gray-200/90 bg-white/80 px-2 text-center shadow-sm dark:border-dark-700 dark:bg-dark-800/80"
                >
                  <dt class="truncate text-[10px] font-medium text-gray-500 dark:text-gray-400">{{ stat.label }}</dt>
                  <dd :class="['mt-1 text-xl font-bold tabular-nums', stat.valueClass]">{{ stat.value }}</dd>
                </div>
              </dl>

              <section class="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-2" data-testid="openai-usage-windows">
                <article
                  v-for="window in windows"
                  :key="window.key"
                  :data-testid="`usage-details-${window.key}`"
                  class="openai-usage-window-card"
                >
                  <div class="flex items-start justify-between gap-2">
                    <h3 class="text-sm font-bold text-gray-950 dark:text-white">{{ window.label }}</h3>
                    <span :class="['shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold', window.sourceClass]">
                      {{ window.sourceLabel }}
                    </span>
                  </div>

                  <div class="mt-3 flex items-end justify-between gap-3">
                    <div class="flex items-baseline gap-1.5">
                      <strong
                        data-testid="usage-details-window-percent"
                        class="text-[28px] font-extrabold leading-none tabular-nums tracking-tight text-gray-950 dark:text-white"
                      >
                        {{ formatPercent(window.data.usage_percent) }}
                      </strong>
                      <span class="text-[10px] text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.used') }}</span>
                    </div>
                    <dl class="space-y-0.5 text-right text-[10px] tabular-nums">
                      <div class="flex items-center justify-end gap-2">
                        <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.usedShort') }}</dt>
                        <dd class="font-semibold text-gray-800 dark:text-gray-200">{{ formatCurrency(window.data.used) }}</dd>
                      </div>
                      <div class="flex items-center justify-end gap-2">
                        <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openaiUsageSummary.remainingShort') }}</dt>
                        <dd class="font-semibold text-emerald-600 dark:text-emerald-400">{{ formatEstimate(window.data.estimated_remaining) }}</dd>
                      </div>
                    </dl>
                  </div>

                  <div
                    class="mt-3 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600"
                    role="progressbar"
                    :aria-label="window.label"
                    aria-valuemin="0"
                    aria-valuemax="100"
                    :aria-valuenow="clampPercent(window.data.usage_percent)"
                  >
                    <div
                      data-testid="usage-details-progress"
                      :class="['h-full rounded-full transition-[width,background-color] duration-300', progressColor(window.key, window.data.usage_percent)]"
                      :style="{ width: `${clampPercent(window.data.usage_percent)}%` }"
                    />
                  </div>

                  <dl class="mt-4 grid grid-cols-3 divide-x divide-gray-200/80 border-y border-gray-200/80 py-2.5 dark:divide-dark-700 dark:border-dark-700">
                    <div v-for="metric in window.capacityMetrics" :key="metric.key" class="min-w-0 px-2 first:pl-0 last:pr-0">
                      <dt class="truncate text-[10px] leading-4 text-gray-500 dark:text-gray-400">{{ metric.label }}</dt>
                      <dd :class="['mt-0.5 truncate text-[13px] font-semibold tabular-nums', metric.valueClass || 'text-gray-900 dark:text-white']">
                        {{ metric.value }}
                      </dd>
                    </div>
                  </dl>

                  <dl class="grid grid-cols-3 divide-x divide-gray-200/80 py-2.5 dark:divide-dark-700">
                    <div v-for="metric in window.coverageMetrics" :key="metric.key" class="min-w-0 px-2 first:pl-0 last:pr-0">
                      <dt class="truncate text-[10px] leading-4 text-gray-500 dark:text-gray-400">{{ metric.label }}</dt>
                      <dd class="mt-0.5 truncate text-[13px] font-semibold tabular-nums text-gray-900 dark:text-white">{{ metric.value }}</dd>
                    </div>
                  </dl>

                  <p class="flex min-h-[28px] items-start gap-1.5 rounded-lg bg-gray-50/80 px-2 py-1.5 text-[10px] leading-4 text-gray-500 dark:bg-dark-800/70 dark:text-gray-400">
                    <Icon name="lightbulb" size="xs" class="mt-0.5 shrink-0 text-violet-500 dark:text-violet-300" aria-hidden="true" />
                    <span>{{ window.hint }}</span>
                  </p>
                </article>
              </section>

              <button
                type="button"
                data-testid="view-openai-oauth-accounts"
                class="mt-3 flex h-[50px] w-full items-center gap-3 rounded-xl border border-blue-200/80 bg-white/80 px-3 text-left text-sm font-semibold text-gray-800 shadow-sm transition-colors hover:border-blue-400 hover:bg-blue-50/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/50 dark:border-dark-700 dark:bg-dark-800/80 dark:text-gray-100 dark:hover:border-blue-700 dark:hover:bg-blue-950/30"
                @click="handleViewAccounts"
              >
                <span class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-950/50 dark:text-blue-300">
                  <Icon name="users" size="sm" aria-hidden="true" />
                </span>
                <span class="min-w-0 flex-1 truncate">{{ t('admin.accounts.openaiUsageSummary.viewAccounts', { count: summary.account_count }) }}</span>
                <Icon name="chevronRight" size="sm" class="shrink-0 text-gray-400" aria-hidden="true" />
              </button>
            </template>

            <template v-else-if="error">
              <div class="flex min-h-32 flex-col items-center justify-center rounded-xl border border-red-200 bg-red-50/70 px-4 text-center dark:border-red-900/60 dark:bg-red-950/20">
                <Icon name="exclamationCircle" size="lg" class="text-red-500 dark:text-red-300" aria-hidden="true" />
                <p class="mt-2 text-sm font-semibold text-red-800 dark:text-red-200">{{ t('admin.accounts.openaiUsageSummary.loadFailed') }}</p>
                <button type="button" class="mt-2 text-xs font-semibold text-red-700 underline hover:no-underline dark:text-red-300" @click="emit('retry')">
                  {{ t('admin.accounts.openaiUsageSummary.retry') }}
                </button>
              </div>
            </template>

            <div v-else class="space-y-3" data-testid="openai-usage-details-skeleton" aria-hidden="true">
              <div class="grid grid-cols-3 gap-2">
                <div v-for="key in 3" :key="key" class="h-[68px] animate-pulse rounded-xl border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-800" />
              </div>
              <div class="grid grid-cols-1 gap-3 xl:grid-cols-2">
                <div v-for="key in 2" :key="key" class="h-[300px] animate-pulse rounded-xl border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-800" />
              </div>
            </div>
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatCurrency, formatDateTime } from '@/utils/format'
import type { OpenAIOAuthUsageSummary, OpenAIOAuthUsageWindowSummary } from '@/types'

const props = defineProps<{
  show: boolean
  summary: OpenAIOAuthUsageSummary | null
  error: string | null
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'retry'): void
  (event: 'view-accounts'): void
}>()

const { t } = useI18n()
const drawerRef = ref<HTMLElement | null>(null)
const bodyRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
const dialogId = `openai-usage-details-${Math.random().toString(36).slice(2, 10)}`
let previousActiveElement: HTMLElement | null = null
let scrollLockActive = false

const clampPercent = (value: number) => Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0))
const formatPercent = (value: number) => `${clampPercent(value).toFixed(1)}%`
const formatEstimate = (value: number | null) => value == null
  ? t('admin.accounts.openaiUsageSummary.pendingEstimate')
  : formatCurrency(value)

const progressColor = (key: string, value: number) => {
  const percent = clampPercent(value)
  if (percent >= 95) return 'bg-red-500'
  if (percent >= 85) return 'bg-orange-500'
  if (percent >= 70) return 'bg-amber-500'
  return key === 'seven-day' ? 'bg-violet-600' : 'bg-blue-600'
}

const sourceMeta = (data: OpenAIOAuthUsageWindowSummary) => {
  const sources = {
    current: {
      label: t('admin.accounts.openaiUsageSummary.sourceCurrent'),
      className: 'bg-cyan-50 text-cyan-700 dark:bg-cyan-950/50 dark:text-cyan-300'
    },
    historical: {
      label: t('admin.accounts.openaiUsageSummary.sourceHistorical'),
      className: 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
    },
    mixed: {
      label: t('admin.accounts.openaiUsageSummary.sourceMixed'),
      className: 'bg-violet-50 text-violet-700 dark:bg-violet-950/40 dark:text-violet-300'
    },
    unavailable: {
      label: t('admin.accounts.openaiUsageSummary.sourceUnavailable'),
      className: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
    }
  } as const
  return sources[data.reference_source]
}

const accountStats = computed(() => {
  const summary = props.summary
  if (!summary) return []
  return [
    {
      key: 'total',
      label: t('admin.accounts.openaiUsageSummary.totalAccounts'),
      value: summary.account_count,
      valueClass: 'text-blue-600 dark:text-blue-300'
    },
    {
      key: 'included',
      label: t('admin.accounts.openaiUsageSummary.includedAccounts'),
      value: summary.included_account_count,
      valueClass: 'text-emerald-500 dark:text-emerald-300'
    },
    {
      key: 'excluded',
      label: t('admin.accounts.openaiUsageSummary.excludedAccounts'),
      value: summary.excluded_account_count,
      valueClass: 'text-red-500 dark:text-red-300'
    }
  ]
})

const windows = computed(() => {
  const summary = props.summary
  if (!summary) return []

  const buildWindow = (key: 'five-hour' | 'seven-day', labelKey: string, data: OpenAIOAuthUsageWindowSummary) => {
    const source = sourceMeta(data)
    const hintParts = [
      key === 'five-hour'
        ? t('admin.accounts.openaiUsageSummary.infoHintFiveHour', { count: data.current_sample_account_count })
        : t('admin.accounts.openaiUsageSummary.infoHintSevenDay')
    ]
    if (data.reference_source === 'historical' || data.reference_source === 'mixed') {
      hintParts.push(t('admin.accounts.openaiUsageSummary.historicalHint'))
    }
    if (data.unestimated_account_count > 0) {
      hintParts.push(t('admin.accounts.openaiUsageSummary.unestimatedHint', { count: data.unestimated_account_count }))
    }
    if (data.pending_sync_account_count > 0) {
      hintParts.push(t('admin.accounts.openaiUsageSummary.pendingSyncHint', { count: data.pending_sync_account_count }))
    }
    const hint = hintParts.join(' · ')

    return {
      key,
      label: t(labelKey),
      data,
      sourceLabel: source.label,
      sourceClass: source.className,
      hint,
      capacityMetrics: [
        {
          key: 'capacity',
          label: t('admin.accounts.openaiUsageSummary.capacity'),
          value: formatEstimate(data.estimated_capacity)
        },
        {
          key: 'remaining-percent',
          label: t('admin.accounts.openaiUsageSummary.remainingPercent'),
          value: formatPercent(data.remaining_percent),
          valueClass: 'text-emerald-600 dark:text-emerald-400'
        },
        {
          key: 'reference-capacity',
          label: t('admin.accounts.openaiUsageSummary.referenceCapacity'),
          value: formatEstimate(data.reference_capacity)
        }
      ],
      coverageMetrics: [
        {
          key: 'samples',
          label: t('admin.accounts.openaiUsageSummary.currentSamples'),
          value: data.current_sample_account_count
        },
        {
          key: 'estimated',
          label: t('admin.accounts.openaiUsageSummary.estimatedCoverage'),
          value: `${data.estimated_account_count} / ${summary.included_account_count}`
        },
        {
          key: 'pending',
          label: t('admin.accounts.openaiUsageSummary.pendingAccounts'),
          value: data.pending_sync_account_count
        }
      ]
    }
  }

  return [
    buildWindow('five-hour', 'admin.accounts.openaiUsageSummary.fiveHourWindow', summary.five_hour),
    buildWindow('seven-day', 'admin.accounts.openaiUsageSummary.sevenDayWindow', summary.seven_day)
  ]
})

const handleViewAccounts = () => {
  emit('view-accounts')
  emit('close')
}

const handleClose = () => {
  emit('close')
}

const focusableSelector = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

const focusableElements = () => {
  if (!drawerRef.value) return []
  return Array.from(drawerRef.value.querySelectorAll<HTMLElement>(focusableSelector)).filter((element) => {
    const style = window.getComputedStyle(element)
    return style.display !== 'none' && style.visibility !== 'hidden'
  })
}

const handleKeydown = (event: KeyboardEvent) => {
  if (!props.show) return
  if (event.key === 'Escape') {
    event.preventDefault()
    handleClose()
    return
  }
  if (event.key !== 'Tab' || !drawerRef.value) return

  const focusable = focusableElements()
  if (focusable.length === 0) {
    event.preventDefault()
    drawerRef.value.focus()
    return
  }

  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  const active = document.activeElement
  if (event.shiftKey && (active === first || !drawerRef.value.contains(active))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

const openDrawer = async () => {
  if (scrollLockActive || typeof document === 'undefined') return
  scrollLockActive = true
  previousActiveElement = document.activeElement as HTMLElement
  document.body.classList.add('modal-open')
  await nextTick()
  bodyRef.value?.scrollTo?.({ top: 0 })
  closeButtonRef.value?.focus()
}

const closeDrawer = () => {
  if (!scrollLockActive || typeof document === 'undefined') return
  scrollLockActive = false
  document.body.classList.remove('modal-open')
  previousActiveElement?.focus?.()
  previousActiveElement = null
}

watch(() => props.show, (isOpen) => {
  if (isOpen) void openDrawer()
  else closeDrawer()
}, { immediate: true })

onMounted(() => document.addEventListener('keydown', handleKeydown))
onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
  closeDrawer()
})
</script>

<style scoped>
.openai-usage-overlay {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  overflow: hidden;
  background: rgb(15 23 42 / 0.34);
  backdrop-filter: blur(3px);
  -webkit-backdrop-filter: blur(3px);
}

.openai-usage-backdrop {
  position: absolute;
  inset: 0;
  width: 100%;
  border: 0;
  background: transparent;
  cursor: default;
}

.openai-usage-drawer {
  position: relative;
  z-index: 1;
  display: flex;
  width: min(560px, 100vw);
  height: 100vh;
  height: 100dvh;
  margin-left: auto;
  flex-direction: column;
  overflow: hidden;
  border-left: 1px solid rgb(148 163 184 / 0.24);
  background: rgb(255 255 255 / 0.96);
  box-shadow: -18px 0 48px rgb(15 23 42 / 0.14);
  backdrop-filter: blur(22px) saturate(130%);
  -webkit-backdrop-filter: blur(22px) saturate(130%);
}

.dark .openai-usage-overlay {
  background: rgb(2 6 23 / 0.58);
}

.dark .openai-usage-drawer {
  border-left-color: rgb(71 85 105 / 0.5);
  background: rgb(15 23 42 / 0.97);
  box-shadow: -18px 0 48px rgb(0 0 0 / 0.36);
}

.openai-usage-header {
  position: relative;
  min-height: 112px;
  flex: 0 0 auto;
  overflow: hidden;
  border-bottom: 1px solid rgb(226 232 240 / 0.9);
  padding: 20px 20px 16px;
  background:
    radial-gradient(circle at 88% 15%, rgb(125 211 252 / 0.2), transparent 34%),
    linear-gradient(135deg, rgb(255 255 255 / 0.96), rgb(239 246 255 / 0.76));
}

.dark .openai-usage-header {
  border-bottom-color: rgb(51 65 85 / 0.78);
  background:
    radial-gradient(circle at 88% 15%, rgb(59 130 246 / 0.2), transparent 34%),
    linear-gradient(135deg, rgb(15 23 42 / 0.98), rgb(30 41 59 / 0.9));
}

.openai-usage-ip {
  pointer-events: none;
  position: absolute;
  right: 48px;
  top: 8px;
  display: flex;
  align-items: center;
  gap: 2px;
  user-select: none;
  opacity: 0.92;
}

.openai-usage-ip-star {
  align-self: flex-start;
  color: rgb(16 185 129 / 0.8);
  font-size: 15px;
}

.openai-usage-ip-label {
  align-self: flex-start;
  margin-top: 11px;
  color: rgb(79 70 229 / 0.85);
  font-size: 11px;
  font-weight: 800;
  transform: rotate(8deg);
}

.openai-usage-body {
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 16px 20px 20px;
  scrollbar-gutter: stable;
}

.openai-usage-window-card {
  min-width: 0;
  border: 1px solid rgb(226 232 240 / 0.95);
  border-radius: 14px;
  background: rgb(255 255 255 / 0.82);
  padding: 14px;
  box-shadow: 0 6px 20px rgb(30 64 175 / 0.06), inset 0 1px 0 rgb(255 255 255 / 0.9);
}

.dark .openai-usage-window-card {
  border-color: rgb(51 65 85 / 0.9);
  background: rgb(30 41 59 / 0.76);
  box-shadow: 0 6px 20px rgb(0 0 0 / 0.18), inset 0 1px 0 rgb(255 255 255 / 0.04);
}

.openai-usage-drawer-enter-active,
.openai-usage-drawer-leave-active {
  transition: opacity 220ms ease-out;
}

.openai-usage-drawer-enter-active .openai-usage-drawer,
.openai-usage-drawer-leave-active .openai-usage-drawer {
  transition: transform 230ms ease-out;
}

.openai-usage-drawer-enter-from,
.openai-usage-drawer-leave-to {
  opacity: 0;
}

.openai-usage-drawer-enter-from .openai-usage-drawer,
.openai-usage-drawer-leave-to .openai-usage-drawer {
  transform: translateX(100%);
}

@media (min-width: 768px) and (max-width: 1279px) {
  .openai-usage-drawer {
    width: min(560px, 90vw);
  }
}

@media (max-width: 767px) {
  .openai-usage-drawer {
    width: 100%;
    border-left: 0;
  }

  .openai-usage-header,
  .openai-usage-body {
    padding-left: 16px;
    padding-right: 16px;
  }
}
</style>
