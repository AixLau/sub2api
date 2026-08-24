<template>
  <section
    class="user-dashboard-charts"
    :aria-label="`${t('dashboard.modelDistribution')} / ${t('dashboard.tokenUsageTrend')}`"
  >
    <img
      class="user-dashboard-charts__badge"
      src="/assets/dashboard/badge-good-job.png"
      alt=""
      aria-hidden="true"
      draggable="false"
      width="116"
      height="116"
      loading="lazy"
      decoding="async"
    />

    <div
      class="user-dashboard-charts__filters"
      data-testid="user-dashboard-filters"
    >
      <div class="user-dashboard-charts__filter-start">
        <div
          class="user-dashboard-charts__filter-control"
          role="group"
          :aria-label="t('dashboard.timeRange')"
        >
          <span class="user-dashboard-charts__filter-label">
            {{ t('dashboard.timeRange') }}:
          </span>
          <DateRangePicker
            :start-date="startDate"
            :end-date="endDate"
            @update:start-date="handleStartDateUpdate"
            @update:end-date="handleEndDateUpdate"
            @change="handleDateRangeChange"
          />
        </div>

        <button
          type="button"
          class="user-dashboard-charts__refresh"
          data-testid="user-dashboard-refresh"
          :disabled="loading"
          :aria-label="t('common.refresh')"
          @click="emit('refresh')"
        >
          <Icon
            name="refresh"
            size="sm"
            :class="{ 'user-dashboard-charts__refresh-icon--loading': loading }"
            aria-hidden="true"
          />
          <span>{{ t('common.refresh') }}</span>
        </button>
      </div>

      <div
        class="user-dashboard-charts__filter-control user-dashboard-charts__filter-end"
        role="group"
        :aria-label="t('dashboard.granularity')"
      >
        <span class="user-dashboard-charts__filter-label">
          {{ t('dashboard.granularity') }}:
        </span>
        <div class="user-dashboard-charts__granularity">
          <Select
            :model-value="granularity"
            :options="granularityOptions"
            :aria-label="t('dashboard.granularity')"
            @update:model-value="handleGranularityUpdate"
            @change="handleGranularityChange"
          />
        </div>
      </div>
    </div>

    <div class="user-dashboard-charts__analysis-grid">
      <article
        class="user-dashboard-charts__panel user-dashboard-charts__model-panel"
        data-testid="user-dashboard-model-distribution"
        :aria-labelledby="modelDistributionTitleId"
        :aria-busy="loading"
      >
        <div
          v-if="loading"
          class="user-dashboard-charts__loading"
          role="status"
          aria-live="polite"
          :aria-label="t('common.loading')"
        >
          <LoadingSpinner size="md" />
          <span class="sr-only">{{ t('common.loading') }}</span>
        </div>

        <h2
          :id="modelDistributionTitleId"
          class="user-dashboard-charts__panel-title"
        >
          {{ t('dashboard.modelDistribution') }}
        </h2>

        <div v-if="modelData" class="user-dashboard-charts__distribution">
          <div class="user-dashboard-charts__ring">
            <Doughnut
              :data="modelData"
              :options="doughnutOptions"
              role="img"
              :aria-label="modelChartSummary"
            />
            <div
              class="user-dashboard-charts__ring-center"
              data-testid="user-model-ring-center"
              aria-hidden="true"
            >
              <strong>{{ formatTokens(totalModelTokens) }}</strong>
              <span>{{ t('dashboard.tokens') }}</span>
            </div>
          </div>

          <div
            class="user-dashboard-charts__table-scroll"
            data-testid="user-dashboard-model-table"
            role="region"
            tabindex="0"
            :aria-label="t('dashboard.modelDistribution')"
          >
            <table class="user-dashboard-charts__table">
              <thead>
                <tr>
                  <th scope="col">{{ t('dashboard.model') }}</th>
                  <th scope="col">{{ t('dashboard.requests') }}</th>
                  <th scope="col">{{ t('dashboard.tokens') }}</th>
                  <th scope="col">{{ t('dashboard.actual') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(model, index) in models" :key="`${model.model}-${index}`">
                  <td>
                    <span class="user-dashboard-charts__model-name-wrap">
                      <span
                        class="user-dashboard-charts__model-dot"
                        :style="{ backgroundColor: getModelColor(index) }"
                        aria-hidden="true"
                      />
                      <span
                        class="user-dashboard-charts__model-name"
                        :title="model.model"
                      >
                        {{ model.model }}
                      </span>
                    </span>
                  </td>
                  <td>{{ formatNumber(model.requests) }}</td>
                  <td>{{ formatTokens(model.total_tokens) }}</td>
                  <td class="user-dashboard-charts__actual">
                    ${{ formatCost(model.actual_cost) }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div
          v-else
          class="user-dashboard-charts__empty"
          data-testid="user-dashboard-model-empty"
          role="status"
        >
          {{ t('dashboard.noDataAvailable') }}
        </div>
      </article>

      <TokenUsageTrend
        class="user-dashboard-charts__trend"
        :trend-data="trend"
        :loading="loading"
        :show-cost="true"
        surface="playfulDashboard"
        chart-height-class="h-48"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArcElement,
  Chart as ChartJS,
  Legend,
  Tooltip,
  type ChartData,
  type ChartOptions,
  type TooltipItem,
} from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import Icon from '@/components/icons/Icon.vue'
import { distributionRingPalette } from '@/theme/designTokens'
import type { ModelStat, TrendDataPoint } from '@/types'
import {
  formatCostFixed as formatCost,
  formatNumberLocaleString as formatNumber,
  formatTokensK as formatTokens,
} from '@/utils/format'

ChartJS.register(ArcElement, Tooltip, Legend)

type DashboardGranularity = 'day' | 'hour'
type SelectValue = string | number | boolean | null
type DateRangeChangePayload = {
  startDate: string
  endDate: string
  preset: string | null
}

interface Props {
  loading: boolean
  startDate: string
  endDate: string
  granularity: DashboardGranularity
  trend: TrendDataPoint[]
  models: ModelStat[]
}

interface Emits {
  (event: 'update:startDate', value: string): void
  (event: 'update:endDate', value: string): void
  (event: 'update:granularity', value: DashboardGranularity): void
  (event: 'dateRangeChange', value: DateRangeChangePayload): void
  (event: 'granularityChange'): void
  (event: 'refresh'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()
const { t } = useI18n()

const modelDistributionTitleId = 'user-dashboard-model-distribution-title'

const granularityOptions = computed(() => [
  { value: 'day' satisfies DashboardGranularity, label: t('dashboard.day') },
  { value: 'hour' satisfies DashboardGranularity, label: t('dashboard.hour') },
])

const modelData = computed<ChartData<'doughnut', number[], string> | null>(() => {
  if (!props.models.length) return null

  return {
    labels: props.models.map((model) => model.model),
    datasets: [{
      data: props.models.map((model) => model.total_tokens),
      backgroundColor: distributionRingPalette,
      borderWidth: 0,
      borderRadius: 8,
      spacing: 2,
      hoverOffset: 4,
    }],
  }
})

const totalModelTokens = computed(() =>
  props.models.reduce((total, model) => total + Number(model.total_tokens || 0), 0),
)

const modelChartSummary = computed(() =>
  `${t('dashboard.modelDistribution')}: ${formatTokens(totalModelTokens.value)} ${t('dashboard.tokens')}`,
)

const doughnutOptions = computed<ChartOptions<'doughnut'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  cutout: '72%',
  rotation: -90,
  layout: {
    padding: 4,
  },
  animation: {
    duration: 700,
    easing: 'easeOutQuart',
  },
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context: TooltipItem<'doughnut'>): string =>
          `${context.label}: ${formatTokens(context.parsed)} ${t('dashboard.tokens')}`,
      },
    },
  },
}))

