<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>

      <template v-else>
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.title') }}</h1>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="statusLoading" @click="loadStatus(false)">
              <Icon name="refresh" size="sm" :class="statusLoading ? 'animate-spin' : ''" />
              {{ t('admin.riskControl.refreshStatus') }}
            </button>
            <button type="button" class="btn btn-primary inline-flex items-center gap-2" @click="openSettings">
              <Icon name="cog" size="sm" />
              {{ t('admin.riskControl.openSettings') }}
            </button>
          </div>
        </div>

        <div
          class="rounded-lg border px-5 py-4 shadow-sm"
          :class="protectionStatusCardClass"
        >
          <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div class="flex min-w-0 gap-3">
              <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg" :class="protectionStatusIconClass">
                <Icon name="shield" size="md" />
              </div>
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 class="text-base font-semibold" :class="protectionStatusTitleClass">{{ protectionStatusTitle }}</h2>
                  <span class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium" :class="protectionStatusBadgeClass">
                    {{ protectionStatusBadge }}
                  </span>
                </div>
                <p class="mt-1 text-sm" :class="protectionStatusDescriptionClass">{{ protectionStatusDescription }}</p>
                <div v-if="protectionUnsafeReasonLabels.length > 0" class="mt-3 flex flex-wrap gap-1.5">
                  <span
                    v-for="reason in protectionUnsafeReasonLabels"
                    :key="reason"
                    class="inline-flex max-w-[240px] truncate rounded-md bg-white/70 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-dark-900/40 dark:text-gray-200"
                  >
                    {{ reason }}
                  </span>
                </div>
              </div>
            </div>
            <div class="grid grid-cols-2 gap-x-6 gap-y-3 border-t border-current/10 pt-3 text-sm md:grid-cols-5 xl:min-w-[760px] xl:border-l xl:border-t-0 xl:pl-6 xl:pt-0">
              <div class="min-w-0">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.protectionBuild') }}</p>
                <p class="mt-1 truncate font-mono font-semibold text-gray-900 dark:text-white">{{ protectionBuildCommit }}</p>
              </div>
              <div class="min-w-0">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.protectionBaseline') }}</p>
                <p class="mt-1 truncate font-semibold text-gray-900 dark:text-white">{{ protectionBaselineText }}</p>
              </div>
              <div class="min-w-0">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.protectionExternalAPI') }}</p>
                <p class="mt-1 truncate font-semibold text-gray-900 dark:text-white">{{ protectionExternalAPIText }}</p>
              </div>
              <div class="min-w-0">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.protectionRoutes') }}</p>
                <p class="mt-1 truncate font-semibold text-gray-900 dark:text-white">{{ protectionRouteCoverageText }}</p>
              </div>
              <div class="min-w-0">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.protectionPipeline') }}</p>
                <p class="mt-1 truncate font-semibold text-gray-900 dark:text-white">{{ protectionPipelineCoverageText }}</p>
              </div>
            </div>
          </div>
        </div>

        <div
          v-if="pipelineCoverageMatrixVisible"
          data-test="pipeline-operator-summary"
          class="card"
        >
          <div class="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
            <div class="flex min-w-0 gap-3">
              <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg" :class="pipelineOperatorIconClass">
                <Icon name="shield" size="md" />
              </div>
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.protectionChainTitle') }}</h2>
                  <span class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium" :class="pipelineOperatorBadgeClass">
                    {{ pipelineOperatorBadgeText }}
                  </span>
                </div>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ pipelineOperatorDescription }}</p>
              </div>
            </div>
            <button
              type="button"
              data-test="pipeline-advanced-toggle"
              class="btn btn-secondary inline-flex w-fit items-center gap-2"
              :aria-expanded="advancedPipelineDiagnosticsOpen"
              @click="advancedPipelineDiagnosticsOpen = !advancedPipelineDiagnosticsOpen"
            >
              <Icon name="document" size="sm" />
              {{ advancedPipelineDiagnosticsOpen ? t('admin.riskControl.hideAdvancedDiagnostics') : t('admin.riskControl.showAdvancedDiagnostics') }}
            </button>
          </div>

          <div class="space-y-4 p-6">
            <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
              <div
                v-for="item in pipelineOperatorSummaryItems"
                :key="item.key"
                class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30"
              >
                <div class="flex min-w-0 items-center gap-3">
                  <div class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg" :class="item.iconClass">
                    <Icon :name="item.icon" size="sm" />
                  </div>
                  <div class="min-w-0">
                    <p class="truncate text-xs font-medium text-gray-500 dark:text-gray-400">{{ item.label }}</p>
                    <p class="mt-1 truncate text-xl font-semibold leading-7 text-gray-900 dark:text-white" :class="item.valueClass">{{ item.value }}</p>
                    <p class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">{{ item.meta }}</p>
                  </div>
                </div>
              </div>
            </div>

            <div v-if="pipelineCoverageNeedsAttention || pipelineExecutionErrorCount > 0 || pipelineExecutionUnobservedCount > 0" class="space-y-2">
              <div
                v-if="pipelineCoverageNeedsAttention"
                class="rounded-lg border border-rose-100 bg-rose-50 px-4 py-3 text-sm text-rose-800 dark:border-rose-900/30 dark:bg-rose-900/10 dark:text-rose-200"
              >
                {{ t('admin.riskControl.protectionChainCoverageAction') }}
              </div>
              <div
                v-if="pipelineExecutionErrorCount > 0"
                class="rounded-lg border border-rose-100 bg-rose-50 px-4 py-3 text-sm text-rose-800 dark:border-rose-900/30 dark:bg-rose-900/10 dark:text-rose-200"
              >
                {{ t('admin.riskControl.protectionChainRuntimeErrorAction') }}
              </div>
              <div
                v-if="pipelineExecutionUnobservedCount > 0"
                class="rounded-lg border border-amber-100 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/30 dark:bg-amber-900/10 dark:text-amber-200"
              >
                {{ t('admin.riskControl.protectionChainUnobservedSummary') }}
                <span class="font-mono font-semibold">{{ formatNumber(pipelineExecutionUnobservedCount) }}</span>
              </div>
            </div>
          </div>

          <div
            v-if="advancedPipelineDiagnosticsOpen"
            data-test="pipeline-advanced-diagnostics"
            class="space-y-5 border-t border-gray-100 p-6 dark:border-dark-700"
          >
            <div class="flex flex-col gap-3 rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.advancedDiagnosticsTitle') }}</h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.advancedDiagnosticsHint') }}</p>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="inline-flex max-w-full rounded-md bg-white px-2.5 py-1 font-mono text-xs font-medium text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300">
                  {{ t('admin.riskControl.pipelineManifestVersion') }} {{ pipelineCoverageManifestVersionText }}
                </span>
                <span class="inline-flex rounded-md bg-white px-2.5 py-1 font-mono text-xs font-medium text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300">
                  {{ pipelineCoverageVersionText }}
                </span>
                <span class="inline-flex max-w-full rounded-md bg-white px-2.5 py-1 font-mono text-xs font-medium text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300">
                  {{ t('admin.riskControl.pipelineManifestHash') }} {{ pipelineCoverageManifestHashText }}
                </span>
                <span class="inline-flex rounded-md px-2.5 py-1 text-xs font-medium" :class="pipelineCoverageStatusClass">
                  {{ pipelineCoverageStatusText }}
                </span>
              </div>
            </div>

            <div class="space-y-5">
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30">
              <div class="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                <div>
                  <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.pipelineExecutionTitle') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.pipelineExecutionHint') }}</p>
                </div>
                <div class="flex flex-wrap gap-1.5">
                  <span class="inline-flex w-fit rounded-md bg-white px-2.5 py-1 font-mono text-xs font-medium text-gray-700 shadow-sm dark:bg-dark-800 dark:text-gray-200">
                    {{ formatNumber(pipelineExecutionTotalCount) }}
                  </span>
                  <span class="inline-flex w-fit rounded-md bg-white px-2.5 py-1 font-mono text-xs font-medium text-gray-700 shadow-sm dark:bg-dark-800 dark:text-gray-200">
                    {{ t('admin.riskControl.pipelineExecutionRecent') }} {{ formatNumber(pipelineExecutionRecentCount) }}
                  </span>
                  <span
                    class="inline-flex w-fit rounded-md px-2.5 py-1 font-mono text-xs font-medium shadow-sm"
                    :class="pipelineExecutionErrorCount > 0 ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : 'bg-white text-gray-700 dark:bg-dark-800 dark:text-gray-200'"
                  >
                    {{ t('admin.riskControl.pipelineExecutionErrors') }} {{ formatNumber(pipelineExecutionErrorCount) }}
                  </span>
                  <span
                    v-if="pipelineExecutionObservationCoverage"
                    class="inline-flex w-fit rounded-md px-2.5 py-1 font-mono text-xs font-medium shadow-sm"
                    :class="pipelineExecutionObservationCoverageClass"
                  >
                    {{ t('admin.riskControl.pipelineExecutionObservedStages') }} {{ pipelineExecutionObservationCoverageText }}
                  </span>
                </div>
              </div>
              <div
                v-if="pipelineExecutionUnobservedStageRows.length"
                class="mt-3 rounded-lg border border-amber-100 bg-amber-50 p-3 dark:border-amber-900/30 dark:bg-amber-900/10"
              >
                <p class="mb-2 text-xs font-semibold text-amber-800 dark:text-amber-200">
                  {{ t('admin.riskControl.pipelineExecutionUnobservedStages') }}
                </p>
                <div class="flex flex-wrap gap-1.5">
                  <span
                    v-for="stage in pipelineExecutionUnobservedStageRows"
                    :key="stage"
                    class="inline-flex max-w-full rounded-md bg-white px-2 py-1 font-mono text-xs font-medium text-amber-800 shadow-sm dark:bg-dark-800 dark:text-amber-100"
                  >
                    {{ stage }}
                  </span>
                </div>
              </div>
              <div v-if="pipelineExecutionRouteRows.length" class="mt-3">
                <p class="mb-2 text-xs font-semibold text-gray-500 dark:text-gray-400">
                  {{ t('admin.riskControl.pipelineExecutionRoutes') }}
                </p>
                <div class="flex flex-wrap gap-1.5">
                  <span
                    v-for="route in pipelineExecutionRouteRows"
                    :key="`${route.pipeline}:${route.method ?? ''}:${route.path ?? ''}:${route.handler ?? ''}:${route.protocol ?? ''}`"
                    class="inline-flex rounded-md bg-white px-2 py-1 text-xs font-medium text-gray-700 shadow-sm dark:bg-dark-800 dark:text-gray-200"
                  >
                    {{ route.pipeline }} · {{ route.method ? `${route.method} ${route.path ?? ''}` : route.handler || route.protocol || '-' }} · {{ route.protocol || route.handler || '-' }} · {{ formatNumber(route.count) }}
                    <span v-if="route.error_count > 0" class="ml-1 text-red-600 dark:text-red-300">
                      / {{ t('admin.riskControl.pipelineExecutionErrors') }} {{ formatNumber(route.error_count) }}
                    </span>
                  </span>
                </div>
              </div>
              <div v-else-if="pipelineExecutionRows.length" class="mt-3 flex flex-wrap gap-1.5">
                <span
                  v-for="execution in pipelineExecutionRows"
                  :key="`${execution.pipeline}:${execution.stage}:${execution.source}:${execution.method ?? ''}:${execution.path ?? ''}`"
                  class="inline-flex rounded-md bg-white px-2 py-1 text-xs font-medium text-gray-700 shadow-sm dark:bg-dark-800 dark:text-gray-200"
                >
                  {{ execution.pipeline }} · {{ pipelineStageLabel(execution.stage) }} · {{ execution.method ? `${execution.method} ${execution.path ?? ''}` : execution.source }} · {{ formatNumber(execution.count) }}
                  <span v-if="execution.error_count > 0" class="ml-1 text-red-600 dark:text-red-300">
                    / {{ t('admin.riskControl.pipelineExecutionErrors') }} {{ formatNumber(execution.error_count) }}
                  </span>
                </span>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
              <div
                v-for="stage in pipelineStageRows"
                :key="stage.stage"
                class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ pipelineStageLabel(stage.stage) }}</p>
                    <p class="mt-1 truncate font-mono text-xs text-gray-500 dark:text-gray-400">{{ stage.stage }}</p>
                  </div>
                  <span class="inline-flex rounded-md bg-white px-2 py-1 font-mono text-xs font-medium text-gray-700 shadow-sm dark:bg-dark-800 dark:text-gray-200">
                    {{ formatNumber(stage.covered_routes) }}/{{ formatNumber(stage.required_routes) }}
                  </span>
                </div>
                <div class="mt-3 h-1.5 overflow-hidden rounded-full bg-white dark:bg-dark-800">
                  <div class="h-full rounded-full bg-emerald-500" :style="{ width: pipelineStageCoverageWidth(stage) }"></div>
                </div>
              </div>
            </div>

            <div class="overflow-hidden rounded-lg border border-gray-100 dark:border-dark-700">
              <div class="grid grid-cols-[minmax(190px,1.2fr)_minmax(150px,0.9fr)_minmax(190px,1fr)_minmax(180px,1.1fr)] gap-3 bg-gray-50 px-4 py-2 text-xs font-medium text-gray-500 dark:bg-dark-900/50 dark:text-gray-400">
                <span>{{ t('admin.riskControl.pipelineRoute') }}</span>
                <span>{{ t('admin.riskControl.pipelineProtocol') }}</span>
                <span>{{ t('admin.riskControl.pipelineHandler') }}</span>
                <span>{{ t('admin.riskControl.pipelineStages') }}</span>
              </div>
              <div class="max-h-[360px] divide-y divide-gray-100 overflow-y-auto dark:divide-dark-700">
                <div
                  v-for="route in pipelineRouteRows"
                  :key="`${route.method} ${route.path} ${route.protocol}`"
                  class="grid grid-cols-1 gap-3 px-4 py-3 text-sm lg:grid-cols-[minmax(190px,1.2fr)_minmax(150px,0.9fr)_minmax(190px,1fr)_minmax(180px,1.1fr)] lg:items-center"
                >
                  <div class="min-w-0">
                    <p class="truncate font-mono font-semibold text-gray-900 dark:text-white">{{ route.method }} {{ route.path }}</p>
                    <p class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">{{ route.pipeline || '-' }}</p>
                  </div>
                  <p class="min-w-0 truncate font-mono text-xs text-gray-600 dark:text-gray-300">{{ route.protocol || '-' }}</p>
                  <div class="min-w-0">
                    <p class="truncate font-mono text-xs text-gray-600 dark:text-gray-300">{{ route.handler || '-' }}</p>
                    <p v-if="formatRouteForwardAdapters(route)" class="mt-1 truncate font-mono text-[11px] text-gray-500 dark:text-gray-400">
                      {{ formatRouteForwardAdapters(route) }}
                    </p>
                  </div>
                  <div class="flex flex-wrap gap-1.5">
                    <span
                      v-for="stage in route.stages"
                      :key="stage.stage"
                      class="inline-flex rounded-md px-2 py-1 font-mono text-xs font-medium"
                      :class="pipelineRouteStageClass(stage.covered)"
                    >
                      {{ stage.stage }}
                    </span>
                    <span
                      v-if="route.uncovered_stages?.length"
                      class="inline-flex rounded-md bg-rose-50 px-2 py-1 font-mono text-xs font-medium text-rose-700 dark:bg-rose-900/20 dark:text-rose-200"
                    >
                      {{ route.uncovered_stages.join(', ') }}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>

        <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
          <div
            v-for="item in overviewItems"
            :key="item.key"
            class="rounded-lg border border-gray-100 bg-white px-4 py-3 shadow-sm dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="flex min-w-0 items-center gap-3">
              <div class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg" :class="item.iconClass">
                <Icon :name="item.icon" size="sm" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 items-center justify-between gap-2">
                  <p class="truncate text-xs font-medium text-gray-500 dark:text-gray-400">{{ item.label }}</p>
                  <span
                    v-if="item.badge"
                    class="inline-flex flex-shrink-0 items-center rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="item.badgeClass"
                  >
                    {{ item.badge }}
                  </span>
                </div>
                <div class="mt-1 flex min-w-0 items-baseline gap-2">
                  <p class="truncate text-xl font-semibold leading-7 text-gray-900 dark:text-white">{{ item.value }}</p>
                  <p v-if="item.meta" class="truncate text-xs text-gray-500 dark:text-gray-400">{{ item.meta }}</p>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div
          v-if="showPreBlockRuntimeCard"
          data-test="pre-block-runtime-cards"
          class="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,520px)_minmax(0,1fr)]"
        >
          <div data-test="pre-block-sync-card" class="card">
            <div class="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.preBlockSyncStatus') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.preBlockSyncHint') }}</p>
              </div>
              <span class="inline-flex w-fit items-center rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                {{ modeLabel(status?.mode ?? configForm.mode) }}
              </span>
            </div>

            <div class="p-6">
              <div data-test="pre-block-metric-grid" class="grid grid-cols-2 gap-3 md:grid-cols-3">
                <div
                  v-for="item in preBlockMetricItems"
                  :key="item.key"
                  class="rounded-lg p-4"
                  :class="item.class"
                >
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ item.label }}</p>
                  <p class="mt-2 truncate text-2xl font-semibold leading-8" :class="item.valueClass">{{ item.value }}</p>
                  <p v-if="item.meta" class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">{{ item.meta }}</p>
                </div>
              </div>
            </div>
          </div>

          <div data-test="pre-block-api-key-load-card" class="card">
            <div class="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.preBlockAPIKeyLoad') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                  {{ t('admin.riskControl.preBlockAPIKeyLoadHint') }}
                </p>
              </div>
              <span class="inline-flex w-fit items-center rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                {{ preBlockAPIKeyLoadSummaryText }}
              </span>
            </div>

            <div class="p-6">
              <div
                v-if="preBlockAPIKeyLoads.length > 0"
                data-test="pre-block-api-key-load-list"
                class="max-h-[280px] space-y-3 overflow-y-auto pr-1"
              >
                <div
                  v-for="item in preBlockAPIKeyLoads"
                  :key="item.key_hash || item.index"
                  class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700/50"
                >
                  <div class="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                    <div class="min-w-0">
                      <div class="flex min-w-0 items-center gap-2">
                        <span class="font-mono text-sm font-semibold text-gray-900 dark:text-white">#{{ item.index + 1 }}</span>
                        <span class="truncate font-mono text-sm text-gray-700 dark:text-gray-200">{{ item.masked || '-' }}</span>
                        <span class="h-2 w-2 flex-shrink-0 rounded-full" :class="apiKeyStatusDotClass(item.status)"></span>
                      </div>
                      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                        {{ t('admin.riskControl.preBlockAPIKeyTotals', { total: formatNumber(item.total), success: formatNumber(item.success), errors: formatNumber(item.errors) }) }}
                      </p>
                    </div>
                    <div class="grid grid-cols-4 gap-2 text-right text-xs text-gray-500 dark:text-gray-400 sm:min-w-[280px]">
                      <div>
                        <p>{{ t('admin.riskControl.preBlockKeyActiveShort') }}</p>
                        <p class="mt-1 text-sm font-semibold text-sky-700 dark:text-sky-300">{{ formatNumber(item.active) }}</p>
                      </div>
                      <div>
                        <p>{{ t('admin.riskControl.preBlockKeyTotalShort') }}</p>
                        <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatNumber(item.total) }}</p>
                      </div>
                      <div>
                        <p>{{ t('admin.riskControl.preBlockKeyAvgShort') }}</p>
                        <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatNumber(item.avg_latency_ms) }} ms</p>
                      </div>
                      <div>
                        <p>{{ t('admin.riskControl.preBlockKeyLastShort') }}</p>
                        <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ formatNumber(item.last_latency_ms) }} ms</p>
                      </div>
                    </div>
                  </div>
                  <div class="mt-3 h-1.5 overflow-hidden rounded-full bg-white dark:bg-dark-900">
                    <div class="h-full rounded-full bg-sky-500" :style="{ width: preBlockAPIKeyLoadWidth(item.total) }"></div>
                  </div>
                </div>
              </div>
              <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-700/50 dark:text-gray-400">
                {{ t('admin.riskControl.preBlockAPIKeyLoadEmpty') }}
              </p>
            </div>
          </div>
        </div>

        <div v-if="showWorkerRuntimeCard" class="card">
          <div class="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.workerStatus') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.workerStatusHint') }}</p>
            </div>
            <div class="flex flex-wrap items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
              <span>{{ t('admin.riskControl.autoRefresh') }}</span>
              <span v-if="status?.last_cleanup_at">
                {{ t('admin.riskControl.lastCleanup', { time: formatDateTime(status.last_cleanup_at) }) }}
              </span>
            </div>
          </div>

          <div class="grid grid-cols-1 gap-6 p-6 xl:grid-cols-[minmax(0,360px)_1fr]">
            <div class="space-y-4">
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.queueUsage') }}</p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{ formatNumber(status?.queue_length ?? 0) }} / {{ formatNumber(status?.queue_size ?? configForm.queue_size) }}
                    </p>
                  </div>
                  <span class="text-sm font-semibold text-gray-900 dark:text-white">{{ queueUsagePercent }}</span>
                </div>
                <div class="mt-4 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                  <div class="h-full rounded-full bg-primary-500 transition-all duration-300" :style="queueUsageStyle"></div>
                </div>
              </div>

              <div class="grid grid-cols-2 gap-3">
                <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/50">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.activeWorkers') }}</p>
                  <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ status?.active_workers ?? 0 }}</p>
                </div>
                <div class="rounded-lg bg-emerald-50 p-4 dark:bg-emerald-900/10">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.idleWorkers') }}</p>
                  <p class="mt-2 text-2xl font-semibold text-emerald-700 dark:text-emerald-300">{{ status?.idle_workers ?? configForm.worker_count }}</p>
                </div>
                <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/50">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.processed') }}</p>
                  <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ formatNumber(status?.processed ?? 0) }}</p>
                </div>
                <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/50">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.droppedErrors') }}</p>
                  <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ formatNumber((status?.dropped ?? 0) + (status?.errors ?? 0)) }}</p>
                </div>
              </div>
            </div>

            <div>
              <div class="mb-3 flex items-center justify-between gap-3">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.workerPool') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    {{ t('admin.riskControl.workerPoolMeta', { active: status?.active_workers ?? 0, idle: status?.idle_workers ?? configForm.worker_count, total: status?.worker_count ?? configForm.worker_count }) }}
                  </p>
                </div>
                <span class="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                  {{ modeLabel(status?.mode ?? configForm.mode) }}
                </span>
              </div>
              <div class="grid grid-cols-2 gap-2 sm:grid-cols-4 md:grid-cols-6 xl:grid-cols-8 2xl:grid-cols-10">
                <div
                  v-for="worker in workerSlots"
                  :key="worker.id"
                  class="flex h-12 items-center justify-between rounded-lg border px-3 transition-colors"
                  :class="workerSlotClass(worker.state)"
                  :title="worker.label"
                >
                  <span class="text-sm font-semibold">#{{ worker.id }}</span>
                  <span class="h-2.5 w-2.5 rounded-full" :class="workerDotClass(worker.state)"></span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="card">
          <div class="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.records') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.recordsHint') }}</p>
              </div>
              <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="logsLoading" @click="loadLogs">
                <Icon name="refresh" size="sm" :class="logsLoading ? 'animate-spin' : ''" />
                {{ t('admin.riskControl.refresh') }}
              </button>
            </div>

            <div class="flex flex-col gap-2 rounded-lg border border-gray-100 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-900/30 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex min-w-0 items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
                <Icon name="filter" size="sm" class="flex-shrink-0 text-gray-400" />
                <span class="font-medium">{{ t('admin.riskControl.modelFilter') }}</span>
                <span class="truncate text-gray-500 dark:text-gray-400">{{ modelFilterSummary }}</span>
              </div>
              <div v-if="modelFilterPreviewModels.length > 0" class="flex flex-wrap gap-1.5">
                <span
                  v-for="model in modelFilterPreviewModels"
                  :key="model"
                  class="inline-flex max-w-[180px] items-center truncate rounded-md bg-white px-2 py-1 font-mono text-xs text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300"
                >
                  {{ model }}
                </span>
                <span v-if="hiddenModelFilterModelCount > 0" class="inline-flex rounded-md bg-white px-2 py-1 text-xs text-gray-500 shadow-sm dark:bg-dark-800 dark:text-gray-400">
                  +{{ hiddenModelFilterModelCount }}
                </span>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-7">
              <Select v-model="filters.result" :options="resultOptions" @change="reloadLogsFromFirstPage" />
              <Select v-model="filters.review_status" :options="reviewStatusOptions" @change="reloadLogsFromFirstPage" />
              <Select v-model="filters.group_id" :options="groupFilterOptions" @change="reloadLogsFromFirstPage" />
              <Select v-model="filters.endpoint" :options="endpointOptions" @change="reloadLogsFromFirstPage" />
              <input v-model.trim="filters.search" type="search" class="input" :placeholder="t('admin.riskControl.filters.search')" @keyup.enter="reloadLogsFromFirstPage" />
              <input v-model="filters.from" type="datetime-local" class="input" :title="t('admin.riskControl.filters.from')" @change="reloadLogsFromFirstPage" />
              <input v-model="filters.to" type="datetime-local" class="input" :title="t('admin.riskControl.filters.to')" @change="reloadLogsFromFirstPage" />
            </div>
          </div>

          <div class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
              <thead class="bg-gray-50 dark:bg-dark-800">
                <tr>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.time') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.group') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.user') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.apiKey') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.endpoint') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.result') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.highest') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.actionMeta') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.latency') }}</th>
                  <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.input') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-800">
                <tr v-if="logsLoading">
                  <td colspan="10" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</td>
                </tr>
                <tr v-else-if="logs.length === 0">
                  <td colspan="10" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.emptyLogs') }}</td>
                </tr>
                <template v-else>
                  <tr v-for="row in logs" :key="row.id" class="hover:bg-gray-50 dark:hover:bg-dark-700/60">
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(row.created_at) }}</td>
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">{{ row.group_name || '-' }}</td>
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
                      <div>{{ row.user_email || '-' }}</div>
                      <div v-if="row.user_id" class="text-xs text-gray-400">UID {{ row.user_id }}</div>
                    </td>
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">{{ row.api_key_name || '-' }}</td>
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
                      <div>{{ row.endpoint || '-' }}</div>
                      <div class="text-xs text-gray-400">{{ row.provider || '-' }} / {{ row.model || '-' }}</div>
                    </td>
                    <td class="whitespace-nowrap px-5 py-4">
                      <span class="inline-flex rounded-md px-2 py-1 text-xs font-medium" :class="resultBadgeClass(row)">
                        {{ resultLabel(row) }}
                      </span>
                    </td>
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
                      <div>{{ row.highest_category || '-' }}</div>
                      <div class="text-xs text-gray-400">{{ percent(row.highest_score) }}</div>
                      <div v-if="row.matched_keyword" class="mt-0.5 text-xs font-medium text-red-600 dark:text-red-300" :title="t('admin.riskControl.matchedKeyword') + ': ' + row.matched_keyword">
                        {{ t('admin.riskControl.matchedKeyword') }}: {{ row.matched_keyword }}
                      </div>
                    </td>
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
                      <div>{{ violationCountText(row) }}</div>
                      <div class="text-xs text-gray-400">
                        {{ row.email_sent ? t('admin.riskControl.emailSent') : t('admin.riskControl.emailNotSent') }}
                        <span v-if="row.auto_banned"> / {{ t('admin.riskControl.autoBanned') }}</span>
                      </div>
                      <div v-if="row.matched_keyword" class="mt-2 max-w-[220px] space-y-1 rounded-md bg-amber-50 px-2 py-1.5 text-xs leading-5 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                        <div class="truncate font-medium">{{ t('admin.riskControl.matchedKeyword') }}: {{ row.matched_keyword }}</div>
                        <div class="truncate text-amber-600/80 dark:text-amber-200/80">
                          {{ row.keyword_category || '-' }} / {{ row.keyword_severity || '-' }}
                        </div>
                        <div class="truncate text-amber-600/80 dark:text-amber-200/80">
                          {{ t('admin.riskControl.keywordAction') }}: {{ keywordActionText(row) }}
                        </div>
                        <div v-if="row.risk_context_type" class="truncate text-amber-600/80 dark:text-amber-200/80">
                          {{ t('admin.riskControl.riskContext') }}: {{ riskContextLabel(row.risk_context_type) }}
                        </div>
                        <div v-if="row.review_status" class="truncate text-amber-600/80 dark:text-amber-200/80">
                          {{ t('admin.riskControl.reviewStatusLabel') }}: {{ reviewStatusLabel(row.review_status) }}
                        </div>
                      </div>
                      <div v-if="row.action === 'keyword_review'" class="mt-2 flex flex-wrap gap-1.5">
                        <button
                          type="button"
                          class="inline-flex items-center gap-1 rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700 transition-colors hover:bg-emerald-100 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300"
                          :disabled="reviewingLogID === row.id"
                          @click="reviewLog(row, 'false_positive')"
                        >
                          <Icon name="checkCircle" size="xs" :class="reviewingLogID === row.id ? 'animate-spin' : ''" />
                          {{ t('admin.riskControl.markFalsePositive') }}
                        </button>
                        <button
                          type="button"
                          class="inline-flex items-center gap-1 rounded-md border border-rose-200 bg-rose-50 px-2 py-1 text-xs font-medium text-rose-700 transition-colors hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-60 dark:border-rose-900/60 dark:bg-rose-900/20 dark:text-rose-300"
                          :disabled="reviewingLogID === row.id"
                          @click="reviewLog(row, 'confirmed_violation')"
                        >
                          <Icon name="exclamationTriangle" size="xs" />
                          {{ t('admin.riskControl.markConfirmedViolation') }}
                        </button>
                      </div>
                      <button
                        v-if="canUnbanRow(row)"
                        type="button"
                        class="mt-2 inline-flex items-center gap-1 rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700 transition-colors hover:bg-emerald-100 disabled:cursor-not-allowed disabled:opacity-60 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300 dark:hover:bg-emerald-900/30"
                        :disabled="unbanningUserID === row.user_id"
                        @click="unbanUser(row)"
                      >
                        <Icon name="checkCircle" size="xs" :class="unbanningUserID === row.user_id ? 'animate-spin' : ''" />
                        {{ unbanningUserID === row.user_id ? t('common.processing') : t('admin.riskControl.unbanUser') }}
                      </button>
                    </td>
                    <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
                      <div>{{ latencyText(row.upstream_latency_ms) }}</div>
                      <div v-if="row.queue_delay_ms !== null && row.queue_delay_ms !== undefined" class="text-xs text-gray-400">
                        {{ t('admin.riskControl.queueDelay', { ms: row.queue_delay_ms }) }}
                      </div>
                    </td>
                    <td class="w-[320px] max-w-sm px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
                      <button
                        type="button"
                        class="group flex w-full min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-gray-100 dark:hover:bg-dark-700"
                        :title="inputSummaryText(row)"
                        @click="openInputDetail(row)"
                      >
                        <span class="min-w-0 flex-1 truncate">{{ inputSummaryText(row) }}</span>
                        <Icon name="eye" size="xs" class="flex-shrink-0 text-gray-300 transition-colors group-hover:text-primary-500 dark:text-gray-500" />
                      </button>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>

          <Pagination
            v-if="pagination.total > 0"
            :page="pagination.page"
            :total="pagination.total"
            :page-size="pagination.page_size"
            @update:page="onPageChange"
            @update:pageSize="onPageSizeChange"
          />
        </div>
      </template>

      <BaseDialog :show="settingsOpen" :title="t('admin.riskControl.settingsTitle')" width="extra-wide" @close="settingsOpen = false">
        <div class="space-y-6">
          <div class="flex gap-2 overflow-x-auto border-b border-gray-100 pb-3 dark:border-dark-700">
            <button
              v-for="tab in settingsTabs"
              :key="tab.id"
              type="button"
              class="inline-flex whitespace-nowrap rounded-lg px-3 py-2 text-sm font-medium transition-colors"
              :class="activeSettingsTab === tab.id ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-500 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white'"
              @click="activeSettingsTab = tab.id"
            >
              {{ tab.label }}
            </button>
          </div>

          <div v-if="activeSettingsTab === 'basic'" class="space-y-5">
            <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
              <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.enabled') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.enabledHint') }}</p>
                </div>
                <Toggle v-model="configForm.enabled" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.mode') }}</label>
                <Select v-model="configForm.mode" :options="modeOptions" />
                <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ modeDescription(configForm.mode) }}</p>
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.baseUrl') }}</label>
                <input v-model.trim="configForm.base_url" type="url" class="input" placeholder="https://api.openai.com" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.model') }}</label>
                <input v-model.trim="configForm.model" type="text" class="input" placeholder="omni-moderation-latest" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.timeoutMs') }}</label>
                <input v-model.number="configForm.timeout_ms" type="number" min="500" max="30000" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.retryCount') }}</label>
                <input v-model.number="configForm.retry_count" type="number" min="0" max="5" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.sampleRate') }}</label>
                <div class="relative">
                  <input v-model.number="configForm.sample_rate" type="number" min="0" max="100" step="1" class="input pr-8" />
                  <span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-gray-400">%</span>
                </div>
              </div>
            </div>

            <div class="overflow-hidden rounded-xl border border-gray-100 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
              <div class="flex flex-col gap-4 border-b border-gray-100 bg-gray-50 px-4 py-4 dark:border-dark-700 dark:bg-dark-800/60 lg:flex-row lg:items-center lg:justify-between">
                <div class="flex items-start gap-3">
                  <span class="mt-0.5 flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300">
                    <Icon name="key" size="md" />
                  </span>
                  <div>
                    <label class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.apiKeys') }}</label>
                    <p class="mt-1 max-w-3xl text-xs leading-5 text-gray-500 dark:text-gray-400">
                      {{ t('admin.riskControl.apiKeysHint', { count: configForm.api_key_count }) }}
                    </p>
                  </div>
                </div>
                <div class="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    class="btn btn-secondary inline-flex items-center gap-2"
                    :disabled="apiKeyTesting || inputApiKeyCount === 0 || configForm.clear_api_key"
                    @click="testApiKeys(true)"
                  >
                    <Icon name="beaker" size="sm" :class="apiKeyTesting ? 'animate-pulse' : ''" />
                    {{ apiKeyTesting ? t('admin.riskControl.testingApiKeys') : t('admin.riskControl.testInputApiKeys') }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-secondary inline-flex items-center gap-2"
                    :disabled="apiKeyTesting || effectiveStoredApiKeyCount === 0 || pendingDeletedApiKeyCount > 0 || configForm.clear_api_key || configForm.api_keys_mode === 'replace'"
                    @click="testApiKeys(false)"
                  >
                    <Icon name="shield" size="sm" />
                    {{ storedApiKeyTestButtonText }}
                  </button>
                  <button
                    v-if="configForm.api_key_configured"
                    type="button"
                    class="btn btn-secondary inline-flex items-center gap-2"
                    @click="toggleClearApiKey"
                  >
                    <Icon :name="configForm.clear_api_key ? 'x' : 'trash'" size="sm" />
                    {{ configForm.clear_api_key ? t('admin.riskControl.keepApiKey') : t('admin.riskControl.clearApiKey') }}
                  </button>
                </div>
              </div>

              <div class="grid grid-cols-1 gap-4 p-4 xl:grid-cols-[minmax(0,1fr)_minmax(360px,440px)]">
                <div class="space-y-3">
                  <div class="flex flex-col gap-2 rounded-lg border border-gray-100 bg-gray-50 p-2 dark:border-dark-700 dark:bg-dark-900/30 sm:flex-row sm:items-center sm:justify-between">
                    <div class="text-xs leading-5 text-gray-500 dark:text-gray-400">
                      <span class="font-medium text-gray-700 dark:text-gray-200">{{ t('admin.riskControl.apiKeysWriteMode') }}</span>
                      <span class="ml-2">{{ apiKeysModeHint }}</span>
                    </div>
                    <div class="inline-flex rounded-lg bg-white p-1 shadow-sm dark:bg-dark-800">
                      <button
                        type="button"
                        class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
                        :class="configForm.api_keys_mode === 'append' ? 'bg-primary-500 text-white shadow-sm' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
                        :disabled="configForm.clear_api_key"
                        @click="setAPIKeysMode('append')"
                      >
                        {{ t('admin.riskControl.apiKeysModeAppend') }}
                      </button>
                      <button
                        type="button"
                        class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
                        :class="configForm.api_keys_mode === 'replace' ? 'bg-amber-500 text-white shadow-sm' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
                        :disabled="configForm.clear_api_key"
                        @click="setAPIKeysMode('replace')"
                      >
                        {{ t('admin.riskControl.apiKeysModeReplace') }}
                      </button>
                    </div>
                  </div>
                  <textarea
                    v-model="configForm.api_keys_text"
                    class="input min-h-44 resize-y font-mono text-sm"
                    :placeholder="apiKeysPlaceholder"
                    autocomplete="new-password"
                    :disabled="configForm.clear_api_key"
                  ></textarea>
                  <div class="flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                    <span class="inline-flex rounded-md bg-gray-100 px-2 py-1 dark:bg-dark-700">
                      {{ t('admin.riskControl.inputApiKeyCount', { count: inputApiKeyCount }) }}
                    </span>
                    <span v-if="configForm.api_key_configured" class="inline-flex rounded-md bg-gray-100 px-2 py-1 dark:bg-dark-700">
                      {{ t('admin.riskControl.storedApiKeyCount', { count: configForm.api_key_count }) }}
                    </span>
                    <span v-if="configForm.clear_api_key" class="inline-flex rounded-md bg-red-50 px-2 py-1 text-red-700 dark:bg-red-900/20 dark:text-red-300">
                      {{ t('admin.riskControl.apiKeyWillClear') }}
                    </span>
                    <span v-else-if="pendingDeletedApiKeyCount > 0" class="inline-flex rounded-md bg-amber-50 px-2 py-1 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                      {{ t('admin.riskControl.apiKeyPendingDeleteCount', { count: pendingDeletedApiKeyCount }) }}
                    </span>
                    <span v-if="configForm.api_keys_mode === 'replace'" class="inline-flex rounded-md bg-amber-50 px-2 py-1 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                      {{ t('admin.riskControl.apiKeysReplaceWarning') }}
                    </span>
                  </div>

                  <div class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900/30" @paste="handleModerationImagePaste">
                    <div class="mb-3 flex items-center justify-between gap-3">
                      <div>
                        <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.auditTestInput') }}</p>
                        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.auditTestInputHint') }}</p>
                      </div>
                      <button
                        v-if="moderationTestPrompt || moderationTestImages.length > 0 || moderationTestResult"
                        type="button"
                        class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-gray-500 hover:bg-white hover:text-gray-900 dark:text-gray-400 dark:hover:bg-dark-800 dark:hover:text-white"
                        @click="clearModerationTestInput"
                      >
                        <Icon name="x" size="xs" />
                        {{ t('admin.riskControl.clearAuditTest') }}
                      </button>
                    </div>
                    <textarea
                      v-model="moderationTestPrompt"
                      class="input min-h-24 resize-y text-sm"
                      :placeholder="t('admin.riskControl.auditTestPromptPlaceholder')"
                    ></textarea>
                    <div
                      class="mt-3 rounded-lg border border-dashed border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800"
                      @dragover.prevent
                      @drop.prevent="handleModerationImageDrop"
                    >
                      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                        <div class="flex items-start gap-2">
                          <Icon name="upload" size="md" class="mt-0.5 text-gray-400" />
                          <div>
                            <p class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('admin.riskControl.auditTestImages') }}</p>
                            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.auditTestImagesHint') }}</p>
                          </div>
                        </div>
                        <label class="btn btn-secondary inline-flex cursor-pointer items-center gap-2">
                          <Icon name="plus" size="sm" />
                          {{ t('admin.riskControl.addAuditTestImage') }}
                          <input type="file" accept="image/*" multiple class="sr-only" @change="handleModerationImageUpload" />
                        </label>
                      </div>
                      <div v-if="moderationTestImages.length > 0" class="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                        <div
                          v-for="(image, index) in moderationTestImages"
                          :key="image.slice(0, 64) + index"
                          class="group relative aspect-square overflow-hidden rounded-lg border border-gray-100 bg-gray-100 dark:border-dark-700 dark:bg-dark-700"
                        >
                          <img :src="image" alt="" class="h-full w-full object-cover" />
                          <button
                            type="button"
                            class="absolute right-1.5 top-1.5 flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white opacity-0 transition-opacity group-hover:opacity-100"
                            @click="removeModerationTestImage(index)"
                          >
                            <Icon name="x" size="xs" :stroke-width="2" />
                          </button>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>

                <div class="rounded-lg border border-gray-100 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900/30">
                  <div class="mb-3 flex items-start justify-between gap-3">
                    <div class="min-w-0">
                      <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.apiKeyHealth') }}</p>
                      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.apiKeyFreezeRule') }}</p>
                    </div>
                    <span class="inline-flex shrink-0 items-center whitespace-nowrap rounded-full bg-white px-2 py-0.5 text-[11px] font-medium leading-5 text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300">
                      {{ t('admin.riskControl.apiKeyRows', { count: apiKeyRows.length }) }}
                    </span>
                  </div>

                  <div v-if="apiKeyRows.length === 0" class="flex min-h-32 flex-col items-center justify-center rounded-lg border border-dashed border-gray-200 bg-white px-4 py-6 text-center dark:border-dark-700 dark:bg-dark-800">
                    <Icon name="infoCircle" size="lg" class="text-gray-300 dark:text-dark-500" />
                    <p class="mt-2 text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.riskControl.apiKeyHealthEmpty') }}</p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.apiKeyHealthEmptyHint') }}</p>
                  </div>
                  <div v-else class="space-y-2">
                    <div class="space-y-2" :class="apiKeyRowsExpanded ? 'max-h-72 overflow-y-auto pr-1' : ''">
                      <div
                        v-for="(row, index) in visibleApiKeyRows"
                        :key="apiKeyRowKey(row, index)"
                        class="rounded-lg border bg-white p-2.5 shadow-sm dark:bg-dark-800"
                        :class="isStoredApiKeyPendingDelete(row) ? 'border-amber-200 opacity-70 dark:border-amber-800/60' : 'border-gray-100 dark:border-dark-700'"
                      >
                        <div class="flex items-start justify-between gap-2">
                          <div class="min-w-0">
                            <div class="flex min-w-0 flex-wrap items-center gap-2">
                              <span class="truncate font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ row.masked || '-' }}</span>
                              <span
                                class="inline-flex rounded-md px-1.5 py-0.5 text-[11px] font-medium"
                                :class="row.configured ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'bg-purple-50 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300'"
                              >
                                {{ isStoredApiKeyPendingDelete(row) ? t('admin.riskControl.apiKeyPendingDelete') : row.configured ? t('admin.riskControl.apiKeyConfigured') : t('admin.riskControl.apiKeyTemporary') }}
                              </span>
                            </div>
                            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ apiKeyStatusMeta(row) }}</p>
                          </div>
                          <div class="flex flex-shrink-0 items-center gap-1.5">
                            <span class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium" :class="apiKeyStatusBadgeClass(row.status)">
                              <span class="h-1.5 w-1.5 rounded-full" :class="apiKeyStatusDotClass(row.status)"></span>
                              {{ apiKeyStatusLabel(row.status) }}
                            </span>
                            <button
                              v-if="row.configured && !configForm.clear_api_key"
                              type="button"
                              class="inline-flex h-7 w-7 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-gray-200"
                              :title="isStoredApiKeyPendingDelete(row) ? t('admin.riskControl.undoDeleteApiKey') : t('admin.riskControl.deleteApiKey')"
                              @click="toggleDeleteStoredApiKey(row)"
                            >
                              <Icon :name="isStoredApiKeyPendingDelete(row) ? 'refresh' : 'trash'" size="xs" />
                            </button>
                          </div>
                        </div>
                        <p v-if="row.last_error" class="mt-1.5 rounded-md bg-amber-50 px-2 py-1.5 text-xs leading-5 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                          {{ row.last_error }}
                        </p>
                      </div>
                    </div>

                    <div v-if="canToggleApiKeyRows" class="flex items-center justify-between gap-3 rounded-lg border border-dashed border-gray-200 bg-white px-3 py-2 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400">
                      <span class="min-w-0 truncate">
                        {{ apiKeyRowsExpanded ? t('admin.riskControl.apiKeyRowsExpanded', { count: apiKeyRows.length }) : t('admin.riskControl.apiKeyRowsCollapsed', { count: hiddenApiKeyRowCount }) }}
                      </span>
                      <button
                        type="button"
                        class="inline-flex shrink-0 items-center gap-1 rounded-md px-2 py-1 font-medium text-primary-600 transition-colors hover:bg-primary-50 hover:text-primary-700 dark:text-primary-300 dark:hover:bg-primary-900/20"
                        @click="apiKeyRowsExpanded = !apiKeyRowsExpanded"
                      >
                        <Icon :name="apiKeyRowsExpanded ? 'chevronUp' : 'chevronDown'" size="xs" />
                        {{ apiKeyRowsExpanded ? t('admin.riskControl.collapseApiKeyRows') : t('admin.riskControl.expandApiKeyRows') }}
                      </button>
                    </div>
                  </div>

                  <div v-if="moderationTestResult" class="mt-4 rounded-lg border border-gray-100 bg-white p-3 dark:border-dark-700 dark:bg-dark-800">
                    <div class="flex items-start justify-between gap-3">
                      <div>
                        <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.auditTestResult') }}</p>
                        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                          {{ t('admin.riskControl.auditTestHighest', { category: moderationTestResult.highest_category || '-', score: percent(moderationTestResult.highest_score) }) }}
                        </p>
                      </div>
                      <span class="inline-flex rounded-full px-2 py-1 text-xs font-medium" :class="moderationTestResult.flagged ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'">
                        {{ moderationTestResult.flagged ? t('admin.riskControl.auditTestFlagged') : t('admin.riskControl.auditTestPassed') }}
                      </span>
                    </div>
                    <div class="mt-3">
                      <div class="mb-2 flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
                        <span>{{ t('admin.riskControl.auditTestComposite') }}</span>
                        <span class="font-semibold text-gray-900 dark:text-white">{{ percent(moderationTestResult.composite_score) }}</span>
                      </div>
                      <div class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                        <div class="h-full rounded-full" :class="moderationTestResult.flagged ? 'bg-red-500' : 'bg-emerald-500'" :style="{ width: percentWidth(moderationTestResult.composite_score) }"></div>
                      </div>
                    </div>
                    <div class="mt-3 max-h-52 space-y-2 overflow-y-auto pr-1">
                      <div v-for="score in moderationScoreRows" :key="score.category">
                        <div class="mb-1 flex items-center justify-between gap-3 text-xs">
                          <span class="truncate text-gray-600 dark:text-gray-300">{{ score.category }}</span>
                          <span class="font-mono text-gray-500 dark:text-gray-400">{{ percent(score.score) }} / {{ percent(score.threshold) }}</span>
                        </div>
                        <div class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                          <div class="h-full rounded-full" :class="score.hit ? 'bg-red-500' : 'bg-primary-500'" :style="{ width: percentWidth(score.score) }"></div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div v-else-if="activeSettingsTab === 'scope'" class="space-y-5">
            <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.groupScope') }}</h3>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.groupScopeHint') }}</p>
              </div>
              <div class="inline-flex rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
                <button
                  type="button"
                  class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
                  :class="configForm.all_groups ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white' : 'text-gray-500 dark:text-gray-400'"
                  @click="configForm.all_groups = true"
                >
                  {{ t('admin.riskControl.allGroups') }}
                </button>
                <button
                  type="button"
                  class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
                  :class="!configForm.all_groups ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white' : 'text-gray-500 dark:text-gray-400'"
                  @click="configForm.all_groups = false"
                >
                  {{ t('admin.riskControl.selectedGroups') }}
                </button>
              </div>
            </div>

            <div v-if="!configForm.all_groups" class="space-y-4">
              <div class="relative">
                <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
                <input v-model.trim="groupSearch" type="search" class="input pl-9" :placeholder="t('admin.riskControl.searchGroups')" />
              </div>
              <div class="grid max-h-[420px] grid-cols-1 gap-3 overflow-y-auto pr-1 md:grid-cols-2 xl:grid-cols-3">
                <button
                  v-for="group in filteredGroups"
                  :key="group.id"
                  type="button"
                  class="flex min-h-20 items-center justify-between rounded-lg border p-4 text-left transition-colors"
                  :class="isGroupSelected(group.id) ? 'border-primary-300 bg-primary-50 dark:border-primary-700 dark:bg-primary-900/20' : 'border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/60'"
                  @click="toggleGroup(group.id)"
                >
                  <span class="min-w-0">
                    <span class="block truncate text-sm font-semibold text-gray-900 dark:text-white">{{ group.name }}</span>
                    <span class="mt-1 inline-flex rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ group.platform }}</span>
                  </span>
                  <span
                    class="flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full border"
                    :class="isGroupSelected(group.id) ? 'border-primary-500 bg-primary-500 text-white' : 'border-gray-300 text-transparent dark:border-dark-500'"
                  >
                    <Icon name="check" size="xs" :stroke-width="2" />
                  </span>
                </button>
                <p v-if="filteredGroups.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.noGroups') }}</p>
              </div>
            </div>

            <div class="space-y-4 rounded-lg border border-gray-100 p-4 dark:border-dark-700">
              <div class="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
                <div>
                  <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.modelFilter') }}</h3>
                  <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.modelFilterHint') }}</p>
                </div>
                <span class="inline-flex w-fit rounded-md bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                  {{ modelFilterSummary }}
                </span>
              </div>

              <div class="grid grid-cols-1 gap-2 md:grid-cols-3">
                <button
                  v-for="option in modelFilterOptions"
                  :key="option.value"
                  type="button"
                  class="rounded-lg border p-3 text-left transition-colors"
                  :class="configForm.model_filter_type === option.value
                    ? 'border-primary-300 bg-primary-50 text-primary-900 shadow-sm dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-100'
                    : 'border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/60'"
                  @click="setModelFilterType(option.value)"
                >
                  <div class="flex items-center justify-between gap-2">
                    <span class="text-sm font-semibold">{{ option.label }}</span>
                    <span
                      class="flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full border"
                      :class="configForm.model_filter_type === option.value
                        ? 'border-primary-500 bg-primary-500 text-white'
                        : 'border-gray-300 text-transparent dark:border-dark-500'"
                    >
                      <Icon name="check" size="xs" :stroke-width="2" />
                    </span>
                  </div>
                  <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ option.description }}</p>
                </button>
              </div>

              <div v-if="configForm.model_filter_type !== 'all'" class="space-y-2">
                <label class="input-label">{{ t('admin.riskControl.modelFilterModels') }}</label>
                <ModelWhitelistSelector v-model="configForm.model_filter_models" />
                <p class="text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.riskControl.modelFilterModelCount', { count: modelFilterModelCount }) }}
                </p>
              </div>
            </div>
          </div>

          <div v-else-if="activeSettingsTab === 'runtime'" class="grid grid-cols-1 gap-5 lg:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.riskControl.workerCount') }}</label>
              <input v-model.number="configForm.worker_count" type="number" min="1" max="32" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.riskControl.queueSize') }}</label>
              <input v-model.number="configForm.queue_size" type="number" min="100" max="100000" class="input" />
            </div>
            <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
              <div>
                <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.recordNonHits') }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.recordNonHitsHint') }}</p>
              </div>
              <Toggle v-model="configForm.record_non_hits" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.riskControl.auditScope') }}</label>
              <Select v-model="configForm.audit_scope" :options="auditScopeOptions" />
            </div>
            <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
              <div>
                <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.storeInputExcerpt') }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.storeInputExcerptHint') }}</p>
              </div>
              <Toggle v-model="configForm.store_input_excerpt" />
            </div>
            <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
              <div>
                <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.searchInputExcerpt') }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.searchInputExcerptHint') }}</p>
              </div>
              <Toggle v-model="configForm.search_input_excerpt" />
            </div>
            <div class="space-y-4 rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.preHashCheck') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.preHashCheckHint') }}</p>
                </div>
                <Toggle v-model="configForm.pre_hash_check_enabled" />
              </div>
              <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-900/30">
                <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
                  <div>
                    <p class="text-sm font-medium text-gray-900 dark:text-white">
                      {{ t('admin.riskControl.flaggedHashCount', { count: formatNumber(status?.flagged_hash_count ?? 0) }) }}
                    </p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.flaggedHashHint') }}</p>
                  </div>
                  <button
                    type="button"
                    class="btn btn-secondary inline-flex items-center justify-center gap-2 text-red-600 hover:text-red-700 dark:text-red-300"
                    :disabled="hashActionLoading || (status?.flagged_hash_count ?? 0) === 0"
                    @click="clearFlaggedHashes"
                  >
                    <Icon name="trash" size="sm" :class="hashActionLoading ? 'animate-pulse' : ''" />
                    {{ t('admin.riskControl.clearFlaggedHashes') }}
                  </button>
                </div>
                <div class="mt-3 flex flex-col gap-2 sm:flex-row">
                  <input
                    v-model.trim="flaggedHashInput"
                    type="text"
                    class="input font-mono text-sm"
                    :placeholder="t('admin.riskControl.flaggedHashPlaceholder')"
                  />
                  <button
                    type="button"
                    class="btn btn-secondary inline-flex items-center justify-center gap-2"
                    :disabled="hashActionLoading || !isFlaggedHashInputValid"
                    @click="deleteFlaggedHash"
                  >
                    <Icon name="trash" size="sm" />
                    {{ t('admin.riskControl.deleteFlaggedHash') }}
                  </button>
                </div>
              </div>
            </div>
          </div>

          <div v-else-if="activeSettingsTab === 'response'" class="space-y-5">
            <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.riskControl.blockStatus') }}</label>
                <input v-model.number="configForm.block_status" type="number" min="400" max="599" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.blockMessage') }}</label>
                <input v-model.trim="configForm.block_message" type="text" class="input" />
              </div>
              <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.emailOnHit') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.emailOnHitHint') }}</p>
                </div>
                <Toggle v-model="configForm.email_on_hit" />
              </div>
              <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.autoBan') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.autoBanHint') }}</p>
                </div>
                <Toggle v-model="configForm.auto_ban_enabled" />
              </div>
              <div class="flex items-center justify-between rounded-lg border border-gray-100 p-4 dark:border-dark-700 lg:col-span-2">
                <div>
                  <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.cyberPolicyExcludeBan') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberPolicyExcludeBanHint') }}</p>
                </div>
                <Toggle v-model="configForm.cyber_policy_exclude_from_ban_count" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.banThreshold') }}</label>
                <input v-model.number="configForm.ban_threshold" type="number" min="1" max="1000" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.riskControl.violationWindowHours') }}</label>
                <input v-model.number="configForm.violation_window_hours" type="number" min="1" max="8760" class="input" />
              </div>
            </div>
          </div>

          <div v-else-if="activeSettingsTab === 'riskThresholds'" class="space-y-5">
            <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.riskThresholds') }}</h3>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.riskThresholdsHint') }}</p>
              </div>
              <button
                type="button"
                class="btn btn-secondary inline-flex items-center justify-center gap-2"
                @click="resetRiskThresholds"
              >
                <Icon name="refresh" size="sm" />
                {{ t('admin.riskControl.riskThresholdReset') }}
              </button>
            </div>

            <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
              <div
                v-for="row in riskThresholdRows"
                :key="row.category"
                class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <label class="block truncate text-sm font-semibold text-gray-900 dark:text-white" :for="`risk-threshold-${row.category}`">
                      {{ row.category }}
                    </label>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{ t('admin.riskControl.riskThresholdDefault', { value: formatThresholdPercent(row.defaultValue) }) }}
                    </p>
                  </div>
                  <span class="inline-flex shrink-0 rounded-md bg-white px-2 py-1 font-mono text-xs font-medium text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300">
                    {{ formatThresholdPercent(row.value) }}
                  </span>
                </div>
                <div class="mt-3">
                  <label class="sr-only" :for="`risk-threshold-${row.category}`">
                    {{ t('admin.riskControl.riskThresholdPercent') }}
                  </label>
                  <div class="relative">
                    <input
                      :id="`risk-threshold-${row.category}`"
                      v-model.number="configForm.thresholds[row.category]"
                      :data-test="`risk-threshold-${row.category}`"
                      type="number"
                      min="0"
                      max="100"
                      step="0.1"
                      class="input pr-8 font-mono"
                    />
                    <span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-gray-400">%</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div v-else-if="activeSettingsTab === 'keywords'" class="space-y-5">
            <div
              class="flex items-start gap-3 rounded-lg border p-4"
              :class="keywordNotice.toneClass"
            >
              <Icon
                :name="keywordNotice.icon"
                size="md"
                :class="keywordNotice.iconClass"
              />
              <div class="text-sm leading-6">
                <p class="font-medium" :class="keywordNotice.titleClass">{{ keywordNotice.title }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ keywordNotice.description }}</p>
              </div>
            </div>

            <div class="space-y-2">
              <label class="input-label">{{ t('admin.riskControl.keywordBlockingMode') }}</label>
              <div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
                <button
                  v-for="option in keywordBlockingModeOptions"
                  :key="option.value"
                  type="button"
                  class="rounded-lg border p-3 text-left transition-colors"
                  :class="configForm.keyword_blocking_mode === option.value
                    ? 'border-primary-300 bg-primary-50 text-primary-900 shadow-sm dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-100'
                    : 'border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/60'"
                  @click="configForm.keyword_blocking_mode = option.value"
                >
                  <div class="flex items-center justify-between gap-2">
                    <span class="text-sm font-semibold">{{ option.label }}</span>
                    <span
                      class="flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full border"
                      :class="configForm.keyword_blocking_mode === option.value
                        ? 'border-primary-500 bg-primary-500 text-white'
                        : 'border-gray-300 text-transparent dark:border-dark-500'"
                    >
                      <Icon name="check" size="xs" :stroke-width="2" />
                    </span>
                  </div>
                  <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ option.description }}</p>
                </button>
              </div>
            </div>

            <div class="overflow-hidden rounded-lg border border-gray-100 bg-white dark:border-dark-700 dark:bg-dark-800">
              <div class="flex flex-col gap-3 border-b border-gray-100 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-800/60 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.keywordRules') }}</p>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.keywordRulesDescription') }}</p>
                </div>
                <span class="inline-flex w-fit rounded-md bg-white px-2 py-1 text-xs text-gray-500 shadow-sm dark:bg-dark-700 dark:text-gray-300">
                  {{ t('admin.riskControl.keywordRuleCount', { enabled: enabledKeywordRuleCount, total: keywordRuleCount }) }}
                </span>
              </div>
              <div v-if="keywordRuleList.length > 0" class="overflow-x-auto">
                <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
                  <thead class="bg-white text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                    <tr>
                      <th class="px-4 py-3 text-left font-medium">{{ t('admin.riskControl.matchedKeyword') }}</th>
                      <th class="px-4 py-3 text-left font-medium">{{ t('admin.riskControl.keywordCategory') }}</th>
                      <th class="px-4 py-3 text-left font-medium">{{ t('admin.riskControl.keywordSeverity') }}</th>
                      <th class="px-4 py-3 text-left font-medium">{{ t('admin.riskControl.keywordAction') }}</th>
                      <th class="px-4 py-3 text-left font-medium">{{ t('admin.riskControl.keywordRuleStatus') }}</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-800">
                    <tr v-for="rule in keywordRuleList" :key="`${rule.keyword}:${rule.category}:${rule.severity}:${rule.action}`">
                      <td class="max-w-[360px] px-4 py-3">
                        <span class="block break-words font-mono text-xs font-semibold text-gray-900 dark:text-white">{{ rule.keyword }}</span>
                      </td>
                      <td class="px-4 py-3">
                        <span class="inline-flex rounded-md bg-sky-50 px-2 py-1 font-mono text-xs font-medium text-sky-700 dark:bg-sky-900/20 dark:text-sky-300">{{ rule.category || '-' }}</span>
                      </td>
                      <td class="px-4 py-3">
                        <span class="inline-flex rounded-md bg-amber-50 px-2 py-1 font-mono text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ rule.severity || '-' }}</span>
                      </td>
                      <td class="px-4 py-3">
                        <span class="font-mono text-xs font-medium text-gray-700 dark:text-gray-200">{{ rule.action || '-' }}</span>
                      </td>
                      <td class="px-4 py-3">
                        <span
                          class="inline-flex rounded-full px-2 py-1 text-xs font-medium"
                          :class="rule.enabled ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-300'"
                        >
                          {{ rule.enabled ? t('admin.riskControl.keywordRuleEnabled') : t('admin.riskControl.keywordRuleDisabled') }}
                        </span>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div v-else class="px-4 py-6 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.riskControl.keywordRulesEmpty') }}
              </div>
            </div>

            <div>
              <div class="mb-2 flex items-center justify-between">
                <label class="input-label mb-0">{{ t('admin.riskControl.legacyBlockedKeywords') }}</label>
                <span class="inline-flex rounded-md bg-gray-100 px-2 py-1 text-xs text-gray-500 dark:bg-dark-700 dark:text-gray-300">
                  {{ t('admin.riskControl.legacyBlockedKeywordCount', { count: legacyBlockedKeywordCount }) }}
                </span>
              </div>
              <textarea
                v-model="configForm.blocked_keywords_text"
                class="input min-h-52 resize-y font-mono text-sm"
                :placeholder="t('admin.riskControl.blockedKeywordsPlaceholder')"
                :disabled="configForm.keyword_blocking_mode === 'api_only'"
              ></textarea>
              <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.riskControl.blockedKeywordsLimit', { max: blockedKeywordMax }) }}
              </p>
            </div>

            <div class="grid grid-cols-1 gap-4 rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30 lg:grid-cols-[minmax(0,1fr)_minmax(280px,360px)]">
              <div class="space-y-3">
                <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                  <div>
                    <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.keywordTest') }}</p>
                    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.keywordTestHint') }}</p>
                  </div>
                  <button
                    type="button"
                    class="btn btn-secondary inline-flex items-center justify-center gap-2"
                    :disabled="keywordTesting || keywordTestPrompt.trim() === ''"
                    @click="runKeywordTest"
                  >
                    <Icon name="beaker" size="sm" :class="keywordTesting ? 'animate-pulse' : ''" />
                    {{ keywordTesting ? t('admin.riskControl.keywordTesting') : t('admin.riskControl.runKeywordTest') }}
                  </button>
                </div>
                <textarea
                  v-model="keywordTestPrompt"
                  data-test="keyword-test-prompt"
                  class="input min-h-28 resize-y text-sm"
                  :placeholder="t('admin.riskControl.keywordTestPlaceholder')"
                ></textarea>
              </div>

              <div class="rounded-lg border border-gray-100 bg-white p-3 dark:border-dark-700 dark:bg-dark-800">
                <div v-if="keywordTestResult" class="space-y-3">
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.keywordTestResult') }}</p>
                      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ keywordTestResult.normalized_excerpt || '-' }}</p>
                    </div>
                    <span class="inline-flex shrink-0 rounded-full px-2 py-1 text-xs font-medium" :class="keywordTestResult.matched ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'">
                      {{ keywordTestResult.matched ? t('admin.riskControl.keywordTestMatched') : t('admin.riskControl.keywordTestPassed') }}
                    </span>
                  </div>
                  <div class="grid grid-cols-2 gap-2 text-xs">
                    <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
                      <p class="text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.matchedKeyword') }}</p>
                      <p class="mt-1 break-words font-mono font-semibold text-gray-900 dark:text-white">{{ keywordTestResult.matched_keyword || '-' }}</p>
                    </div>
                    <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
                      <p class="text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.keywordCategory') }}</p>
                      <p class="mt-1 break-words font-mono font-semibold text-gray-900 dark:text-white">{{ keywordTestResult.keyword_category || '-' }}</p>
                    </div>
                    <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
                      <p class="text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.keywordSeverity') }}</p>
                      <p class="mt-1 break-words font-mono font-semibold text-gray-900 dark:text-white">{{ keywordTestResult.keyword_severity || '-' }}</p>
                    </div>
                    <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
                      <p class="text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.keywordAction') }}</p>
                      <p class="mt-1 break-words font-mono font-semibold text-gray-900 dark:text-white">{{ keywordTestResult.keyword_action || '-' }}</p>
                    </div>
                    <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
                      <p class="text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.effectiveKeywordAction') }}</p>
                      <p class="mt-1 break-words font-mono font-semibold text-gray-900 dark:text-white">{{ keywordTestResult.effective_keyword_action || '-' }}</p>
                    </div>
                    <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
                      <p class="text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.riskContext') }}</p>
                      <p class="mt-1 break-words font-mono font-semibold text-gray-900 dark:text-white">{{ riskContextLabel(keywordTestResult.risk_context_type) }}</p>
                    </div>
                  </div>
                </div>
                <div v-else class="flex min-h-32 items-center justify-center rounded-lg border border-dashed border-gray-200 px-4 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400">
                  {{ t('admin.riskControl.keywordTestEmpty') }}
                </div>
              </div>
            </div>
          </div>

          <div v-else class="grid grid-cols-1 gap-5 lg:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.riskControl.hitRetentionDays') }}</label>
              <input v-model.number="configForm.hit_retention_days" type="number" min="1" max="3650" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.riskControl.nonHitRetentionDays') }}</label>
              <input v-model.number="configForm.non_hit_retention_days" type="number" min="1" max="3" class="input" />
            </div>
            <div class="rounded-lg border border-gray-100 p-4 text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400 lg:col-span-2">
              <div class="flex flex-wrap items-center gap-3">
                <Icon name="database" size="md" class="text-gray-400" />
                <span>{{ t('admin.riskControl.cleanupStats', { hit: status?.last_cleanup_deleted_hit ?? 0, nonHit: status?.last_cleanup_deleted_non_hit ?? 0 }) }}</span>
              </div>
            </div>
          </div>
        </div>

        <template #footer>
          <div class="flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" @click="settingsOpen = false">{{ t('common.cancel') }}</button>
            <button type="button" class="btn btn-primary inline-flex items-center gap-2" :disabled="saving" @click="saveConfig">
              <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="check" size="sm" />
              {{ saving ? t('common.saving') : t('admin.riskControl.saveConfig') }}
            </button>
          </div>
        </template>
      </BaseDialog>

      <BaseDialog
        :show="inputDetailRow !== null"
        :title="t('admin.riskControl.inputDetailTitle')"
        width="wide"
        @close="closeInputDetail"
      >
        <div v-if="inputDetailRow" class="space-y-5">
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.time') }}</p>
              <p class="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-white">{{ formatDateTime(inputDetailRow.created_at) }}</p>
            </div>
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.user') }}</p>
              <p class="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-white">{{ inputDetailRow.user_email || '-' }}</p>
            </div>
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.result') }}</p>
              <span class="mt-1 inline-flex rounded-md px-2 py-1 text-xs font-medium" :class="resultBadgeClass(inputDetailRow)">
                {{ resultLabel(inputDetailRow) }}
              </span>
            </div>
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.table.highest') }}</p>
              <p class="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-white">
                {{ inputDetailRow.highest_category || '-' }} / {{ percent(inputDetailRow.highest_score) }}
              </p>
            </div>
            <div v-if="inputDetailRow.matched_keyword" class="rounded-lg border border-red-100 bg-red-50 p-4 dark:border-red-900/60 dark:bg-red-900/20">
              <p class="text-xs font-medium text-red-500 dark:text-red-300">{{ t('admin.riskControl.matchedKeyword') }}</p>
              <p class="mt-1 truncate text-sm font-semibold text-red-700 dark:text-red-200" :title="inputDetailRow.matched_keyword">{{ inputDetailRow.matched_keyword }}</p>
            </div>
          </div>

          <div v-if="inputDetailRow.matched_keyword" class="rounded-xl border border-amber-100 bg-amber-50 p-4 shadow-sm dark:border-amber-900/40 dark:bg-amber-900/10">
            <p class="text-sm font-semibold text-amber-800 dark:text-amber-100">{{ t('admin.riskControl.keywordMetadata') }}</p>
            <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div>
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.matchedKeyword') }}</p>
                <p class="mt-1 break-words font-mono text-sm font-semibold text-amber-900 dark:text-amber-50">{{ inputDetailRow.matched_keyword }}</p>
              </div>
              <div>
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.keywordCategory') }}</p>
                <p class="mt-1 break-words font-mono text-sm font-semibold text-amber-900 dark:text-amber-50">{{ inputDetailRow.keyword_category || '-' }}</p>
              </div>
              <div>
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.keywordSeverity') }}</p>
                <p class="mt-1 break-words font-mono text-sm font-semibold text-amber-900 dark:text-amber-50">{{ inputDetailRow.keyword_severity || '-' }}</p>
              </div>
              <div>
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.keywordAction') }}</p>
                <p class="mt-1 break-words font-mono text-sm font-semibold text-amber-900 dark:text-amber-50">{{ keywordActionText(inputDetailRow) }}</p>
              </div>
              <div>
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.riskContext') }}</p>
                <p class="mt-1 break-words font-mono text-sm font-semibold text-amber-900 dark:text-amber-50">{{ riskContextLabel(inputDetailRow.risk_context_type) }}</p>
              </div>
              <div>
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.reviewStatusLabel') }}</p>
                <p class="mt-1 break-words font-mono text-sm font-semibold text-amber-900 dark:text-amber-50">{{ reviewStatusLabel(inputDetailRow.review_status) }}</p>
              </div>
              <div class="sm:col-span-3">
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.riskContextReason') }}</p>
                <p class="mt-1 break-words font-mono text-sm font-semibold text-amber-900 dark:text-amber-50">{{ inputDetailRow.risk_context_reason || '-' }}</p>
              </div>
              <div v-if="inputDetailRow.review_note" class="sm:col-span-3">
                <p class="text-xs font-medium text-amber-700/80 dark:text-amber-200/80">{{ t('admin.riskControl.reviewNote') }}</p>
                <p class="mt-1 break-words text-sm font-semibold text-amber-900 dark:text-amber-50">{{ inputDetailRow.review_note }}</p>
              </div>
            </div>
          </div>

          <div class="rounded-xl border border-gray-100 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.inputDetailContent') }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ inputDetailRow.endpoint || '-' }} · {{ inputDetailRow.provider || '-' }} / {{ inputDetailRow.model || '-' }}
                </p>
              </div>
              <span v-if="inputDetailRow.group_name" class="inline-flex rounded-md bg-sky-50 px-2.5 py-1 text-xs font-medium text-sky-700 dark:bg-sky-900/20 dark:text-sky-300">
                {{ inputDetailRow.group_name }}
              </span>
            </div>
            <pre class="mt-4 max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-950 p-4 text-sm leading-6 text-gray-100 shadow-inner dark:bg-black/50">{{ inputDetailText }}</pre>
          </div>
        </div>

        <template #footer>
          <div class="flex justify-end">
            <button type="button" class="btn btn-secondary" @click="closeInputDetail">{{ t('common.close') }}</button>
          </div>
        </template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Pagination from '@/components/common/Pagination.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import { adminAPI } from '@/api/admin'
import type {
  ContentModerationAuditScope,
  ContentModerationAPIKeyLoad,
  ContentModerationAPIKeyStatus,
  ContentModerationConfig,
  ContentModerationKeywordRule,
  ContentModerationLog,
  ContentModerationModelFilter,
  ContentModerationModelFilterType,
  ContentModerationPipelineGroupCoverageStatus,
  ContentModerationPipelineRouteCoverageStatus,
  ContentModerationPipelineRouteStageCoverageStatus,
  ContentModerationPipelineStageCoverageStatus,
  ContentModerationRuntimeStatus,
  ContentModerationTestAuditResult,
  KeywordBlockingMode,
  ModerationMode,
  TestContentModerationKeywordsResponse,
  UpdateContentModerationConfig,
} from '@/api/admin/riskControl'
import type { AdminGroup, SelectOption } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime as formatDateTimeValue } from '@/utils/format'

type SettingsTab = 'basic' | 'scope' | 'runtime' | 'response' | 'riskThresholds' | 'retention' | 'keywords'
type WorkerSlotState = 'active' | 'idle' | 'disabled'
type APIKeysWriteMode = 'append' | 'replace'
type OverviewIcon = 'shield' | 'key' | 'users' | 'document'
type ProtectionStatusTone = 'strong' | 'unsafe' | 'unknown'
type PipelineOperatorIcon = 'shield' | 'filter' | 'refresh' | 'document'
type OverviewItem = {
  key: string
  label: string
  value: string
  meta: string
  icon: OverviewIcon
  iconClass: string
  badge?: string
  badgeClass?: string
}
type PipelineOperatorSummaryItem = {
  key: string
  label: string
  value: string
  meta: string
  icon: PipelineOperatorIcon
  iconClass: string
  valueClass?: string
}
type ModerationScoreRow = {
  category: string
  score: number
  threshold: number
  hit: boolean
}
type RiskThresholdRow = {
  category: string
  value: number
  defaultValue: number
}