const handleStartDateUpdate = (value: string) => {
  emit('update:startDate', value)
}

const handleEndDateUpdate = (value: string) => {
  emit('update:endDate', value)
}

const handleDateRangeChange = (value: DateRangeChangePayload) => {
  emit('dateRangeChange', value)
}

const isDashboardGranularity = (value: SelectValue): value is DashboardGranularity =>
  value === 'day' || value === 'hour'

const handleGranularityUpdate = (value: SelectValue) => {
  if (isDashboardGranularity(value)) {
    emit('update:granularity', value)
  }
}

const handleGranularityChange = () => {
  emit('granularityChange')
}

const getModelColor = (index: number): string =>
  distributionRingPalette[index % distributionRingPalette.length] ?? '#3B82F6'
</script>

<style scoped>
.user-dashboard-charts {
  position: relative;
  isolation: isolate;
  display: grid;
  width: 100%;
  min-width: 0;
  gap: 16px;
}

.user-dashboard-charts__badge {
  position: absolute;
  top: 40px;
  right: -52px;
  z-index: 5;
  width: clamp(90px, 7vw, 116px);
  height: auto;
  aspect-ratio: 1;
  object-fit: contain;
  pointer-events: none;
  user-select: none;
}

.user-dashboard-charts__filters {
  position: relative;
  z-index: 4;
  display: flex;
  min-width: 0;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 9px 14px;
  border: 1px solid rgb(var(--color-line-subtle) / 0.86);
  border-radius: 18px;
  background:
    linear-gradient(115deg, rgb(var(--color-surface-panel) / 0.94), rgb(var(--color-surface-panel) / 0.78));
  box-shadow: 0 12px 34px rgb(var(--color-shadow) / 0.07);
  backdrop-filter: blur(18px);
}