const maxModerationTestImages = 1
const maxModerationTestImageSize = 8 * 1024 * 1024
const maxVisibleApiKeyRows: number = 3
const blockedKeywordMax = 10000
const riskThresholdDefaults: Record<string, number> = {
  harassment: 98,
  'harassment/threatening': 90,
  hate: 65,
  'hate/threatening': 65,
  illicit: 95,
  'illicit/violent': 95,
  'self-harm': 65,
  'self-harm/intent': 85,
  'self-harm/instructions': 65,
  sexual: 65,
  'sexual/minors': 65,
  violence: 95,
  'violence/graphic': 95,
}
const riskThresholdCategories = Object.keys(riskThresholdDefaults)

const { t } = useI18n()
const appStore = useAppStore()
const defaultBlockMessage = () => t('admin.riskControl.defaultBlockMessage')

const loading = ref(true)
const saving = ref(false)
const logsLoading = ref(false)
const statusLoading = ref(false)
const apiKeyTesting = ref(false)
const keywordTesting = ref(false)
const hashActionLoading = ref(false)
const unbanningUserID = ref<number | null>(null)
const reviewingLogID = ref<number | null>(null)
const settingsOpen = ref(false)
const advancedPipelineDiagnosticsOpen = ref(false)
const activeSettingsTab = ref<SettingsTab>('basic')
const groupSearch = ref('')
const flaggedHashInput = ref('')
const groups = ref<AdminGroup[]>([])
const logs = ref<ContentModerationLog[]>([])
const status = ref<ContentModerationRuntimeStatus | null>(null)
const testedApiKeyStatuses = ref<ContentModerationAPIKeyStatus[]>([])
const pendingDeleteApiKeyHashes = ref<string[]>([])
const apiKeyRowsExpanded = ref<boolean>(false)
const moderationTestPrompt = ref('')
const moderationTestImages = ref<string[]>([])
const moderationTestResult = ref<ContentModerationTestAuditResult | null>(null)
const keywordTestPrompt = ref('')
const keywordTestResult = ref<TestContentModerationKeywordsResponse | null>(null)
const inputDetailRow = ref<ContentModerationLog | null>(null)
let statusTimer: number | null = null

const configForm = reactive({
  enabled: false,
  mode: 'pre_block' as ModerationMode,
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  api_keys_text: '',
  api_key_configured: false,
  api_key_masked: '',
  api_key_count: 0,
  api_key_masks: [] as string[],
  api_key_statuses: [] as ContentModerationAPIKeyStatus[],
  api_keys_mode: 'append' as APIKeysWriteMode,
  clear_api_key: false,
  timeout_ms: 3000,
  retry_count: 2,
  sample_rate: 100,
  all_groups: true,
  group_ids: [] as number[],
  record_non_hits: false,
  audit_scope: 'all_context' as ContentModerationAuditScope,
  store_input_excerpt: true,
  search_input_excerpt: false,
  worker_count: 4,
  queue_size: 32768,
  block_status: 403,
  block_message: defaultBlockMessage(),
  email_on_hit: true,
  auto_ban_enabled: true,
  cyber_policy_exclude_from_ban_count: false,
  ban_threshold: 10,
  violation_window_hours: 720,
  hit_retention_days: 180,
  non_hit_retention_days: 3,
  pre_hash_check_enabled: false,
  thresholds: { ...riskThresholdDefaults } as Record<string, number>,
  blocked_keywords_text: '',
  keyword_rules: [] as ContentModerationKeywordRule[],
  keyword_blocking_mode: 'keyword_and_api' as KeywordBlockingMode,
  model_filter_type: 'all' as ContentModerationModelFilterType,
  model_filter_models: [] as string[],
})