.user-dashboard-charts__filter-start,
.user-dashboard-charts__filter-control {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.user-dashboard-charts__filter-start {
  flex: 1 1 auto;
}

.user-dashboard-charts__filter-end {
  flex: 0 0 auto;
  margin-left: auto;
}

.user-dashboard-charts__filter-label {
  flex: 0 0 auto;
  color: rgb(var(--color-content-secondary));
  font-size: 13px;
  font-weight: 650;
  white-space: nowrap;
}

.user-dashboard-charts__granularity {
  width: 116px;
  flex: 0 0 116px;
}

.user-dashboard-charts__filters :deep(.date-picker-trigger),
.user-dashboard-charts__filters :deep(.select-trigger) {
  min-height: 40px;
  border-color: rgb(var(--color-line-subtle));
  border-radius: 12px;
  background: rgb(var(--color-surface-raised) / 0.74);
  box-shadow: 0 4px 12px rgb(var(--color-shadow) / 0.04);
}

.user-dashboard-charts__refresh {
  display: inline-flex;
  min-height: 40px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 8px 15px;
  border: 1px solid rgb(var(--color-line-subtle));
  border-radius: 12px;
  background: rgb(var(--color-surface-subtle) / 0.82);
  box-shadow: 0 4px 12px rgb(var(--color-shadow) / 0.04);
  color: rgb(var(--color-content-secondary));
  cursor: pointer;
  font-size: 13px;
  font-weight: 650;
  transition: border-color 160ms ease, box-shadow 160ms ease, color 160ms ease, transform 160ms ease;
}

.user-dashboard-charts__refresh:hover:not(:disabled) {
  border-color: rgb(var(--color-line-strong));
  box-shadow: 0 7px 16px rgb(var(--color-shadow) / 0.08);
  color: rgb(var(--color-content-primary));
  transform: translateY(-1px);
}

.user-dashboard-charts__refresh:focus-visible {
  outline: 2px solid rgb(var(--color-line-focus));
  outline-offset: 2px;
}

.user-dashboard-charts__refresh:disabled {
  cursor: not-allowed;
  opacity: 0.58;
}

.user-dashboard-charts__refresh-icon--loading {
  animation: user-dashboard-refresh-spin 850ms linear infinite;
}

.user-dashboard-charts__analysis-grid {
  position: relative;
  z-index: 2;
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  align-items: stretch;
  gap: 16px;
}

.user-dashboard-charts__panel {
  position: relative;
  box-sizing: border-box;
  min-width: 0;
  max-width: 100%;
  padding: 18px;
  border: 1px solid rgb(var(--color-line-subtle) / 0.86);
  border-radius: 22px;
  background:
    radial-gradient(circle at 8% 4%, rgb(255 255 255 / 0.5), transparent 34%),
    linear-gradient(145deg, rgb(var(--color-surface-panel) / 0.96), rgb(var(--color-surface-panel) / 0.79));
  box-shadow: 0 16px 38px rgb(var(--color-shadow) / 0.08);
  backdrop-filter: blur(18px);
}

.user-dashboard-charts__model-panel {
  overflow: hidden;
}

.user-dashboard-charts__panel-title {
  margin: 0 0 12px;
  color: rgb(var(--color-content-primary));
  font-size: 15px;
  font-weight: 750;
  line-height: 1.35;
}

.user-dashboard-charts__distribution {
  display: grid;
  min-width: 0;
  min-height: 192px;
  grid-template-columns: minmax(132px, 0.8fr) minmax(0, 1.2fr);
  align-items: center;
  gap: 16px;
}

.user-dashboard-charts__ring {
  position: relative;
  width: min(100%, 184px);
  min-height: 0;
  aspect-ratio: 1;
  justify-self: center;
}

.user-dashboard-charts__ring > :first-child {
  position: absolute !important;
  inset: 0;
  z-index: 1;
  width: 100% !important;
  height: 100% !important;
}

.user-dashboard-charts__ring-center {
  position: absolute;
  inset: 25%;
  z-index: 2;
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  color: rgb(var(--color-content-primary));
  pointer-events: none;
  text-align: center;
}

.user-dashboard-charts__ring-center strong {
  display: block;
  max-width: 100%;
  overflow: hidden;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: clamp(15px, 1.5vw, 21px);
  font-variant-numeric: tabular-nums;
  font-weight: 750;
  line-height: 1.1;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-dashboard-charts__ring-center span {
  margin-top: 4px;
  color: rgb(var(--color-content-tertiary));
  font-size: 10px;
  font-weight: 600;
}

.user-dashboard-charts__table-scroll {
  width: 100%;
  min-width: 0;
  max-height: 192px;
  overflow: auto;
  border-radius: 10px;
  scrollbar-gutter: stable;
}

.user-dashboard-charts__table-scroll:focus-visible {
  outline: 2px solid rgb(var(--color-line-focus));
  outline-offset: -2px;
}

.user-dashboard-charts__table {
  width: 100%;
  min-width: 326px;
  border-collapse: collapse;
  color: rgb(var(--color-content-secondary));
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}

.user-dashboard-charts__table th,
.user-dashboard-charts__table td {
  padding: 7px;
  border-bottom: 1px solid rgb(var(--color-line-subtle) / 0.72);
  text-align: right;
  white-space: nowrap;
}

.user-dashboard-charts__table th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: rgb(var(--color-surface-panel) / 0.95);
  color: rgb(var(--color-content-tertiary));
  font-size: 10px;
  font-weight: 700;
}

.user-dashboard-charts__table th:first-child,
.user-dashboard-charts__table td:first-child {
  text-align: left;
}

.user-dashboard-charts__table tbody tr:last-child td {
  border-bottom: 0;
}

.user-dashboard-charts__table tbody tr {
  transition: background-color 150ms ease;
}

.user-dashboard-charts__table tbody tr:hover {
  background: rgb(var(--color-surface-subtle) / 0.68);
}

.user-dashboard-charts__model-name-wrap {
  display: flex;
  min-width: 0;
  max-width: 132px;
  align-items: center;
  gap: 6px;
}

.user-dashboard-charts__model-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 7px;
  border-radius: 999px;
}