const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0,
  pages: 1,
})

const filters = reactive({
  result: '',
  review_status: '',
  group_id: 0,
  endpoint: '',
  search: '',
  from: '',
  to: '',
})

const settingsTabs = computed<Array<{ id: SettingsTab; label: string }>>(() => [
  { id: 'basic', label: t('admin.riskControl.tabs.basic') },
  { id: 'scope', label: t('admin.riskControl.tabs.scope') },
  { id: 'runtime', label: t('admin.riskControl.tabs.runtime') },
  { id: 'response', label: t('admin.riskControl.tabs.response') },
  { id: 'riskThresholds', label: t('admin.riskControl.tabs.riskThresholds') },
  { id: 'keywords', label: t('admin.riskControl.tabs.keywords') },
  { id: 'retention', label: t('admin.riskControl.tabs.retention') },
])

const modeOptions = computed<SelectOption[]>(() => [
  { value: 'pre_block', label: t('admin.riskControl.modePreBlock') },
  { value: 'observe', label: t('admin.riskControl.modeObserve') },
  { value: 'off', label: t('admin.riskControl.modeOff') },
])

const keywordBlockingModeOptions = computed<Array<{ value: KeywordBlockingMode; label: string; description: string }>>(() => [
  {
    value: 'keyword_and_api',
    label: t('admin.riskControl.keywordModeKeywordAndApi'),
    description: t('admin.riskControl.keywordModeKeywordAndApiDesc'),
  },
  {
    value: 'keyword_only',
    label: t('admin.riskControl.keywordModeKeywordOnly'),
    description: t('admin.riskControl.keywordModeKeywordOnlyDesc'),
  },
  {
    value: 'api_only',
    label: t('admin.riskControl.keywordModeApiOnly'),
    description: t('admin.riskControl.keywordModeApiOnlyDesc'),
  },
])

const auditScopeOptions = computed<SelectOption[]>(() => [
  { value: 'all_context', label: t('admin.riskControl.auditScopeAllContext') },
  { value: 'user_and_tool', label: t('admin.riskControl.auditScopeUserAndTool') },
  { value: 'user_only', label: t('admin.riskControl.auditScopeUserOnly') },
])

const modelFilterOptions = computed<Array<{ value: ContentModerationModelFilterType; label: string; description: string }>>(() => [
  {
    value: 'all',
    label: t('admin.riskControl.modelFilterAll'),
    description: t('admin.riskControl.modelFilterAllDesc'),
  },
  {
    value: 'include',
    label: t('admin.riskControl.modelFilterInclude'),
    description: t('admin.riskControl.modelFilterIncludeDesc'),
  },
  {
    value: 'exclude',
    label: t('admin.riskControl.modelFilterExclude'),
    description: t('admin.riskControl.modelFilterExcludeDesc'),
  },
])

type KeywordNoticeView = {
  title: string
  description: string
  icon: 'infoCircle' | 'exclamationTriangle'
  toneClass: string
  iconClass: string
  titleClass: string
}

const keywordNoticeTones = {
  info: {
    icon: 'infoCircle' as const,
    toneClass: 'border-primary-100 bg-primary-50/60 dark:border-primary-900/40 dark:bg-primary-900/10',
    iconClass: 'mt-0.5 flex-shrink-0 text-primary-500 dark:text-primary-300',
    titleClass: 'text-primary-700 dark:text-primary-200',
  },
  warning: {
    icon: 'exclamationTriangle' as const,
    toneClass: 'border-amber-200 bg-amber-50 dark:border-amber-900/40 dark:bg-amber-900/20',
    iconClass: 'mt-0.5 flex-shrink-0 text-amber-500 dark:text-amber-300',
    titleClass: 'text-amber-700 dark:text-amber-200',
  },
}

const keywordNotice = computed<KeywordNoticeView>(() => {
  const strategy = configForm.keyword_blocking_mode
  if (strategy === 'api_only') {
    return {
      ...keywordNoticeTones.info,
      title: t('admin.riskControl.keywordModeApiOnlyNotice'),
      description: t('admin.riskControl.keywordModeApiOnlyDesc'),
    }
  }
  if (configForm.mode !== 'pre_block') {
    return {
      ...keywordNoticeTones.warning,
      title: t('admin.riskControl.blockedKeywordsModeWarning', { mode: modeLabel(configForm.mode) }),
      description: t('admin.riskControl.blockedKeywordsDescription'),
    }
  }
  if (strategy === 'keyword_only') {
    return {
      ...keywordNoticeTones.info,
      title: t('admin.riskControl.keywordModeKeywordOnlyNotice'),
      description: t('admin.riskControl.keywordModeKeywordOnlyDesc'),
    }
  }
  return {
    ...keywordNoticeTones.info,
    title: t('admin.riskControl.blockedKeywordsPreBlockHint'),
    description: t('admin.riskControl.blockedKeywordsDescription'),
  }
})

const resultOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('admin.riskControl.result.all') },
  { value: 'hit', label: t('admin.riskControl.result.hit') },
  { value: 'blocked', label: t('admin.riskControl.result.blocked') },
  { value: 'review', label: t('admin.riskControl.result.review') },
  { value: 'pass', label: t('admin.riskControl.result.pass') },
  { value: 'error', label: t('admin.riskControl.result.error') },
])

const reviewStatusOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('admin.riskControl.reviewStatus.all') },
  { value: 'pending', label: t('admin.riskControl.reviewStatus.pending') },
  { value: 'false_positive', label: t('admin.riskControl.reviewStatus.falsePositive') },
  { value: 'confirmed_violation', label: t('admin.riskControl.reviewStatus.confirmedViolation') },
])

const endpointOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('admin.riskControl.filters.allEndpoints') },
  { value: '/v1/messages', label: '/v1/messages' },
  { value: '/v1/responses', label: '/v1/responses' },
  { value: '/v1/chat/completions', label: '/v1/chat/completions' },
  { value: '/v1beta/models', label: '/v1beta/models' },
  { value: '/v1/images/generations', label: '/v1/images/generations' },
  { value: '/v1/images/edits', label: '/v1/images/edits' },
])

const groupFilterOptions = computed<SelectOption[]>(() => [
  { value: 0, label: t('admin.riskControl.filters.allGroups') },
  ...groups.value.map((group) => ({
    value: group.id,
    label: `${group.name} (${group.platform})`,
  })),
])

const selectedGroupCount = computed(() => String(configForm.group_ids.length))

const modelFilterModelCount = computed(() => configForm.model_filter_models.length)

const modelFilterSummary = computed(() => {
  if (configForm.model_filter_type === 'include') {
    return t('admin.riskControl.modelFilterIncludeSummary', { count: modelFilterModelCount.value })
  }
  if (configForm.model_filter_type === 'exclude') {
    return t('admin.riskControl.modelFilterExcludeSummary', { count: modelFilterModelCount.value })
  }
  return t('admin.riskControl.modelFilterAllSummary')
})

const modelFilterPreviewModels = computed(() => configForm.model_filter_models.slice(0, 6))

const hiddenModelFilterModelCount = computed(() => Math.max(0, configForm.model_filter_models.length - modelFilterPreviewModels.value.length))

const filteredGroups = computed(() => {
  const keyword = groupSearch.value.trim().toLowerCase()
  if (!keyword) return groups.value
  return groups.value.filter((group) => {
    return group.name.toLowerCase().includes(keyword) || String(group.platform).toLowerCase().includes(keyword)
  })
})

const inputApiKeyCount = computed(() => parseApiKeys(configForm.api_keys_text).length)

const blockedKeywordList = computed(() => parseBlockedKeywords(configForm.blocked_keywords_text))

const legacyBlockedKeywordCount = computed(() => blockedKeywordList.value.length)

const keywordRuleList = computed(() => normalizeKeywordRules(configForm.keyword_rules))

const keywordRuleCount = computed(() => keywordRuleList.value.length)

const enabledKeywordRuleCount = computed(() => keywordRuleList.value.filter((rule) => rule.enabled).length)

const pendingDeletedApiKeyCount = computed(() => pendingDeleteApiKeyHashes.value.length)

const effectiveStoredApiKeyCount = computed(() => Math.max(0, configForm.api_key_count - pendingDeletedApiKeyCount.value))

const apiKeysPlaceholder = computed(() => (
  configForm.api_keys_mode === 'replace'
    ? t('admin.riskControl.apiKeysPlaceholderReplace')
    : t('admin.riskControl.apiKeysPlaceholder')
))

const apiKeysModeHint = computed(() => (
  configForm.api_keys_mode === 'replace'
    ? t('admin.riskControl.apiKeysModeReplaceHint')
    : t('admin.riskControl.apiKeysModeAppendHint')
))

const hasModerationAuditInput = computed(() => {
  return moderationTestPrompt.value.trim() !== '' || moderationTestImages.value.length > 0
})

const isFlaggedHashInputValid = computed(() => /^[a-fA-F0-9]{64}$/.test(flaggedHashInput.value.trim()))

const storedApiKeyTestButtonText = computed(() => {
  if (apiKeyTesting.value) return t('admin.riskControl.testingApiKeys')
  if (hasModerationAuditInput.value) return t('admin.riskControl.testContentWithStoredApiKey')
  return t('admin.riskControl.testStoredApiKeys')
})

const savedApiKeyRows = computed<ContentModerationAPIKeyStatus[]>(() => {
  const rows = status.value?.api_key_statuses?.length
    ? status.value.api_key_statuses
    : configForm.api_key_statuses
  return Array.isArray(rows) ? rows : []
})

const apiKeyRows = computed<ContentModerationAPIKeyStatus[]>(() => [
  ...savedApiKeyRows.value,
  ...testedApiKeyStatuses.value,
])

const visibleApiKeyRows = computed<ContentModerationAPIKeyStatus[]>(() => {
  if (apiKeyRowsExpanded.value) return apiKeyRows.value
  return apiKeyRows.value.slice(0, maxVisibleApiKeyRows)
})

const hiddenApiKeyRowCount = computed<number>(() => Math.max(0, apiKeyRows.value.length - visibleApiKeyRows.value.length))

const canToggleApiKeyRows = computed<boolean>(() => apiKeyRows.value.length > maxVisibleApiKeyRows)

const activeSavedApiKeyRows = computed<ContentModerationAPIKeyStatus[]>(() => (
  savedApiKeyRows.value.filter((row) => !isStoredApiKeyPendingDelete(row))
))

const apiKeyHealthBadges = computed<Array<{ status: ContentModerationAPIKeyStatus['status']; count: number }>>(() => {
  const counts: Record<ContentModerationAPIKeyStatus['status'], number> = {
    ok: 0,
    error: 0,
    frozen: 0,
    unknown: 0,
  }
  for (const row of activeSavedApiKeyRows.value) {
    counts[row.status] = (counts[row.status] ?? 0) + 1
  }
  if (activeSavedApiKeyRows.value.length === 0 && effectiveStoredApiKeyCount.value > 0) {
    counts.unknown = effectiveStoredApiKeyCount.value
  }
  return (['ok', 'frozen', 'error', 'unknown'] as Array<ContentModerationAPIKeyStatus['status']>)
    .map((item) => ({ status: item, count: counts[item] }))
    .filter((item) => item.count > 0)
})

const apiKeyHealthSummary = computed(() => {
  if (!configForm.api_key_configured) return ''
  if (apiKeyHealthBadges.value.length === 0) return t('admin.riskControl.apiKeyStatusUnknown')
  return apiKeyHealthBadges.value
    .map((badge) => `${apiKeyStatusLabel(badge.status)} ${badge.count}`)
    .join(' · ')
})

const protectionStatusTone = computed<ProtectionStatusTone>(() => {
  if (!status.value?.effective_protection) return 'unknown'
  return status.value.effective_protection.effective_blocking ? 'strong' : 'unsafe'
})

const protectionStatusBadge = computed(() => {
  if (protectionStatusTone.value === 'strong') return t('admin.riskControl.protectionStrong')
  if (protectionStatusTone.value === 'unsafe') return t('admin.riskControl.protectionUnsafe')
  return t('admin.riskControl.protectionUnknown')
})

const protectionStatusTitle = computed(() => t('admin.riskControl.protectionTitle'))

const protectionStatusDescription = computed(() => {
  if (protectionStatusTone.value === 'strong') return t('admin.riskControl.protectionStrongDescription')
  if (protectionStatusTone.value === 'unsafe') return t('admin.riskControl.protectionUnsafeDescription')
  return t('admin.riskControl.protectionUnknownDescription')
})

const protectionStatusCardClass = computed(() => {
  if (protectionStatusTone.value === 'strong') return 'border-emerald-200 bg-emerald-50 dark:border-emerald-900/40 dark:bg-emerald-900/15'
  if (protectionStatusTone.value === 'unsafe') return 'border-rose-200 bg-rose-50 dark:border-rose-900/40 dark:bg-rose-900/15'
  return 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-800'
})