.user-dashboard-charts__model-name {
  min-width: 0;
  overflow: hidden;
  color: rgb(var(--color-content-primary));
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-dashboard-charts__actual {
  color: rgb(var(--color-status-success));
  font-weight: 700;
}

.user-dashboard-charts__loading {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: inherit;
  background: rgb(var(--color-surface-panel) / 0.62);
  backdrop-filter: blur(5px);
}

.user-dashboard-charts__empty {
  display: flex;
  min-height: 192px;
  align-items: center;
  justify-content: center;
  color: rgb(var(--color-content-tertiary));
  font-size: 13px;
  text-align: center;
}

.user-dashboard-charts__trend {
  min-width: 0;
  max-width: 100%;
}

@keyframes user-dashboard-refresh-spin {
  to { transform: rotate(360deg); }
}

@media (min-width: 1024px) {
  .user-dashboard-charts__analysis-grid {
    grid-template-columns: minmax(0, 0.85fr) minmax(0, 1.15fr);
  }
}

@media (max-width: 1279px) {
  .user-dashboard-charts__badge {
    top: 44px;
    right: -40px;
    width: 82px;
  }
}

@media (max-width: 1023px) {
  .user-dashboard-charts__badge {
    display: none;
  }
}

@media (max-width: 767px) {
  .user-dashboard-charts__filters {
    align-items: stretch;
    flex-direction: column;
    padding: 12px;
  }

  .user-dashboard-charts__filter-start {
    align-items: stretch;
    flex-wrap: wrap;
  }

  .user-dashboard-charts__filter-control {
    flex: 1 1 100%;
  }

  .user-dashboard-charts__filter-control > :deep(.relative) {
    min-width: 0;
    flex: 1 1 auto;
  }

  .user-dashboard-charts__filters :deep(.date-picker-trigger) {
    width: 100%;
  }

  .user-dashboard-charts__refresh {
    flex: 1 1 auto;
  }

  .user-dashboard-charts__filter-end {
    width: 100%;
    margin-left: 0;
  }

  .user-dashboard-charts__granularity {
    width: auto;
    min-width: 0;
    flex: 1 1 auto;
  }
}

@media (max-width: 639px) {
  .user-dashboard-charts__distribution {
    grid-template-columns: minmax(0, 1fr);
  }

  .user-dashboard-charts__ring {
    width: min(52vw, 178px);
  }

  .user-dashboard-charts__table-scroll {
    max-height: 184px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .user-dashboard-charts__refresh,
  .user-dashboard-charts__table tbody tr {
    transition: none;
  }

  .user-dashboard-charts__refresh-icon--loading {
    animation-duration: 1800ms;
  }
}
</style>