const protectionStatusIconClass = computed(() => {
  if (protectionStatusTone.value === 'strong') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-200'
  if (protectionStatusTone.value === 'unsafe') return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-200'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
})

const protectionStatusTitleClass = computed(() => {
  if (protectionStatusTone.value === 'strong') return 'text-emerald-900 dark:text-emerald-100'
  if (protectionStatusTone.value === 'unsafe') return 'text-rose-900 dark:text-rose-100'
  return 'text-gray-900 dark:text-white'
})

const protectionStatusDescriptionClass = computed(() => {
  if (protectionStatusTone.value === 'strong') return 'text-emerald-700 dark:text-emerald-200'
  if (protectionStatusTone.value === 'unsafe') return 'text-rose-700 dark:text-rose-200'
  return 'text-gray-600 dark:text-gray-300'
})

const protectionStatusBadgeClass = computed(() => {
  if (protectionStatusTone.value === 'strong') return 'bg-emerald-600 text-white'
  if (protectionStatusTone.value === 'unsafe') return 'bg-rose-600 text-white'
  return 'bg-gray-600 text-white'
})

const protectionUnsafeReasonLabels = computed(() => (
  (status.value?.effective_protection?.unsafe_reasons ?? [])
    .slice(0, 8)
    .map((reason) => protectionUnsafeReasonLabel(reason))
))

const protectionBuildCommit = computed(() => status.value?.build?.commit || '-')

const protectionBaselineText = computed(() => {
  const baseline = status.value?.security_baseline
  if (!baseline) return '-'
  const state = baseline.baseline_satisfied ? t('admin.riskControl.protectionSatisfied') : t('admin.riskControl.protectionUnsatisfied')
  return `${state} · ${baseline.baseline_satisfaction_method || '-'}`
})

const protectionExternalAPIText = computed(() => {
  const effective = status.value?.effective_protection
  if (!effective) return '-'
  if (effective.engine_mode === 'rule_only') return t('admin.riskControl.protectionExternalLocalAudit')
  if (effective.engine_mode === 'hybrid' && !effective.external_api_configured) {
    return t('admin.riskControl.protectionExternalOptionalNotConfigured')
  }
  if (!effective.external_api_configured) return t('admin.riskControl.protectionExternalNotConfigured')
  const state = effective.external_api_healthy ? t('admin.riskControl.protectionHealthy') : t('admin.riskControl.protectionUnhealthy')
  if (effective.engine_mode === 'hybrid') return `${t('admin.riskControl.protectionExternalOptional')} · ${state} · ${formatNumber(effective.external_api_usable_key_count)}`
  return `${state} · ${formatNumber(effective.external_api_usable_key_count)}`
})

const protectionRouteCoverageText = computed(() => {
  const coverage = status.value?.route_coverage
  if (!coverage) return '-'
  return `${coverage.status || '-'} · ${formatNumber(coverage.covered_routes)}/${formatNumber(coverage.required_routes)}`
})

const protectionPipelineCoverageText = computed(() => {
  const groups = pipelineCoverageGroups.value
  if (!groups.length) return '-'
  const covered = groups.reduce((total, coverage) => total + coverage.covered_routes, 0)
  const required = groups.reduce((total, coverage) => total + coverage.required_routes, 0)
  return `${status.value?.pipeline_coverage?.status || '-'} · ${formatNumber(covered)}/${formatNumber(required)}`
})

const pipelineCoverageGroups = computed<ContentModerationPipelineGroupCoverageStatus[]>(() => {
  const coverage = status.value?.pipeline_coverage
  return [
    coverage?.openai_http,
    coverage?.openai_websocket,
    coverage?.gateway_pre_forward,
  ].filter((group): group is ContentModerationPipelineGroupCoverageStatus => Boolean(group))
})

const pipelineCoverageMatrixVisible = computed(() => (
  pipelineCoverageGroups.value.some((coverage) => coverage.required_routes > 0) ||
  (status.value?.pipeline_execution?.total_count ?? 0) > 0
))

const pipelineCoverageRequiredRouteCount = computed(() => (
  pipelineCoverageGroups.value.reduce((total, coverage) => total + coverage.required_routes, 0)
))

const pipelineCoverageCoveredRouteCount = computed(() => (
  pipelineCoverageGroups.value.reduce((total, coverage) => total + coverage.covered_routes, 0)
))

const pipelineCoverageUncoveredRouteCount = computed(() => (
  Math.max(0, pipelineCoverageRequiredRouteCount.value - pipelineCoverageCoveredRouteCount.value)
))

const pipelineCoverageVersionText = computed(() => {
  const coverage = status.value?.pipeline_coverage
  return coverage?.version || '-'
})

const pipelineCoverageManifestVersionText = computed(() => status.value?.pipeline_coverage?.manifest_version || '-')

const pipelineCoverageManifestHashText = computed(() => status.value?.pipeline_coverage?.manifest_hash || '-')

const pipelineCoverageStatusText = computed(() => status.value?.pipeline_coverage?.status || '-')

const pipelineCoverageStatusClass = computed(() => {
  if (pipelineCoverageStatusText.value === 'covered') {
    return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-200'
  }
  if (pipelineCoverageStatusText.value === 'mismatch') {
    return 'bg-rose-50 text-rose-700 dark:bg-rose-900/20 dark:text-rose-200'
  }
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
})

const pipelineStageRows = computed<ContentModerationPipelineStageCoverageStatus[]>(() => (
  Array.from(pipelineCoverageGroups.value.reduce((byStage, coverage) => {
    for (const stage of coverage.stage_coverage ?? []) {
      const key = stage.stage
      const existing = byStage.get(key) ?? {
        stage: key,
        required_routes: 0,
        covered_routes: 0,
        uncovered_routes: [],
      }
      existing.required_routes += stage.required_routes
      existing.covered_routes += stage.covered_routes
      existing.uncovered_routes = [...existing.uncovered_routes, ...(stage.uncovered_routes ?? [])]
      byStage.set(key, existing)
    }
    return byStage
  }, new Map<string, ContentModerationPipelineStageCoverageStatus>()).values())
    .map((stage) => ({
      ...stage,
      uncovered_routes: [...new Set(stage.uncovered_routes)].sort(),
    }))
    .sort((a, b) => pipelineStageSortKey(a.stage).localeCompare(pipelineStageSortKey(b.stage)))
))

const pipelineRouteRows = computed<ContentModerationPipelineRouteCoverageStatus[]>(() => (
  pipelineCoverageGroups.value.flatMap((coverage) => coverage.routes ?? []).sort((a, b) => {
    const left = `${a.method} ${a.path} ${a.protocol}`
    const right = `${b.method} ${b.path} ${b.protocol}`
    return left.localeCompare(right)
  })
))

function formatRouteForwardAdapters(route: ContentModerationPipelineRouteCoverageStatus): string {
  const descriptors = route.stage_adapter_descriptors ?? route.forward_adapter_descriptors ?? []
  if (descriptors.length > 0) {
    return descriptors
      .map((adapter) => {
        const name = adapter.name || '-'
        const pipeline = adapter.pipeline ? `@${adapter.pipeline}` : ''
        const stage = adapter.stage ? `${adapter.stage}:` : ''
        return `${stage}${name}${pipeline}`
      })
      .join(', ')
  }
  return route.forward_adapters?.join(', ') ?? ''
}

const pipelineExecutionTotalCount = computed(() => status.value?.pipeline_execution?.total_count ?? 0)
const pipelineExecutionRecentCount = computed(() => status.value?.pipeline_execution?.recent_window_count ?? 0)
const pipelineExecutionErrorCount = computed(() => status.value?.pipeline_execution?.error_count ?? 0)
const pipelineExecutionRecentErrorCount = computed(() => status.value?.pipeline_execution?.recent_window_error_count ?? 0)
const pipelineExecutionObservationCoverage = computed(() => status.value?.pipeline_execution?.stage_observation_coverage)
const pipelineExecutionObservationCoverageText = computed(() => {
  const coverage = pipelineExecutionObservationCoverage.value
  if (!coverage) return '-'
  return `${formatNumber(coverage.observed_stages)}/${formatNumber(coverage.expected_stages)}`
})
const pipelineExecutionObservationCoverageClass = computed(() => {
  const statusText = pipelineExecutionObservationCoverage.value?.status
  if (statusText === 'covered') return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-200'
  if (statusText === 'mismatch') return 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200'
  return 'bg-white text-gray-700 dark:bg-dark-800 dark:text-gray-200'
})
const pipelineExecutionUnobservedStageRows = computed(() => (
  [...(pipelineExecutionObservationCoverage.value?.unobserved_stages ?? [])].sort()
))

const pipelineExecutionUnobservedCount = computed(() => pipelineExecutionUnobservedStageRows.value.length)

const pipelineCoverageNeedsAttention = computed(() => (
  pipelineCoverageStatusText.value === 'mismatch' ||
  pipelineCoverageUncoveredRouteCount.value > 0
))

const pipelineObservationNeedsAttention = computed(() => (
  pipelineExecutionObservationCoverage.value?.status === 'mismatch' ||
  pipelineExecutionUnobservedCount.value > 0
))

const pipelineOperatorTone = computed<'ok' | 'warning' | 'danger' | 'idle'>(() => {
  if (!pipelineCoverageMatrixVisible.value) return 'idle'
  if (protectionStatusTone.value === 'unsafe' || pipelineCoverageNeedsAttention.value) return 'danger'
  if (pipelineExecutionErrorCount.value > 0 || pipelineObservationNeedsAttention.value) return 'warning'
  if (pipelineExecutionTotalCount.value === 0) return 'idle'
  return 'ok'
})

const pipelineOperatorIconClass = computed(() => {
  if (pipelineOperatorTone.value === 'ok') return 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-300'
  if (pipelineOperatorTone.value === 'danger') return 'bg-rose-50 text-rose-600 dark:bg-rose-900/20 dark:text-rose-300'
  if (pipelineOperatorTone.value === 'warning') return 'bg-amber-50 text-amber-600 dark:bg-amber-900/20 dark:text-amber-300'
  return 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-300'
})

const pipelineOperatorBadgeClass = computed(() => {
  if (pipelineOperatorTone.value === 'ok') return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-200'
  if (pipelineOperatorTone.value === 'danger') return 'bg-rose-50 text-rose-700 dark:bg-rose-900/20 dark:text-rose-200'
  if (pipelineOperatorTone.value === 'warning') return 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
})

const pipelineOperatorBadgeText = computed(() => {
  if (pipelineOperatorTone.value === 'ok') return t('admin.riskControl.protectionChainNormal')
  if (pipelineOperatorTone.value === 'danger') return t('admin.riskControl.protectionChainNeedsAttention')
  if (pipelineOperatorTone.value === 'warning') return t('admin.riskControl.protectionChainHasWarnings')
  return t('admin.riskControl.protectionChainWaiting')
})

const pipelineOperatorDescription = computed(() => {
  if (pipelineOperatorTone.value === 'ok') return t('admin.riskControl.protectionChainNormalDescription')
  if (pipelineOperatorTone.value === 'danger') return t('admin.riskControl.protectionChainNeedsAttentionDescription')
  if (pipelineOperatorTone.value === 'warning') return t('admin.riskControl.protectionChainHasWarningsDescription')
  return t('admin.riskControl.protectionChainWaitingDescription')
})

const pipelineCoverageMetaText = computed(() => {
  if (pipelineCoverageRequiredRouteCount.value === 0) return t('admin.riskControl.protectionChainCoverageMetaUnknown')
  if (pipelineCoverageUncoveredRouteCount.value > 0) {
    return t('admin.riskControl.protectionChainCoverageMetaMissing', { count: formatNumber(pipelineCoverageUncoveredRouteCount.value) })
  }
  return t('admin.riskControl.protectionChainCoverageMetaOk')
})

const pipelineRecentTrafficMetaText = computed(() => (
  t('admin.riskControl.protectionChainRecentTrafficMeta', { count: formatNumber(pipelineExecutionTotalCount.value) })
))

const pipelineErrorsMetaText = computed(() => {
  if (pipelineExecutionErrorCount.value === 0) return t('admin.riskControl.protectionChainNoErrors')
  return t('admin.riskControl.protectionChainErrorsMeta', {
    recent: formatNumber(pipelineExecutionRecentErrorCount.value),
    total: formatNumber(pipelineExecutionErrorCount.value),
  })
})

const pipelineObservedMetaText = computed(() => {
  if (!pipelineExecutionObservationCoverage.value) return t('admin.riskControl.protectionChainObservedChecksWaiting')
  if (pipelineExecutionUnobservedCount.value > 0) {
    return t('admin.riskControl.protectionChainObservedChecksMissing', { count: formatNumber(pipelineExecutionUnobservedCount.value) })
  }
  return t('admin.riskControl.protectionChainObservedChecksOk')
})

const pipelineOperatorSummaryItems = computed<PipelineOperatorSummaryItem[]>(() => [
  {
    key: 'coverage',
    label: t('admin.riskControl.protectionChainCoverage'),
    value: protectionPipelineCoverageText.value,
    meta: pipelineCoverageMetaText.value,
    icon: 'shield',
    iconClass: pipelineCoverageNeedsAttention.value
      ? 'bg-rose-50 text-rose-600 dark:bg-rose-900/20 dark:text-rose-300'
      : 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-300',
  },
  {
    key: 'traffic',
    label: t('admin.riskControl.protectionChainRecentTraffic'),
    value: formatNumber(pipelineExecutionRecentCount.value),
    meta: pipelineRecentTrafficMetaText.value,
    icon: 'refresh',
    iconClass: 'bg-sky-50 text-sky-600 dark:bg-sky-900/20 dark:text-sky-300',
  },
  {
    key: 'errors',
    label: t('admin.riskControl.protectionChainErrors'),
    value: formatNumber(pipelineExecutionErrorCount.value),
    meta: pipelineErrorsMetaText.value,
    icon: 'document',
    iconClass: pipelineExecutionErrorCount.value > 0
      ? 'bg-rose-50 text-rose-600 dark:bg-rose-900/20 dark:text-rose-300'
      : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-300',
    valueClass: pipelineExecutionErrorCount.value > 0 ? 'text-rose-700 dark:text-rose-300' : undefined,
  },
  {
    key: 'observed',
    label: t('admin.riskControl.protectionChainObservedChecks'),
    value: pipelineExecutionObservationCoverageText.value,
    meta: pipelineObservedMetaText.value,
    icon: 'filter',
    iconClass: pipelineObservationNeedsAttention.value
      ? 'bg-amber-50 text-amber-600 dark:bg-amber-900/20 dark:text-amber-300'
      : 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-300',
  },
])

const pipelineExecutionRouteRows = computed(() => (
  [...(status.value?.pipeline_execution?.routes ?? [])].sort((a, b) => {
    const left = `${a.pipeline} ${a.method ?? ''} ${a.path ?? ''} ${a.protocol ?? ''} ${a.handler ?? ''}`
    const right = `${b.pipeline} ${b.method ?? ''} ${b.path ?? ''} ${b.protocol ?? ''} ${b.handler ?? ''}`
    return left.localeCompare(right)
  })
))

const pipelineExecutionRows = computed(() => (
  [...(status.value?.pipeline_execution?.executions ?? [])].sort((a, b) => {
    const stageOrder = pipelineStageSortKey(a.stage).localeCompare(pipelineStageSortKey(b.stage))
    if (stageOrder !== 0) return stageOrder
    const left = `${a.pipeline} ${a.source} ${a.method ?? ''} ${a.path ?? ''}`
    const right = `${b.pipeline} ${b.source} ${b.method ?? ''} ${b.path ?? ''}`
    return left.localeCompare(right)
  })
))

const overviewItems = computed<OverviewItem[]>(() => [
  {
    key: 'status',
    label: t('admin.riskControl.overview.status'),
    value: configForm.enabled ? t('admin.riskControl.overview.enabled') : t('admin.riskControl.overview.disabled'),
    meta: modeLabel(configForm.mode),
    icon: 'shield',
    iconClass: configForm.enabled
      ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-300'
      : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400',
    badge: runtimeBadgeText.value,
    badgeClass: runtimeBadgeClass.value,
  },
  {
    key: 'api-key',
    label: t('admin.riskControl.overview.apiKey'),
    value: configForm.api_key_configured ? t('admin.riskControl.apiKeyCount', { count: configForm.api_key_count }) : t('admin.riskControl.notConfigured'),
    meta: configForm.api_key_configured ? apiKeyHealthSummary.value || configForm.model || '-' : configForm.model || '-',
    icon: 'key',
    iconClass: 'bg-sky-50 text-sky-600 dark:bg-sky-900/20 dark:text-sky-300',
  },
  {
    key: 'scope',
    label: t('admin.riskControl.overview.groupScope'),
    value: configForm.all_groups ? t('admin.riskControl.allGroups') : selectedGroupCount.value,
    meta: modelFilterSummary.value,
    icon: 'users',
    iconClass: 'bg-violet-50 text-violet-600 dark:bg-violet-900/20 dark:text-violet-300',
  },
  {
    key: 'logs',
    label: t('admin.riskControl.overview.logs'),
    value: formatNumber(pagination.total),
    meta: t('admin.riskControl.overview.currentFilter'),
    icon: 'document',
    iconClass: 'bg-amber-50 text-amber-600 dark:bg-amber-900/20 dark:text-amber-300',
  },
])

const moderationScoreRows = computed<ModerationScoreRow[]>(() => {
  const result = moderationTestResult.value
  if (!result) return []
  return Object.entries(result.category_scores || {})
    .map(([category, score]) => {
      const threshold = result.thresholds?.[category] ?? 1
      return {
        category,
        score,
        threshold,
        hit: score >= threshold,
      }
    })
    .sort((a, b) => b.score - a.score)
})

const riskThresholdRows = computed<RiskThresholdRow[]>(() => (
  riskThresholdCategories.map((category) => ({
    category,
    value: configForm.thresholds[category] ?? riskThresholdDefaults[category],
    defaultValue: riskThresholdDefaults[category],
  }))
))

const inputDetailText = computed(() => {
  if (!inputDetailRow.value) return '-'
  return inputDetailRow.value.input_excerpt || inputDetailRow.value.error || '-'
})

const queueUsagePercent = computed(() => `${Math.min(100, Math.max(0, status.value?.queue_usage_percent ?? 0)).toFixed(1)}%`)

const queueUsageStyle = computed(() => ({
  width: queueUsagePercent.value,
}))

const runtimeMode = computed<ModerationMode>(() => status.value?.mode ?? configForm.mode)

const showPreBlockRuntimeCard = computed(() => runtimeMode.value === 'pre_block')

const showWorkerRuntimeCard = computed(() => runtimeMode.value === 'observe')

const preBlockMetricItems = computed(() => [
  {
    key: 'active',
    label: t('admin.riskControl.preBlockActive'),
    value: formatNumber(status.value?.pre_block_active ?? 0),
    meta: t('admin.riskControl.preBlockActiveHint'),
    class: 'bg-sky-50 dark:bg-sky-900/10',
    valueClass: 'text-sky-700 dark:text-sky-300',
  },
  {
    key: 'checked',
    label: t('admin.riskControl.preBlockChecked'),
    value: formatNumber(status.value?.pre_block_checked ?? 0),
    meta: t('admin.riskControl.preBlockCheckedHint'),
    class: 'bg-gray-50 dark:bg-dark-700/50',
    valueClass: 'text-gray-900 dark:text-white',
  },
  {
    key: 'allowed',
    label: t('admin.riskControl.preBlockAllowed'),
    value: formatNumber(status.value?.pre_block_allowed ?? 0),
    meta: t('admin.riskControl.preBlockAllowedHint'),
    class: 'bg-emerald-50 dark:bg-emerald-900/10',
    valueClass: 'text-emerald-700 dark:text-emerald-300',
  },
  {
    key: 'blocked',
    label: t('admin.riskControl.preBlockBlocked'),
    value: formatNumber(status.value?.pre_block_blocked ?? 0),
    meta: t('admin.riskControl.preBlockBlockedHint'),
    class: 'bg-rose-50 dark:bg-rose-900/10',
    valueClass: 'text-rose-700 dark:text-rose-300',
  },
  {
    key: 'errors',
    label: t('admin.riskControl.preBlockErrors'),
    value: formatNumber(status.value?.pre_block_errors ?? 0),
    meta: t('admin.riskControl.preBlockErrorsHint'),
    class: 'bg-amber-50 dark:bg-amber-900/10',
    valueClass: 'text-amber-700 dark:text-amber-300',
  },
  {
    key: 'latency',
    label: t('admin.riskControl.preBlockAvgLatency'),
    value: `${formatNumber(status.value?.pre_block_avg_latency_ms ?? 0)} ms`,
    meta: t('admin.riskControl.preBlockAvgLatencyHint'),
    class: 'bg-violet-50 dark:bg-violet-900/10',
    valueClass: 'text-violet-700 dark:text-violet-300',
  },
])

const preBlockAPIKeyLoads = computed<ContentModerationAPIKeyLoad[]>(() => (
  [...(status.value?.pre_block_api_key_loads ?? [])].sort((a, b) => a.index - b.index)
))

const preBlockAPIKeyMaxTotal = computed(() => Math.max(1, ...preBlockAPIKeyLoads.value.map((item) => item.total || 0)))

const preBlockAPIKeyLoadSummaryText = computed(() => t('admin.riskControl.preBlockAPIKeyLoadSummary', {
  active: formatNumber(status.value?.pre_block_api_key_active ?? 0),
  available: formatNumber(status.value?.pre_block_api_key_available_count ?? 0),
  total: formatNumber(status.value?.pre_block_api_key_total_calls ?? 0),
  workerActive: formatNumber(status.value?.active_workers ?? 0),
  workerTotal: formatNumber(status.value?.worker_count ?? configForm.worker_count),
}))

function preBlockAPIKeyLoadWidth(total: number): string {
  return `${Math.min(100, Math.max(0, (total / preBlockAPIKeyMaxTotal.value) * 100)).toFixed(1)}%`
}

const workerSlots = computed(() => {
  const total = Math.max(0, status.value?.worker_count ?? configForm.worker_count)
  const active = Math.max(0, status.value?.active_workers ?? 0)
  const enabled = Boolean(status.value?.risk_control_enabled && status.value?.enabled && status.value?.mode !== 'off')
  return Array.from({ length: total }, (_, index) => ({
    id: index + 1,
    state: (!enabled ? 'disabled' : index < active ? 'active' : 'idle') as WorkerSlotState,
    label: !enabled
      ? t('admin.riskControl.workerDisabled')
      : index < active
        ? t('admin.riskControl.workerActive')
        : t('admin.riskControl.workerIdle'),
  }))
})

const runtimeBadgeText = computed(() => {
  if (!status.value?.risk_control_enabled) return t('admin.riskControl.riskSwitchOff')
  if (!configForm.enabled || configForm.mode === 'off') return t('admin.riskControl.overview.disabled')
  return t('admin.riskControl.overview.enabled')
})

const runtimeBadgeClass = computed(() => {
  if (!status.value?.risk_control_enabled || !configForm.enabled || configForm.mode === 'off') {
    return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
  return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'
})

function applyConfig(config: ContentModerationConfig) {
  configForm.enabled = config.enabled
  configForm.mode = config.mode
  configForm.base_url = config.base_url || 'https://api.openai.com'
  configForm.model = config.model || 'omni-moderation-latest'
  configForm.api_keys_text = ''
  configForm.api_key_configured = config.api_key_configured
  configForm.api_key_masked = config.api_key_masked || ''
  configForm.api_key_count = config.api_key_count || 0
  configForm.api_key_masks = Array.isArray(config.api_key_masks) ? [...config.api_key_masks] : []
  configForm.api_key_statuses = Array.isArray(config.api_key_statuses) ? [...config.api_key_statuses] : []
  configForm.api_keys_mode = 'append'
  configForm.clear_api_key = false
  pendingDeleteApiKeyHashes.value = []
  testedApiKeyStatuses.value = []
  apiKeyRowsExpanded.value = false
  configForm.timeout_ms = config.timeout_ms || 3000
  configForm.retry_count = config.retry_count ?? 2
  configForm.sample_rate = config.sample_rate ?? 100
  configForm.all_groups = config.all_groups
  configForm.group_ids = Array.isArray(config.group_ids) ? [...config.group_ids] : []
  configForm.record_non_hits = config.record_non_hits
  configForm.audit_scope = normalizeAuditScope(config.audit_scope)
  configForm.store_input_excerpt = config.store_input_excerpt ?? true
  configForm.search_input_excerpt = config.search_input_excerpt ?? false
  configForm.worker_count = config.worker_count || 4
  configForm.queue_size = config.queue_size || 32768
  configForm.block_status = config.block_status || 403
  configForm.block_message = config.block_message || defaultBlockMessage()
  configForm.email_on_hit = config.email_on_hit ?? true
  configForm.auto_ban_enabled = config.auto_ban_enabled ?? true
  configForm.cyber_policy_exclude_from_ban_count = config.cyber_policy_exclude_from_ban_count ?? false
  configForm.ban_threshold = config.ban_threshold || 10
  configForm.violation_window_hours = config.violation_window_hours || 720
  configForm.hit_retention_days = config.hit_retention_days || 180
  configForm.non_hit_retention_days = Math.min(Math.max(config.non_hit_retention_days || 3, 1), 3)
  configForm.pre_hash_check_enabled = config.pre_hash_check_enabled ?? false
  configForm.thresholds = riskThresholdsFromConfig(config.thresholds)
  configForm.blocked_keywords_text = Array.isArray(config.blocked_keywords) ? config.blocked_keywords.join('\n') : ''
  configForm.keyword_rules = normalizeKeywordRules(config.keyword_rules)
  configForm.keyword_blocking_mode = normalizeKeywordBlockingMode(config.keyword_blocking_mode)
  const modelFilter = normalizeModelFilter(config.model_filter)
  configForm.model_filter_type = modelFilter.type
  configForm.model_filter_models = modelFilter.models
}

async function loadAll() {
  loading.value = true
  try {
    const [config, groupItems, runtimeStatus] = await Promise.all([
      adminAPI.riskControl.getConfig(),
      adminAPI.groups.getAll(),
      adminAPI.riskControl.getStatus(),
    ])
    applyConfig(config)
    groups.value = groupItems
    status.value = runtimeStatus
    if (Array.isArray(runtimeStatus.api_key_statuses)) {
      configForm.api_key_statuses = [...runtimeStatus.api_key_statuses]
      prunePendingDeleteAPIKeyHashes()
    }
    await loadLogs()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function loadStatus(silent = true) {
  statusLoading.value = true
  try {
    const runtimeStatus = await adminAPI.riskControl.getStatus()
    status.value = runtimeStatus
    if (Array.isArray(runtimeStatus.api_key_statuses)) {
      configForm.api_key_statuses = [...runtimeStatus.api_key_statuses]
      prunePendingDeleteAPIKeyHashes()
    }
  } catch (err: unknown) {
    if (!silent) {
      appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.statusFailed')))
    }
  } finally {
    statusLoading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  try {
    const modelFilterPayload = buildModelFilterPayload()
    if (modelFilterPayload.type !== 'all' && modelFilterPayload.models.length === 0) {
      appStore.showError(t('admin.riskControl.modelFilterModelsRequired'))
      return
    }
    const payload: UpdateContentModerationConfig = {
      enabled: configForm.enabled,
      mode: configForm.mode,
      base_url: configForm.base_url,
      model: configForm.model,
      timeout_ms: Number(configForm.timeout_ms) || 3000,
      retry_count: Number(configForm.retry_count) || 0,
      sample_rate: Number(configForm.sample_rate) || 0,
      all_groups: configForm.all_groups,
      group_ids: configForm.all_groups ? [] : [...configForm.group_ids],
      record_non_hits: configForm.record_non_hits,
      audit_scope: configForm.audit_scope,
      store_input_excerpt: configForm.store_input_excerpt,
      search_input_excerpt: configForm.search_input_excerpt,
      clear_api_key: configForm.clear_api_key,
      worker_count: Number(configForm.worker_count) || 4,
      queue_size: Number(configForm.queue_size) || 32768,
      block_status: Number(configForm.block_status) || 403,
      block_message: configForm.block_message || defaultBlockMessage(),
      email_on_hit: configForm.email_on_hit,
      auto_ban_enabled: configForm.auto_ban_enabled,
      cyber_policy_exclude_from_ban_count: configForm.cyber_policy_exclude_from_ban_count,
      ban_threshold: Number(configForm.ban_threshold) || 10,
      violation_window_hours: Number(configForm.violation_window_hours) || 720,
      hit_retention_days: Number(configForm.hit_retention_days) || 180,
      non_hit_retention_days: Math.min(Math.max(Number(configForm.non_hit_retention_days) || 3, 1), 3),
      pre_hash_check_enabled: configForm.pre_hash_check_enabled,
      thresholds: buildRiskThresholdPayload(),
      blocked_keywords: blockedKeywordList.value,
      keyword_rules: keywordRuleList.value,
      keyword_blocking_mode: configForm.keyword_blocking_mode,
      model_filter: modelFilterPayload,
    }
    const keys = parseApiKeys(configForm.api_keys_text)
    if (!payload.clear_api_key && configForm.api_keys_mode === 'replace' && keys.length === 0) {
      appStore.showError(t('admin.riskControl.apiKeysReplaceNoInput'))
      return
    }
    if (keys.length > 0) {
      payload.api_keys = keys
      payload.api_keys_mode = configForm.api_keys_mode
      payload.clear_api_key = false
    }
    if (!payload.clear_api_key && configForm.api_keys_mode !== 'replace' && pendingDeleteApiKeyHashes.value.length > 0) {
      payload.delete_api_key_hashes = [...pendingDeleteApiKeyHashes.value]
    }

    const updated = await adminAPI.riskControl.updateConfig(payload)
    applyConfig(updated)
    settingsOpen.value = false
    appStore.showSuccess(t('admin.riskControl.saved'))
    await Promise.all([loadStatus(true), loadLogs()])
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function loadLogs() {
  logsLoading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.page_size,
      result: filters.result || undefined,
      review_status: filters.review_status || undefined,
      group_id: filters.group_id || undefined,
      endpoint: filters.endpoint || undefined,
      search: filters.search || undefined,
      from: normalizeDateTimeLocal(filters.from),
      to: normalizeDateTimeLocal(filters.to),
    }
    const result = await adminAPI.riskControl.listLogs(params)
    logs.value = result.items
    pagination.total = result.total
    pagination.page = result.page
    pagination.page_size = result.page_size
    pagination.pages = result.pages
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.logsFailed')))
  } finally {
    logsLoading.value = false
  }
}

function canUnbanRow(row: ContentModerationLog): boolean {
  return Boolean(row.auto_banned && row.user_id && row.user_status === 'disabled')
}

function inputSummaryText(row: ContentModerationLog): string {
  return row.input_excerpt || row.error || '-'
}

function openInputDetail(row: ContentModerationLog) {
  inputDetailRow.value = row
}

function closeInputDetail() {
  inputDetailRow.value = null
}

async function unbanUser(row: ContentModerationLog) {
  if (!row.user_id || unbanningUserID.value !== null) return
  unbanningUserID.value = row.user_id
  try {
    const result = await adminAPI.riskControl.unbanUser(row.user_id)
    logs.value = logs.value.map((item) => {
      if (item.user_id !== row.user_id) return item
      return { ...item, user_status: result.status }
    })
    appStore.showSuccess(t('admin.riskControl.unbanSuccess'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.unbanFailed')))
  } finally {
    unbanningUserID.value = null
  }
}

async function reviewLog(row: ContentModerationLog, status: 'false_positive' | 'confirmed_violation') {
  if (reviewingLogID.value !== null) return
  reviewingLogID.value = row.id
  try {
    const reviewed = await adminAPI.riskControl.reviewLog(row.id, {
      status,
      note: status === 'false_positive'
        ? t('admin.riskControl.defaultFalsePositiveNote')
        : t('admin.riskControl.defaultConfirmedViolationNote'),
    })
    logs.value = logs.value.map((item) => (item.id === reviewed.id ? reviewed : item))
    if (inputDetailRow.value?.id === reviewed.id) {
      inputDetailRow.value = reviewed
    }
    appStore.showSuccess(t('admin.riskControl.reviewSaved'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.reviewFailed')))
  } finally {
    reviewingLogID.value = null
  }
}

async function deleteFlaggedHash() {
  if (!isFlaggedHashInputValid.value || hashActionLoading.value) return
  hashActionLoading.value = true
  try {
    const result = await adminAPI.riskControl.deleteFlaggedHash(flaggedHashInput.value)
    flaggedHashInput.value = ''
    await loadStatus(true)
    appStore.showSuccess(result.deleted ? t('admin.riskControl.flaggedHashDeleted') : t('admin.riskControl.flaggedHashNotFound'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.flaggedHashDeleteFailed')))
  } finally {
    hashActionLoading.value = false
  }
}

async function clearFlaggedHashes() {
  if (hashActionLoading.value) return
  const confirmed = window.confirm(t('admin.riskControl.clearFlaggedHashesConfirm'))
  if (!confirmed) return
  hashActionLoading.value = true
  try {
    const result = await adminAPI.riskControl.clearFlaggedHashes()
    await loadStatus(true)
    appStore.showSuccess(t('admin.riskControl.flaggedHashesCleared', { count: result.deleted }))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.flaggedHashesClearFailed')))
  } finally {
    hashActionLoading.value = false
  }
}

function openSettings() {
  activeSettingsTab.value = 'basic'
  settingsOpen.value = true
}

function reloadLogsFromFirstPage() {
  pagination.page = 1
  void loadLogs()
}

function onPageChange(page: number) {
  pagination.page = page
  void loadLogs()
}

function onPageSizeChange(pageSize: number) {
  pagination.page = 1
  pagination.page_size = pageSize
  void loadLogs()
}

function toggleClearApiKey() {
  configForm.clear_api_key = !configForm.clear_api_key
  if (configForm.clear_api_key) {
    configForm.api_keys_text = ''
    configForm.api_keys_mode = 'append'
    testedApiKeyStatuses.value = []
    pendingDeleteApiKeyHashes.value = []
  }
}

function setAPIKeysMode(mode: APIKeysWriteMode) {
  configForm.api_keys_mode = mode
  if (mode === 'replace') {
    pendingDeleteApiKeyHashes.value = []
  }
}

function setModelFilterType(type: ContentModerationModelFilterType) {
  configForm.model_filter_type = type
  if (type === 'all') {
    configForm.model_filter_models = []
  }
}

async function testApiKeys(useInputKeys: boolean) {
  const keys = useInputKeys ? parseApiKeys(configForm.api_keys_text) : []
  if (useInputKeys && keys.length === 0) {
    appStore.showError(t('admin.riskControl.apiKeyTestNoInput'))
    return
  }
  apiKeyTesting.value = true
  try {
    const result = await adminAPI.riskControl.testAPIKeys({
      api_keys: keys,
      base_url: configForm.base_url,
      model: configForm.model,
      timeout_ms: Number(configForm.timeout_ms) || 3000,
      prompt: moderationTestPrompt.value,
      images: moderationTestImages.value,
    })
    moderationTestResult.value = result.audit_result ?? null
    if (useInputKeys) {
      testedApiKeyStatuses.value = result.items.map((item) => ({ ...item, configured: false }))
    } else {
      mergeConfiguredAPIKeyStatuses(result.items)
      testedApiKeyStatuses.value = []
      await loadStatus(true)
    }
    appStore.showSuccess(t('admin.riskControl.apiKeyTestDone', { count: result.items.length }))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.apiKeyTestFailed')))
  } finally {
    apiKeyTesting.value = false
  }
}

async function runKeywordTest() {
  const prompt = keywordTestPrompt.value.trim()
  if (!prompt || keywordTesting.value) return
  keywordTesting.value = true
  try {
    keywordTestResult.value = await adminAPI.riskControl.testKeywords({ prompt })
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.keywordTestFailed')))
  } finally {
    keywordTesting.value = false
  }
}

function mergeConfiguredAPIKeyStatuses(items: ContentModerationAPIKeyStatus[]) {
  if (!hasModerationAuditInput.value || configForm.api_key_statuses.length === 0) {
    configForm.api_key_statuses = items
    return
  }
  const updates = new Map(items.map((item) => [item.key_hash, item]))
  configForm.api_key_statuses = configForm.api_key_statuses.map((item) => updates.get(item.key_hash) ?? item)
}

function toggleDeleteStoredApiKey(row: ContentModerationAPIKeyStatus) {
  if (!row.configured || !row.key_hash) return
  const index = pendingDeleteApiKeyHashes.value.indexOf(row.key_hash)
  if (index >= 0) {
    pendingDeleteApiKeyHashes.value.splice(index, 1)
    return
  }
  pendingDeleteApiKeyHashes.value.push(row.key_hash)
}

function isStoredApiKeyPendingDelete(row: ContentModerationAPIKeyStatus): boolean {
  return row.configured && row.key_hash !== '' && pendingDeleteApiKeyHashes.value.includes(row.key_hash)
}

function prunePendingDeleteAPIKeyHashes() {
  const currentHashes = new Set(savedApiKeyRows.value.map((row) => row.key_hash).filter(Boolean))
  pendingDeleteApiKeyHashes.value = pendingDeleteApiKeyHashes.value.filter((hash) => currentHashes.has(hash))
}

function clearModerationTestInput() {
  moderationTestPrompt.value = ''
  moderationTestImages.value = []
  moderationTestResult.value = null
}

function removeModerationTestImage(index: number) {
  moderationTestImages.value.splice(index, 1)
}

async function handleModerationImageUpload(event: Event) {
  const input = event.target as HTMLInputElement
  await addModerationTestFiles(input.files)
  input.value = ''
}

async function handleModerationImageDrop(event: DragEvent) {
  await addModerationTestFiles(event.dataTransfer?.files ?? null)
}

async function handleModerationImagePaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.files ?? []).filter((file) => file.type.startsWith('image/'))
  if (files.length === 0) return
  event.preventDefault()
  await addModerationTestFiles(files)
}

async function addModerationTestFiles(files: FileList | File[] | null) {
  if (!files) return
  const items = Array.from(files).filter((file) => file.type.startsWith('image/'))
  for (const file of items) {
    if (moderationTestImages.value.length >= maxModerationTestImages) {
      appStore.showError(t('admin.riskControl.auditTestImageLimit', { count: maxModerationTestImages }))
      return
    }
    if (file.size > maxModerationTestImageSize) {
      appStore.showError(t('admin.riskControl.auditTestImageTooLarge'))
      continue
    }
    try {
      moderationTestImages.value.push(await fileToDataURL(file))
    } catch {
      appStore.showError(t('admin.riskControl.auditTestImageReadFailed'))
    }
  }
}

function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

function toggleGroup(groupID: number) {
  const index = configForm.group_ids.indexOf(groupID)
  if (index >= 0) {
    configForm.group_ids.splice(index, 1)
  } else {
    configForm.group_ids.push(groupID)
  }
}

function isGroupSelected(groupID: number): boolean {
  return configForm.group_ids.includes(groupID)
}

function modeLabel(mode: ModerationMode): string {
  const found = modeOptions.value.find((option) => option.value === mode)
  return found?.label ?? mode
}

function modeDescription(mode: ModerationMode): string {
  const descriptions: Record<ModerationMode, string> = {
    pre_block: t('admin.riskControl.modePreBlockDesc'),
    observe: t('admin.riskControl.modeObserveDesc'),
    off: t('admin.riskControl.modeOffDesc'),
  }
  return descriptions[mode] ?? ''
}

function resultLabel(row: ContentModerationLog): string {
  if (row.action === 'cyber_policy') return t('admin.riskControl.action.cyberPolicy')
  if (row.action === 'keyword_block') return t('admin.riskControl.action.keywordBlock')
  if (row.action === 'keyword_review') return t('admin.riskControl.action.keywordReview')
  if (row.action === 'block') return t('admin.riskControl.action.block')
  if (row.action === 'error' || row.error) return t('admin.riskControl.action.error')
  if (row.flagged) return t('admin.riskControl.result.hit')
  return t('admin.riskControl.result.pass')
}

function resultBadgeClass(row: ContentModerationLog): string {
  if (row.action === 'block' || row.action === 'keyword_block' || row.action === 'cyber_policy') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  if (row.action === 'keyword_review') return 'bg-sky-100 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300'
  if (row.action === 'error' || row.error) return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  if (row.flagged) return 'bg-pink-100 text-pink-700 dark:bg-pink-900/30 dark:text-pink-300'
  return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
}

function keywordActionText(row: Pick<ContentModerationLog, 'keyword_action' | 'effective_keyword_action'>): string {
  const action = row.keyword_action || '-'
  const effective = row.effective_keyword_action || action
  if (!action || action === '-' || effective === action) return action
  return `${action} -> ${effective}`
}

function riskContextLabel(value?: string): string {
  const labels: Record<string, string> = {
    actual_request: t('admin.riskControl.riskContexts.actualRequest'),
    meta_discussion: t('admin.riskControl.riskContexts.metaDiscussion'),
    codex_internal: t('admin.riskControl.riskContexts.codexInternal'),
    educational: t('admin.riskControl.riskContexts.educational'),
    unknown: t('admin.riskControl.riskContexts.unknown'),
  }
  return labels[value || ''] || value || '-'
}

function reviewStatusLabel(value?: string): string {
  const labels: Record<string, string> = {
    pending: t('admin.riskControl.reviewStatus.pending'),
    false_positive: t('admin.riskControl.reviewStatus.falsePositive'),
    confirmed_violation: t('admin.riskControl.reviewStatus.confirmedViolation'),
  }
  return labels[value || ''] || value || '-'
}

function workerSlotClass(state: WorkerSlotState): string {
  if (state === 'active') {
    return 'border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-900/60 dark:bg-sky-900/20 dark:text-sky-300'
  }
  if (state === 'idle') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-900/20 dark:text-emerald-300'
  }
  return 'border-gray-100 bg-white text-gray-400 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-500'
}

function workerDotClass(state: WorkerSlotState): string {
  if (state === 'active') return 'bg-sky-500'
  if (state === 'idle') return 'bg-emerald-500'
  return 'bg-gray-300 dark:bg-dark-500'
}

function pipelineStageLabel(stage: string): string {
  const key = stage.trim().toLowerCase()
  const labels: Record<string, string> = {
    moderation: t('admin.riskControl.pipelineStageModeration'),
    cyber: t('admin.riskControl.pipelineStageCyber'),
    image: t('admin.riskControl.pipelineStageImage'),
    billing: t('admin.riskControl.pipelineStageBilling'),
    routing: t('admin.riskControl.pipelineStageRouting'),
    forward: t('admin.riskControl.pipelineStageForward'),
    usage: t('admin.riskControl.pipelineStageUsage'),
  }
  return labels[key] || stage || '-'
}

function pipelineStageSortKey(stage: string): string {
  switch (stage.trim().toLowerCase()) {
    case 'moderation':
      return '00:moderation'
    case 'cyber':
      return '01:cyber'
    case 'image':
      return '02:image'
    case 'billing':
      return '03:billing'
    case 'routing':
      return '04:routing'
    case 'forward':
      return '05:forward'
    case 'usage':
      return '06:usage'
    default:
      return `99:${stage.trim().toLowerCase()}`
  }
}

function pipelineStageCoverageWidth(stage: ContentModerationPipelineStageCoverageStatus): string {
  if (!stage.required_routes) return '0%'
  return `${Math.min(100, Math.max(0, (stage.covered_routes / stage.required_routes) * 100)).toFixed(1)}%`
}

function pipelineRouteStageClass(covered: ContentModerationPipelineRouteStageCoverageStatus['covered']): string {
  if (covered) {
    return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-200'
  }
  return 'bg-rose-50 text-rose-700 dark:bg-rose-900/20 dark:text-rose-200'
}

function percent(value: number): string {
  if (!Number.isFinite(value)) return '-'
  return `${(value * 100).toFixed(1)}%`
}

function percentWidth(value: number): string {
  if (!Number.isFinite(value)) return '0%'
  return `${Math.min(100, Math.max(0, value * 100)).toFixed(1)}%`
}

function latencyText(value: number | null): string {
  if (value === null || value === undefined) return '-'
  return `${value} ms`
}

function apiKeyRowKey(row: ContentModerationAPIKeyStatus, index: number): string {
  return `${row.configured ? 'saved' : 'test'}-${row.key_hash || index}`
}

function apiKeyStatusLabel(statusValue: ContentModerationAPIKeyStatus['status']): string {
  const labels: Record<ContentModerationAPIKeyStatus['status'], string> = {
    ok: t('admin.riskControl.apiKeyStatusOk'),
    error: t('admin.riskControl.apiKeyStatusError'),
    frozen: t('admin.riskControl.apiKeyStatusFrozen'),
    unknown: t('admin.riskControl.apiKeyStatusUnknown'),
  }
  return labels[statusValue] ?? labels.unknown
}

function protectionUnsafeReasonLabel(reason: string): string {
  const labels: Record<string, string> = {
    risk_control_disabled: t('admin.riskControl.protectionReason.riskControlDisabled'),
    moderation_disabled: t('admin.riskControl.protectionReason.moderationDisabled'),
    mode_not_pre_block: t('admin.riskControl.protectionReason.modeNotPreBlock'),
    audit_scope_not_all_context: t('admin.riskControl.protectionReason.auditScopeNotAllContext'),
    public_fail_open: t('admin.riskControl.protectionReason.publicFailOpen'),
    group_scope_not_all: t('admin.riskControl.protectionReason.groupScopeNotAll'),
    model_filter_not_all: t('admin.riskControl.protectionReason.modelFilterNotAll'),
    external_api_not_configured: t('admin.riskControl.protectionReason.externalAPINotConfigured'),
    external_api_no_usable_key: t('admin.riskControl.protectionReason.externalAPINoUsableKey'),
    external_api_all_keys_frozen: t('admin.riskControl.protectionReason.externalAPIAllKeysFrozen'),
    external_api_health_unknown: t('admin.riskControl.protectionReason.externalAPIHealthUnknown'),
    external_api_last_test_failed: t('admin.riskControl.protectionReason.externalAPILastTestFailed'),
    high_risk_rules_not_blocking: t('admin.riskControl.protectionReason.highRiskRulesNotBlocking'),
    rule_only_without_blocking_rules: t('admin.riskControl.protectionReason.ruleOnlyWithoutBlockingRules'),
    no_deterministic_high_risk_policy: t('admin.riskControl.protectionReason.noDeterministicHighRiskPolicy'),
    api_only_without_healthy_external_api: t('admin.riskControl.protectionReason.apiOnlyWithoutHealthyExternalAPI'),
    hybrid_external_api_unhealthy: t('admin.riskControl.protectionReason.hybridExternalAPIUnhealthy'),
    build_commit_unknown: t('admin.riskControl.protectionReason.buildCommitUnknown'),
    build_commit_placeholder: t('admin.riskControl.protectionReason.buildCommitPlaceholder'),
    build_commit_invalid: t('admin.riskControl.protectionReason.buildCommitInvalid'),
    build_attestation_without_valid_commit: t('admin.riskControl.protectionReason.buildAttestationWithoutValidCommit'),
    build_baseline_unverified: t('admin.riskControl.protectionReason.buildBaselineUnverified'),
    build_below_security_baseline: t('admin.riskControl.protectionReason.buildBelowSecurityBaseline'),
    route_coverage_unknown: t('admin.riskControl.protectionReason.routeCoverageUnknown'),
    route_manifest_mismatch: t('admin.riskControl.protectionReason.routeManifestMismatch'),
    uncovered_upstream_routes: t('admin.riskControl.protectionReason.uncoveredUpstreamRoutes'),
    pipeline_coverage_unknown: t('admin.riskControl.protectionReason.pipelineCoverageUnknown'),
    pipeline_coverage_mismatch: t('admin.riskControl.protectionReason.pipelineCoverageMismatch'),
    uncovered_pipeline_routes: t('admin.riskControl.protectionReason.uncoveredPipelineRoutes'),
  }
  return labels[reason] ?? reason
}

function apiKeyStatusBadgeClass(statusValue: ContentModerationAPIKeyStatus['status']): string {
  const classes: Record<ContentModerationAPIKeyStatus['status'], string> = {
    ok: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
    error: 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300',
    frozen: 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300',
    unknown: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
  }
  return classes[statusValue] ?? classes.unknown
}

function apiKeyStatusDotClass(statusValue: ContentModerationAPIKeyStatus['status']): string {
  const classes: Record<ContentModerationAPIKeyStatus['status'], string> = {
    ok: 'bg-emerald-500',
    error: 'bg-amber-500',
    frozen: 'bg-red-500',
    unknown: 'bg-gray-400',
  }
  return classes[statusValue] ?? classes.unknown
}

function apiKeyStatusMeta(row: ContentModerationAPIKeyStatus): string {
  const parts: string[] = []
  parts.push(t('admin.riskControl.apiKeyFailureCount', { count: row.failure_count || 0 }))
  if (row.last_latency_ms > 0) {
    parts.push(t('admin.riskControl.apiKeyLatency', { ms: row.last_latency_ms }))
  }
  if (row.last_http_status > 0) {
    parts.push(t('admin.riskControl.apiKeyHTTPStatus', { status: row.last_http_status }))
  }
  if (row.frozen_until) {
    parts.push(t('admin.riskControl.apiKeyFrozenUntil', { time: formatDateTime(row.frozen_until) }))
  } else if (row.last_checked_at) {
    parts.push(t('admin.riskControl.apiKeyLastChecked', { time: formatDateTime(row.last_checked_at) }))
  } else {
    parts.push(t('admin.riskControl.apiKeyNotTested'))
  }
  return parts.join(' / ')
}

function parseApiKeys(value: string): string[] {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter((item, index, arr) => item && arr.indexOf(item) === index)
}

function normalizeKeywordBlockingMode(value: unknown): KeywordBlockingMode {
  if (value === 'keyword_only' || value === 'api_only' || value === 'keyword_and_api') {
    return value
  }
  return 'keyword_and_api'
}

function normalizeAuditScope(value: unknown): ContentModerationAuditScope {
  if (value === 'user_only' || value === 'user_and_tool' || value === 'all_context') {
    return value
  }
  return 'all_context'
}

function normalizeModelFilter(value: unknown): ContentModerationModelFilter {
  if (!value || typeof value !== 'object') {
    return { type: 'all', models: [] }
  }
  const raw = value as Partial<ContentModerationModelFilter>
  const type = normalizeModelFilterType(raw.type)
  const models = type === 'all' ? [] : normalizeModelNames(raw.models)
  return { type, models }
}

function normalizeModelFilterType(value: unknown): ContentModerationModelFilterType {
  if (value === 'include' || value === 'exclude' || value === 'all') {
    return value
  }
  return 'all'
}

function normalizeModelNames(models: unknown): string[] {
  if (!Array.isArray(models)) return []
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of models) {
    const model = String(item ?? '').trim()
    if (!model) continue
    const key = model.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(model)
  }
  return out
}

function buildModelFilterPayload(): ContentModerationModelFilter {
  const type = normalizeModelFilterType(configForm.model_filter_type)
  if (type === 'all') {
    return { type: 'all', models: [] }
  }
  return {
    type,
    models: normalizeModelNames(configForm.model_filter_models),
  }
}

function riskThresholdsFromConfig(thresholds: Record<string, number> | null | undefined): Record<string, number> {
  const out: Record<string, number> = { ...riskThresholdDefaults }
  for (const category of riskThresholdCategories) {
    const value = thresholds?.[category]
    if (Number.isFinite(value)) {
      out[category] = clampPercent(Number(value) * 100)
    }
  }
  return out
}

function buildRiskThresholdPayload(): Record<string, number> {
  const payload: Record<string, number> = {}
  for (const category of riskThresholdCategories) {
    payload[category] = Number((clampPercent(configForm.thresholds[category]) / 100).toFixed(4))
  }
  return payload
}

function resetRiskThresholds() {
  configForm.thresholds = { ...riskThresholdDefaults }
}

function clampPercent(value: unknown): number {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) {
    return 0
  }
  return Math.min(100, Math.max(0, numeric))
}

function formatThresholdPercent(value: number): string {
  return `${clampPercent(value).toFixed(1)}%`
}

function parseBlockedKeywords(value: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const line of value.split(/\r?\n/)) {
    const kw = line.trim()
    if (!kw) continue
    const key = kw.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(kw)
  }
  return out
}

function normalizeKeywordRules(value: unknown): ContentModerationKeywordRule[] {
  if (!Array.isArray(value)) return []
  const out: ContentModerationKeywordRule[] = []
  for (const item of value) {
    if (!item || typeof item !== 'object') continue
    const raw = item as Partial<ContentModerationKeywordRule>
    const keyword = String(raw.keyword ?? '').trim()
    if (!keyword) continue
    out.push({
      keyword,
      category: String(raw.category ?? 'other').trim() || 'other',
      severity: String(raw.severity ?? 'high').trim() || 'high',
      action: String(raw.action ?? 'block').trim() || 'block',
      enabled: Boolean(raw.enabled),
    })
  }
  return out
}

function violationCountText(row: ContentModerationLog): string {
  if (!row.flagged) return '-'
  if (row.violation_count === 0) return t('admin.riskControl.violationNotCounted')
  return t('admin.riskControl.violationCount', { count: row.violation_count || 1 })
}

function normalizeDateTimeLocal(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined
  return date.toISOString()
}

function formatDateTime(value: string): string {
  return formatDateTimeValue(value) || '-'
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

onMounted(() => {
  void loadAll()
  statusTimer = window.setInterval(() => {
    void loadStatus(true)
  }, 15000)
})

onUnmounted(() => {
  if (statusTimer !== null) {
    window.clearInterval(statusTimer)
    statusTimer = null
  }
})
</script>
