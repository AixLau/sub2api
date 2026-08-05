<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col justify-between gap-4 md:flex-row md:items-start">
        <div>
          <h1 class="text-2xl font-semibold text-content-primary">{{ t('admin.merchant.title') }}</h1>
          <p class="mt-1 max-w-3xl text-sm text-content-secondary">{{ t('admin.merchant.description') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" type="button" :title="t('admin.merchant.refresh')" @click="loadIntegrations">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            <span class="ml-2">{{ t('common.refresh') }}</span>
          </button>
          <button class="btn btn-primary" type="button" @click="startNewIntegration">
            <Icon name="plus" size="md" class="mr-2" />
            {{ t('admin.merchant.create') }}
          </button>
        </div>
      </div>

      <div class="grid min-h-[620px] gap-6 lg:grid-cols-[280px_minmax(0,1fr)]">
        <section class="overflow-hidden rounded-lg border border-line-subtle bg-surface-raised">
          <div class="flex items-center justify-between border-b border-line-subtle px-4 py-3">
            <h2 class="text-sm font-semibold text-content-primary">{{ t('admin.merchant.integration') }}</h2>
            <span class="text-xs text-content-tertiary">{{ integrations.length }}</span>
          </div>
          <div v-if="loading && integrations.length === 0" class="p-4 text-sm text-content-secondary">
            {{ t('common.loading') }}
          </div>
          <div v-else-if="integrations.length === 0" class="p-4 text-sm text-content-secondary">
            {{ t('admin.merchant.noIntegrations') }}
          </div>
          <div v-else class="divide-y divide-line-subtle">
            <button
              v-for="item in integrations"
              :key="item.id"
              type="button"
              class="flex w-full items-start justify-between gap-3 px-4 py-3 text-left transition-colors hover:bg-surface-subtle"
              :class="selectedId === item.id ? 'bg-primary-50 dark:bg-primary-900/20' : ''"
              @click="selectIntegration(item.id)"
            >
              <span class="min-w-0">
                <span class="block truncate text-sm font-medium text-content-primary">{{ item.name }}</span>
                <span class="mt-0.5 block truncate font-mono text-xs text-content-tertiary">{{ item.code }}</span>
              </span>
              <span
                class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium"
                :class="statusClass(item.status)"
              >
                {{ statusLabel(item.status) }}
              </span>
            </button>
          </div>
        </section>

        <section v-if="editingIntegration" class="min-w-0 space-y-6">
          <form class="rounded-lg border border-line-subtle bg-surface-raised p-5" @submit.prevent="saveIntegration">
            <div class="mb-5 flex flex-col justify-between gap-3 border-b border-line-subtle pb-4 sm:flex-row sm:items-center">
              <div>
                <h2 class="text-base font-semibold text-content-primary">{{ t('admin.merchant.integration') }}</h2>
                <p class="mt-1 text-xs text-content-tertiary">{{ t('admin.merchant.readiness') }}</p>
              </div>
              <div class="flex items-center gap-3">
                <span class="text-sm text-content-secondary">{{ t('admin.merchant.fields.enabled') }}</span>
                <Toggle :model-value="integrationForm.enabled === true" @update:model-value="integrationForm.enabled = $event" />
              </div>
            </div>

            <div class="grid gap-4 md:grid-cols-2">
              <label class="field-label">
                <span>{{ t('admin.merchant.fields.name') }}</span>
                <input v-model.trim="integrationForm.name" class="input" required />
              </label>
              <label class="field-label">
                <span>{{ t('admin.merchant.fields.code') }}</span>
                <input v-model.trim="integrationForm.code" class="input font-mono" required />
              </label>
              <label class="field-label">
                <span>{{ t('admin.merchant.fields.merchantCode') }}</span>
                <input v-model.trim="integrationForm.merchant_code" class="input font-mono" required />
              </label>
              <label class="field-label">
                <span>{{ t('admin.merchant.fields.status') }}</span>
                <select v-model="integrationForm.status" class="input">
                  <option value="draft">{{ t('admin.merchant.draft') }}</option>
                  <option value="active">{{ t('admin.merchant.active') }}</option>
                  <option value="disabled">{{ t('admin.merchant.disabled') }}</option>
                </select>
              </label>
              <label class="field-label md:col-span-2">
                <span>{{ t('admin.merchant.fields.description') }}</span>
                <textarea v-model="integrationForm.description" class="input min-h-20 resize-y" />
              </label>
              <label class="field-label md:col-span-2">
                <span>{{ t('admin.merchant.fields.redirectHosts') }}</span>
                <textarea v-model="redirectHostsText" class="input min-h-20 resize-y font-mono text-sm" required />
                <span class="field-hint">{{ t('admin.merchant.fields.redirectHostsHint') }}</span>
              </label>
            </div>

            <div class="mt-5 flex justify-end">
              <button class="btn btn-primary" type="submit" :disabled="savingIntegration">
                <Icon name="check" size="md" class="mr-2" />
                {{ savingIntegration ? t('common.saving') : t('admin.merchant.save') }}
              </button>
            </div>
          </form>

          <section class="rounded-lg border border-line-subtle bg-surface-raised p-5">
            <div class="mb-4 flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
              <div>
                <h2 class="text-base font-semibold text-content-primary">{{ t('admin.merchant.endpoints') }}</h2>
                <p class="mt-1 text-xs text-content-tertiary">{{ t('admin.merchant.endpointCount', { count: selectedIntegration?.endpoints.length ?? 0 }) }}</p>
              </div>
              <button class="btn btn-secondary" type="button" :disabled="!selectedId" @click="startNewEndpoint">
                <Icon name="plus" size="md" class="mr-2" />
                {{ t('admin.merchant.createEndpoint') }}
              </button>
            </div>

            <div v-if="selectedIntegration && selectedIntegration.endpoints.length" class="space-y-2">
              <div
                v-for="endpoint in selectedIntegration.endpoints"
                :key="endpoint.id"
                class="flex flex-col gap-3 rounded-md border border-line-subtle p-3 sm:flex-row sm:items-center sm:justify-between"
              >
                <button type="button" class="min-w-0 text-left" @click="startEditEndpoint(endpoint)">
                  <span class="flex items-center gap-2">
                    <span class="font-medium text-content-primary">{{ endpointTypeLabel(endpoint.type) }}</span>
                    <span class="rounded bg-surface-subtle px-1.5 py-0.5 font-mono text-[11px] text-content-tertiary">{{ endpoint.method }}</span>
                    <span v-if="endpoint.enabled" class="text-xs text-success-600 dark:text-success-400">{{ t('admin.merchant.enabled') }}</span>
                    <span v-else class="text-xs text-content-tertiary">{{ t('admin.merchant.disabledLabel') }}</span>
                  </span>
                  <span class="mt-1 block max-w-[52rem] truncate font-mono text-xs text-content-tertiary">{{ endpoint.url }}</span>
                </button>
                <div class="flex shrink-0 items-center gap-2">
                  <button class="btn btn-secondary px-2" type="button" :title="t('admin.merchant.test')" :disabled="testingEndpointId === endpoint.id" @click="testEndpoint(endpoint)">
                    <Icon name="play" size="sm" :class="testingEndpointId === endpoint.id ? 'animate-pulse' : ''" />
                    <span class="ml-1.5 hidden sm:inline">{{ testingEndpointId === endpoint.id ? t('admin.merchant.testing') : t('admin.merchant.test') }}</span>
                  </button>
                  <Toggle :model-value="endpoint.enabled" @update:model-value="toggleEndpoint(endpoint, $event)" />
                </div>
              </div>
            </div>
            <p v-else class="text-sm text-content-secondary">{{ t('admin.merchant.noIntegrations') }}</p>

            <form v-if="endpointEditing" class="mt-5 border-t border-line-subtle pt-5" @submit.prevent="saveEndpoint">
              <div class="mb-4 flex items-center justify-between">
                <h3 class="text-sm font-semibold text-content-primary">
                  {{ endpointForm.id ? t('admin.merchant.editEndpoint') : t('admin.merchant.newEndpoint') }}
                </h3>
                <button class="btn btn-secondary px-2" type="button" :title="t('common.close')" @click="closeEndpointForm">
                  <Icon name="x" size="sm" />
                </button>
              </div>

              <div class="grid gap-4 md:grid-cols-2">
                <label class="field-label">
                  <span>{{ t('admin.merchant.fields.type') }}</span>
                  <select v-model="endpointForm.type" class="input" required>
                    <option v-for="option in endpointTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </label>
                <label class="field-label">
                  <span>{{ t('admin.merchant.fields.method') }}</span>
                  <select v-model="endpointForm.method" class="input">
                    <option v-for="method in methods" :key="method" :value="method">{{ t(`admin.merchant.methodOptions.${method}`) }}</option>
                  </select>
                </label>
                <label class="field-label md:col-span-2">
                  <span>{{ t('admin.merchant.fields.url') }}</span>
                  <input v-model.trim="endpointForm.url" class="input font-mono text-sm" required />
                </label>
                <label class="field-label">
                  <span>{{ t('admin.merchant.fields.contentType') }}</span>
                  <select v-model="endpointForm.content_type" class="input">
                    <option value="application/json">application/json</option>
                    <option value="application/x-www-form-urlencoded">application/x-www-form-urlencoded</option>
                  </select>
                </label>
                <label class="field-label">
                  <span>{{ t('admin.merchant.fields.authType') }}</span>
                  <select v-model="endpointForm.auth_type" class="input">
                    <option v-for="auth in authTypes" :key="auth" :value="auth">{{ t(`admin.merchant.authTypes.${auth}`) }}</option>
                  </select>
                </label>
                <label class="field-label">
                  <span>{{ t('admin.merchant.fields.secretRef') }}</span>
                  <input v-model.trim="endpointForm.secret_ref" class="input font-mono text-sm" :placeholder="t('admin.merchant.fields.secretRef')" />
                  <span class="field-hint">{{ t('admin.merchant.fields.secretRefHint') }}</span>
                </label>
                <label class="field-label">
                  <span>{{ t('admin.merchant.fields.timeout') }}</span>
                  <input v-model.number="endpointForm.timeout_ms" class="input" type="number" min="100" max="120000" step="100" />
                </label>
              </div>

              <p class="mt-4 rounded-md bg-primary-50 px-3 py-2 text-xs text-primary-800 dark:bg-primary-900/20 dark:text-primary-200">
                {{ t('admin.merchant.fields.templateHint') }}
              </p>

              <div class="mt-4 grid gap-5 lg:grid-cols-3">
                <div class="field-label">
                  <div class="flex items-center justify-between gap-2">
                    <span>{{ t('admin.merchant.fields.queryTemplate') }}</span>
                    <button class="btn btn-secondary px-2" type="button" :title="t('admin.merchant.fields.addField')" @click="addTemplateRow(endpointForm.query_rows)">
                      <Icon name="plus" size="sm" />
                    </button>
                  </div>
                  <div class="space-y-2">
                    <div v-for="(row, index) in endpointForm.query_rows" :key="`query-${index}`" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2">
                      <input v-model.trim="row.key" class="input min-w-0 font-mono text-xs" :placeholder="t('admin.merchant.fields.keyPlaceholder')" />
                      <input v-model="row.value" class="input min-w-0 font-mono text-xs" :placeholder="t('admin.merchant.fields.valuePlaceholder')" />
                      <button class="btn btn-secondary px-2" type="button" :title="t('admin.merchant.fields.removeField')" @click="removeTemplateRow(endpointForm.query_rows, index)">
                        <Icon name="x" size="sm" />
                      </button>
                    </div>
                    <p v-if="endpointForm.query_rows.length === 0" class="field-hint">{{ t('admin.merchant.fields.noFields') }}</p>
                  </div>
                </div>
                <div class="field-label">
                  <div class="flex items-center justify-between gap-2">
                    <span>{{ t('admin.merchant.fields.headerTemplate') }}</span>
                    <button class="btn btn-secondary px-2" type="button" :title="t('admin.merchant.fields.addField')" @click="addTemplateRow(endpointForm.header_rows)">
                      <Icon name="plus" size="sm" />
                    </button>
                  </div>
                  <div class="space-y-2">
                    <div v-for="(row, index) in endpointForm.header_rows" :key="`header-${index}`" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2">
                      <input v-model.trim="row.key" class="input min-w-0 font-mono text-xs" :placeholder="t('admin.merchant.fields.keyPlaceholder')" />
                      <input v-model="row.value" class="input min-w-0 font-mono text-xs" :placeholder="t('admin.merchant.fields.valuePlaceholder')" />
                      <button class="btn btn-secondary px-2" type="button" :title="t('admin.merchant.fields.removeField')" @click="removeTemplateRow(endpointForm.header_rows, index)">
                        <Icon name="x" size="sm" />
                      </button>
                    </div>
                    <p v-if="endpointForm.header_rows.length === 0" class="field-hint">{{ t('admin.merchant.fields.noFields') }}</p>
                  </div>
                </div>
                <div class="field-label">
                  <div class="flex items-center justify-between gap-2">
                    <span>{{ t('admin.merchant.fields.bodyTemplate') }}</span>
                    <button class="btn btn-secondary px-2" type="button" :title="t('admin.merchant.fields.addField')" @click="addTemplateRow(endpointForm.body_rows)">
                      <Icon name="plus" size="sm" />
                    </button>
                  </div>
                  <div class="space-y-2">
                    <div v-for="(row, index) in endpointForm.body_rows" :key="`body-${index}`" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2">
                      <input v-model.trim="row.key" class="input min-w-0 font-mono text-xs" :placeholder="t('admin.merchant.fields.keyPlaceholder')" />
                      <input v-model="row.value" class="input min-w-0 font-mono text-xs" :placeholder="t('admin.merchant.fields.valuePlaceholder')" />
                      <button class="btn btn-secondary px-2" type="button" :title="t('admin.merchant.fields.removeField')" @click="removeTemplateRow(endpointForm.body_rows, index)">
                        <Icon name="x" size="sm" />
                      </button>
                    </div>
                    <p v-if="endpointForm.body_rows.length === 0" class="field-hint">{{ t('admin.merchant.fields.noFields') }}</p>
                  </div>
                </div>
              </div>

              <section class="mt-5 border-t border-line-subtle pt-5">
                <h4 class="text-sm font-semibold text-content-primary">{{ t('admin.merchant.fields.responseMapping') }}</h4>
                <div class="mt-3 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                  <label class="field-label"><span>{{ t('admin.merchant.fields.successPath') }}</span><input v-model.trim="endpointForm.mapping.success" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.externalUserIdPath') }}</span><input v-model.trim="endpointForm.mapping.external_user_id" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.externalAccountPath') }}</span><input v-model.trim="endpointForm.mapping.external_account" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.redirectUrlPath') }}</span><input v-model.trim="endpointForm.mapping.redirect_url" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.loginTokenPath') }}</span><input v-model.trim="endpointForm.mapping.login_token" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.errorCodePath') }}</span><input v-model.trim="endpointForm.mapping.error_code" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.errorMessagePath') }}</span><input v-model.trim="endpointForm.mapping.error_message" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.recordsPath') }}</span><input v-model.trim="endpointForm.mapping.records_path" class="input font-mono text-xs" /></label>
                </div>

                <h4 class="mt-5 text-sm font-semibold text-content-primary">{{ t('admin.merchant.fields.successRule') }}</h4>
                <div class="mt-3 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                  <label class="field-label"><span>{{ t('admin.merchant.fields.successPath') }}</span><input v-model.trim="endpointForm.success_rule.success.path" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.ruleOperator') }}</span><select v-model="endpointForm.success_rule.success.operator" class="input"><option v-for="operator in ruleOperators" :key="`success-${operator}`" :value="operator">{{ t(`admin.merchant.ruleOperators.${operator}`) }}</option></select></label>
                  <label v-if="ruleNeedsValue(endpointForm.success_rule.success.operator)" class="field-label">
                    <span>{{ ruleUsesList(endpointForm.success_rule.success.operator) ? t('admin.merchant.fields.ruleValues') : t('admin.merchant.fields.ruleValue') }}</span>
                    <input v-if="ruleUsesList(endpointForm.success_rule.success.operator)" v-model="endpointForm.success_rule.success.values" class="input font-mono text-xs" :placeholder="t('admin.merchant.fields.ruleValuesHint')" />
                    <input v-else v-model="endpointForm.success_rule.success.value" class="input font-mono text-xs" />
                  </label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.failurePath') }}</span><input v-model.trim="endpointForm.success_rule.failure.path" class="input font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.ruleOperator') }}</span><select v-model="endpointForm.success_rule.failure.operator" class="input"><option v-for="operator in ruleOperators" :key="`failure-${operator}`" :value="operator">{{ t(`admin.merchant.ruleOperators.${operator}`) }}</option></select></label>
                  <label v-if="ruleNeedsValue(endpointForm.success_rule.failure.operator)" class="field-label">
                    <span>{{ ruleUsesList(endpointForm.success_rule.failure.operator) ? t('admin.merchant.fields.ruleValues') : t('admin.merchant.fields.ruleValue') }}</span>
                    <input v-if="ruleUsesList(endpointForm.success_rule.failure.operator)" v-model="endpointForm.success_rule.failure.values" class="input font-mono text-xs" :placeholder="t('admin.merchant.fields.ruleValuesHint')" />
                    <input v-else v-model="endpointForm.success_rule.failure.value" class="input font-mono text-xs" />
                  </label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.httpMin') }}</span><input v-model.number="endpointForm.success_rule.http_min" class="input" type="number" min="100" max="599" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.httpMax') }}</span><input v-model.number="endpointForm.success_rule.http_max" class="input" type="number" min="100" max="599" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.unmatched') }}</span><select v-model="endpointForm.success_rule.unmatched" class="input"><option value="http">{{ t('admin.merchant.fields.unmatchedHttp') }}</option><option value="success">{{ t('admin.merchant.fields.unmatchedSuccess') }}</option><option value="failure">{{ t('admin.merchant.fields.unmatchedFailure') }}</option></select></label>
                  <label class="flex items-center gap-3 text-sm text-content-secondary md:col-span-2"><Toggle v-model="endpointForm.success_rule.require_http_success" /><span>{{ t('admin.merchant.fields.requireHttpSuccess') }}</span></label>
                </div>

                <h4 class="mt-5 text-sm font-semibold text-content-primary">{{ t('admin.merchant.fields.retryPolicy') }}</h4>
                <div class="mt-3 grid gap-4 md:grid-cols-2">
                  <label class="field-label"><span>{{ t('admin.merchant.fields.retryMaxAttempts') }}</span><input v-model.number="endpointForm.retry_max_attempts" class="input" type="number" min="1" max="5" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.retryBackoffMs') }}</span><input v-model.number="endpointForm.retry_backoff_ms" class="input" type="number" min="0" max="60000" step="100" /></label>
                </div>
              </section>

              <details class="mt-5 rounded-md border border-line-subtle p-3">
                <summary class="cursor-pointer text-sm font-medium text-content-primary">{{ t('admin.merchant.fields.advanced') }}</summary>
                <p class="mt-2 text-xs text-content-tertiary">{{ t('admin.merchant.advancedHint') }}</p>
                <div class="mt-4 grid gap-4 lg:grid-cols-3">
                  <label class="field-label"><span>{{ t('admin.merchant.fields.queryTemplateAdvanced') }}</span><textarea v-model="endpointForm.advanced_query_template" class="input min-h-32 resize-y font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.headerTemplateAdvanced') }}</span><textarea v-model="endpointForm.advanced_header_template" class="input min-h-32 resize-y font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.bodyTemplateAdvanced') }}</span><textarea v-model="endpointForm.advanced_body_template" class="input min-h-32 resize-y font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.responseMappingAdvanced') }}</span><textarea v-model="endpointForm.advanced_response_mapping" class="input min-h-32 resize-y font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.successRuleAdvanced') }}</span><textarea v-model="endpointForm.advanced_success_rule" class="input min-h-32 resize-y font-mono text-xs" /></label>
                  <label class="field-label"><span>{{ t('admin.merchant.fields.retryPolicyAdvanced') }}</span><textarea v-model="endpointForm.advanced_retry_policy" class="input min-h-32 resize-y font-mono text-xs" /></label>
                </div>
              </details>

              <div class="mt-5 flex justify-end gap-2">
                <button class="btn btn-secondary" type="button" @click="closeEndpointForm">{{ t('common.cancel') }}</button>
                <button class="btn btn-primary" type="submit" :disabled="savingEndpoint">
                  <Icon name="check" size="md" class="mr-2" />
                  {{ savingEndpoint ? t('common.saving') : t('admin.merchant.saveEndpoint') }}
                </button>
              </div>
            </form>
          </section>
        </section>

        <section v-else class="flex min-h-[420px] items-center justify-center rounded-lg border border-dashed border-line-subtle bg-surface-raised p-8 text-center">
          <div class="max-w-md">
            <Icon name="globe" size="xl" class="mx-auto text-content-tertiary" />
            <p class="mt-4 text-sm text-content-secondary">{{ t('admin.merchant.selectIntegration') }}</p>
          </div>
        </section>
      </div>
    </div>

    <BaseDialog v-if="testResult" :show="true" :title="t('admin.merchant.testResult')" width="wide" @close="testResult = null">
      <div class="grid gap-4 sm:grid-cols-3">
        <div class="rounded-md bg-surface-subtle p-3">
          <div class="text-xs text-content-tertiary">{{ t('admin.merchant.httpStatus') }}</div>
          <div class="mt-1 text-lg font-semibold" :class="testResult.successful ? 'text-success-600' : 'text-danger-600'">{{ testResult.http_status }}</div>
        </div>
        <div class="rounded-md bg-surface-subtle p-3 sm:col-span-2">
          <div class="text-xs text-content-tertiary">{{ t('admin.merchant.responseMessage') }}</div>
          <div class="mt-1 break-words text-sm text-content-primary">{{ testResult.message || '-' }}</div>
        </div>
      </div>
      <div v-if="testResult.redirect_url" class="mt-4 rounded-md bg-surface-subtle p-3">
        <div class="text-xs text-content-tertiary">{{ t('admin.merchant.responseRedirect') }}</div>
        <code class="mt-1 block break-all text-xs text-content-primary">{{ testResult.redirect_url }}</code>
      </div>
      <template #footer>
        <button class="btn btn-secondary" type="button" @click="testResult = null">{{ t('common.close') }}</button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type {
  MerchantAPIEndpoint,
  MerchantEndpointInput,
  MerchantEndpointType,
  MerchantIntegration,
  MerchantIntegrationInput,
  MerchantTestResult
} from '@/api/admin/merchantIntegrations'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const integrations = ref<MerchantIntegration[]>([])
const selectedId = ref<number | null>(null)
const creatingIntegration = ref(false)
const endpointEditing = ref(false)
const loading = ref(false)
const savingIntegration = ref(false)
const savingEndpoint = ref(false)
const testingEndpointId = ref<number | null>(null)
const testResult = ref<MerchantTestResult | null>(null)

const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'] as const
const authTypes = ['none', 'api_key', 'bearer', 'basic', 'hmac'] as const
const ruleOperators = ['equals', 'in', 'not_in', 'exists', 'not_exists', 'truthy', 'falsy'] as const
type RuleOperator = typeof ruleOperators[number]
const endpointTypes: MerchantEndpointType[] = [
  'register_login',
  'register',
  'login',
  'token',
  'sync',
  'bind',
  'status',
  'callback',
  'recharge_records'
]

const integrationForm = reactive<MerchantIntegrationInput>(emptyIntegration())
const endpointForm = reactive<EndpointForm>(emptyEndpoint())
const redirectHostsText = ref('')

interface TemplateRow {
  key: string
  value: string
}

interface RuleForm {
  path: string
  operator: RuleOperator
  value: string
  values: string
}

interface ResponseMappingForm {
  success: string
  external_user_id: string
  external_account: string
  redirect_url: string
  login_token: string
  error_code: string
  error_message: string
  records_path: string
}

interface SuccessRuleForm {
  success: RuleForm
  failure: RuleForm
  http_min: number
  http_max: number
  unmatched: 'http' | 'success' | 'failure'
  require_http_success: boolean
}

interface EndpointForm {
  id: number | null
  type: MerchantEndpointType
  url: string
  method: string
  content_type: string
  auth_type: MerchantEndpointInput['auth_type']
  secret_ref: string
  timeout_ms: number
  query_rows: TemplateRow[]
  header_rows: TemplateRow[]
  body_rows: TemplateRow[]
  mapping: ResponseMappingForm
  success_rule: SuccessRuleForm
  retry_max_attempts: number
  retry_backoff_ms: number
  advanced_query_template: string
  advanced_header_template: string
  advanced_body_template: string
  advanced_response_mapping: string
  advanced_success_rule: string
  advanced_retry_policy: string
  status: 'draft' | 'active' | 'disabled'
  enabled: boolean
}

function emptyIntegration(): MerchantIntegrationInput {
  return {
    name: '',
    code: '',
    mode: 'dynamic_api',
    merchant_code: '',
    description: '',
    status: 'draft',
    enabled: false,
    redirect_hosts: []
  }
}

function emptyRule(): RuleForm {
  return { path: '', operator: 'truthy', value: '', values: '' }
}

function emptyMapping(): ResponseMappingForm {
  return {
    success: 'success',
    external_user_id: 'data.user_id',
    external_account: 'data.account',
    redirect_url: 'data.redirect_url',
    login_token: 'data.login_token',
    error_code: 'code',
    error_message: 'message',
    records_path: 'data.records'
  }
}

function emptyEndpoint(): EndpointForm {
  return {
    id: null,
    type: 'register_login',
    url: '',
    method: 'POST',
    content_type: 'application/json',
    auth_type: 'none',
    secret_ref: '',
    timeout_ms: 10000,
    query_rows: [],
    header_rows: [],
    body_rows: [],
    mapping: emptyMapping(),
    success_rule: {
      success: emptyRule(),
      failure: emptyRule(),
      http_min: 200,
      http_max: 299,
      unmatched: 'http',
      require_http_success: true
    },
    retry_max_attempts: 1,
    retry_backoff_ms: 300,
    advanced_query_template: '',
    advanced_header_template: '',
    advanced_body_template: '',
    advanced_response_mapping: '',
    advanced_success_rule: '',
    advanced_retry_policy: '',
    status: 'active',
    enabled: true
  }
}

const selectedIntegration = computed(() => integrations.value.find(item => item.id === selectedId.value) ?? null)
const editingIntegration = computed(() => creatingIntegration.value || selectedIntegration.value !== null)
const endpointTypeOptions = computed(() => endpointTypes.map(value => ({ value, label: endpointTypeLabel(value) })))

function statusLabel(status: string): string {
  return t(`admin.merchant.${status}`)
}

function statusClass(status: string): string {
  if (status === 'active') return 'bg-success-50 text-success-700 dark:bg-success-900/20 dark:text-success-300'
  if (status === 'disabled') return 'bg-surface-subtle text-content-tertiary'
  return 'bg-warning-50 text-warning-700 dark:bg-warning-900/20 dark:text-warning-300'
}

function endpointTypeLabel(type: string): string {
  return t(`admin.merchant.endpointTypes.${type}`)
}

function resetIntegrationForm() {
  Object.assign(integrationForm, emptyIntegration())
  redirectHostsText.value = ''
}

function hydrateIntegrationForm(item: MerchantIntegration) {
  Object.assign(integrationForm, {
    name: item.name,
    code: item.code,
    mode: item.mode,
    merchant_code: item.merchant_code,
    description: item.description,
    status: item.status,
    enabled: item.enabled,
    redirect_hosts: [...item.redirect_hosts]
  })
  redirectHostsText.value = item.redirect_hosts.join('\n')
}

async function loadIntegrations() {
  loading.value = true
  try {
    integrations.value = await adminAPI.merchantIntegrations.list(true)
    if (selectedId.value && integrations.value.some(item => item.id === selectedId.value)) {
      await selectIntegration(selectedId.value, false)
    } else if (integrations.value.length > 0) {
      await selectIntegration(integrations.value[0].id, false)
    } else {
      startNewIntegration()
    }
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.loadError')))
  } finally {
    loading.value = false
  }
}

async function selectIntegration(id: number, clearEndpoint = true) {
  creatingIntegration.value = false
  selectedId.value = id
  if (clearEndpoint) closeEndpointForm()
  const item = integrations.value.find(entry => entry.id === id)
  if (item) hydrateIntegrationForm(item)
  try {
    const fresh = await adminAPI.merchantIntegrations.getById(id)
    const index = integrations.value.findIndex(entry => entry.id === id)
    if (index >= 0) integrations.value[index] = fresh
    hydrateIntegrationForm(fresh)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.loadError')))
  }
}

function startNewIntegration() {
  selectedId.value = null
  creatingIntegration.value = true
  closeEndpointForm()
  resetIntegrationForm()
}

async function saveIntegration() {
  const hosts = redirectHostsText.value.split(/\r?\n|,/).map(value => value.trim()).filter(Boolean)
  integrationForm.redirect_hosts = hosts
  savingIntegration.value = true
  try {
    const saved = selectedId.value
      ? await adminAPI.merchantIntegrations.update(selectedId.value, integrationForm)
      : await adminAPI.merchantIntegrations.create(integrationForm)
    const fresh = await adminAPI.merchantIntegrations.getById(saved.id)
    const index = integrations.value.findIndex(item => item.id === fresh.id)
    if (index >= 0) integrations.value[index] = fresh
    else integrations.value.push(fresh)
    selectedId.value = fresh.id
    creatingIntegration.value = false
    hydrateIntegrationForm(fresh)
    appStore.showSuccess(t('admin.merchant.saved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.saveError')))
  } finally {
    savingIntegration.value = false
  }
}

function startNewEndpoint() {
  Object.assign(endpointForm, emptyEndpoint())
  endpointEditing.value = true
}

function addTemplateRow(rows: TemplateRow[]) {
  rows.push({ key: '', value: '' })
}

function removeTemplateRow(rows: TemplateRow[], index: number) {
  rows.splice(index, 1)
}

function stringifyTemplateValue(value: unknown): string {
  if (typeof value === 'string') return value
  const serialized = JSON.stringify(value)
  return serialized === undefined ? String(value ?? '') : serialized
}

function templateRowsFromObject(value: Record<string, unknown> | undefined): TemplateRow[] {
  return Object.entries(value ?? {}).map(([key, item]) => ({ key, value: stringifyTemplateValue(item) }))
}

function parseTemplateValue(value: string): unknown {
  const trimmed = value.trim()
  if (!trimmed) return ''
  if (trimmed === 'true' || trimmed === 'false' || trimmed === 'null' || /^-?\d+(\.\d+)?$/.test(trimmed) || trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return JSON.parse(trimmed) as unknown
    } catch {
      return value
    }
  }
  return value
}

function templateObjectFromRows(rows: TemplateRow[]): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (key) result[key] = parseTemplateValue(row.value)
  }
  return result
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function pathValue(source: Record<string, unknown>, keys: string[], fallback: string): string {
  for (const key of keys) {
    if (typeof source[key] === 'string' && source[key].trim()) return source[key] as string
  }
  return fallback
}

function numberValue(value: unknown, fallback: number): number {
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function ruleFromObject(value: unknown): RuleForm {
  const source = asRecord(value)
  const candidate = typeof source.operator === 'string' ? source.operator as RuleOperator : 'truthy'
  const operator = ruleOperators.includes(candidate) ? candidate : 'truthy'
  return {
    path: typeof source.path === 'string' ? source.path : '',
    operator,
    value: source.value === undefined || source.value === null ? '' : String(source.value),
    values: Array.isArray(source.values) ? source.values.map(item => String(item)).join(', ') : ''
  }
}

function ruleNeedsValue(operator: RuleOperator): boolean {
  return operator !== 'exists' && operator !== 'not_exists'
}

function ruleUsesList(operator: RuleOperator): boolean {
  return operator === 'in' || operator === 'not_in'
}

function ruleToObject(form: RuleForm): Record<string, unknown> | null {
  const path = form.path.trim()
  if (!path) return null
  const rule: Record<string, unknown> = { path, operator: form.operator }
  if (ruleUsesList(form.operator)) {
    rule.values = form.values.split(',').map(value => value.trim()).filter(Boolean)
  } else if (form.operator === 'equals') {
    rule.value = form.value
  }
  return rule
}

function unknownJSON(source: Record<string, unknown>, knownKeys: string[]): string {
  const known = new Set(knownKeys)
  const unknown = Object.fromEntries(Object.entries(source).filter(([key]) => !known.has(key)))
  return Object.keys(unknown).length > 0 ? JSON.stringify(unknown, null, 2) : ''
}

function startEditEndpoint(endpoint: MerchantAPIEndpoint) {
  const responseMapping = endpoint.response_mapping ?? {}
  const successRule = endpoint.success_rule ?? {}
  const retryPolicy = endpoint.retry_policy ?? {}
  const httpRule = asRecord(successRule.http)
  Object.assign(endpointForm, {
    id: endpoint.id,
    type: endpoint.type,
    url: endpoint.url,
    method: endpoint.method,
    content_type: endpoint.content_type,
    auth_type: endpoint.auth_type,
    secret_ref: endpoint.secret_ref ?? '',
    timeout_ms: endpoint.timeout_ms,
    query_rows: templateRowsFromObject(endpoint.query_template),
    header_rows: templateRowsFromObject(endpoint.header_template),
    body_rows: templateRowsFromObject(endpoint.body_template),
    mapping: {
      success: pathValue(responseMapping, ['success', 'success_path'], 'success'),
      external_user_id: pathValue(responseMapping, ['externalUserId', 'external_user_id'], 'data.user_id'),
      external_account: pathValue(responseMapping, ['externalAccount', 'external_account'], 'data.account'),
      redirect_url: pathValue(responseMapping, ['redirectUrl', 'redirect_url'], 'data.redirect_url'),
      login_token: pathValue(responseMapping, ['loginToken', 'login_token'], 'data.login_token'),
      error_code: pathValue(responseMapping, ['errorCode', 'error_code'], 'code'),
      error_message: pathValue(responseMapping, ['errorMessage', 'error_message'], 'message'),
      records_path: pathValue(responseMapping, ['recordsPath', 'records_path'], 'data.records')
    },
    success_rule: {
      success: ruleFromObject(successRule.success),
      failure: ruleFromObject(successRule.failure),
      http_min: numberValue(httpRule.min, 200),
      http_max: numberValue(httpRule.max, 299),
      unmatched: successRule.unmatched === 'success' || successRule.unmatched === 'failure' ? successRule.unmatched : 'http',
      require_http_success: successRule.requireHttpSuccess !== false
    },
    retry_max_attempts: numberValue(retryPolicy.maxAttempts ?? retryPolicy.max_attempts, 1),
    retry_backoff_ms: numberValue(retryPolicy.backoffMs ?? retryPolicy.backoff_ms, 300),
    advanced_query_template: '',
    advanced_header_template: '',
    advanced_body_template: '',
    advanced_response_mapping: unknownJSON(responseMapping, ['success', 'success_path', 'externalUserId', 'external_user_id', 'externalAccount', 'external_account', 'redirectUrl', 'redirect_url', 'loginToken', 'login_token', 'errorCode', 'error_code', 'errorMessage', 'error_message', 'recordsPath', 'records_path']),
    advanced_success_rule: unknownJSON(successRule, ['http', 'success', 'failure', 'unmatched', 'requireHttpSuccess']),
    advanced_retry_policy: unknownJSON(retryPolicy, ['maxAttempts', 'max_attempts', 'backoffMs', 'backoff_ms']),
    status: endpoint.status,
    enabled: endpoint.enabled
  })
  endpointEditing.value = true
}

function closeEndpointForm() {
  endpointEditing.value = false
  Object.assign(endpointForm, emptyEndpoint())
}

function parseObject(value: string, field: string): Record<string, unknown> | null {
  try {
    const parsed: unknown = JSON.parse(value || '{}')
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('object required')
    return parsed as Record<string, unknown>
  } catch {
    appStore.showError(t('admin.merchant.invalidJSON', { field }))
    return null
  }
}

function parseOptionalObject(value: string, field: string): Record<string, unknown> | null {
  return value.trim() ? parseObject(value, field) : {}
}

function buildResponseMapping(advanced: Record<string, unknown>): Record<string, unknown> {
  const mapping: Record<string, unknown> = {
    success: endpointForm.mapping.success.trim() || 'success',
    externalUserId: endpointForm.mapping.external_user_id.trim() || 'data.user_id',
    externalAccount: endpointForm.mapping.external_account.trim() || 'data.account',
    redirectUrl: endpointForm.mapping.redirect_url.trim() || 'data.redirect_url',
    loginToken: endpointForm.mapping.login_token.trim() || 'data.login_token',
    errorCode: endpointForm.mapping.error_code.trim() || 'code',
    errorMessage: endpointForm.mapping.error_message.trim() || 'message',
    recordsPath: endpointForm.mapping.records_path.trim() || 'data.records'
  }
  Object.assign(mapping, advanced)
  return mapping
}

function buildSuccessRule(advanced: Record<string, unknown>): Record<string, unknown> {
  const form = endpointForm.success_rule
  const rule: Record<string, unknown> = {
    http: {
      min: numberValue(form.http_min, 200),
      max: numberValue(form.http_max, 299)
    },
    unmatched: form.unmatched,
    requireHttpSuccess: form.require_http_success
  }
  const success = ruleToObject(form.success)
  const failure = ruleToObject(form.failure)
  if (success) rule.success = success
  if (failure) rule.failure = failure
  Object.assign(rule, advanced)
  return rule
}

async function saveEndpoint() {
  if (!selectedId.value) return
  const advancedQueryTemplate = parseOptionalObject(endpointForm.advanced_query_template, t('admin.merchant.fields.queryTemplateAdvanced'))
  const advancedHeaderTemplate = parseOptionalObject(endpointForm.advanced_header_template, t('admin.merchant.fields.headerTemplateAdvanced'))
  const advancedBodyTemplate = parseOptionalObject(endpointForm.advanced_body_template, t('admin.merchant.fields.bodyTemplateAdvanced'))
  const advancedResponseMapping = parseOptionalObject(endpointForm.advanced_response_mapping, t('admin.merchant.fields.responseMappingAdvanced'))
  const advancedSuccessRule = parseOptionalObject(endpointForm.advanced_success_rule, t('admin.merchant.fields.successRuleAdvanced'))
  const advancedRetryPolicy = parseOptionalObject(endpointForm.advanced_retry_policy, t('admin.merchant.fields.retryPolicyAdvanced'))
  if (!advancedQueryTemplate || !advancedHeaderTemplate || !advancedBodyTemplate || !advancedResponseMapping || !advancedSuccessRule || !advancedRetryPolicy) return
  const queryTemplate = templateObjectFromRows(endpointForm.query_rows)
  const headerTemplate = templateObjectFromRows(endpointForm.header_rows)
  const bodyTemplate = templateObjectFromRows(endpointForm.body_rows)
  Object.assign(queryTemplate, advancedQueryTemplate)
  Object.assign(headerTemplate, advancedHeaderTemplate)
  Object.assign(bodyTemplate, advancedBodyTemplate)
  const responseMapping = buildResponseMapping(advancedResponseMapping)
  const successRule = buildSuccessRule(advancedSuccessRule)
  const retryPolicy: Record<string, unknown> = {
    maxAttempts: numberValue(endpointForm.retry_max_attempts, 1),
    backoffMs: numberValue(endpointForm.retry_backoff_ms, 300)
  }
  Object.assign(retryPolicy, advancedRetryPolicy)
  const input: MerchantEndpointInput = {
    type: endpointForm.type,
    url: endpointForm.url,
    method: endpointForm.method,
    content_type: endpointForm.content_type,
    auth_type: endpointForm.auth_type,
    secret_ref: endpointForm.secret_ref,
    timeout_ms: endpointForm.timeout_ms,
    query_template: queryTemplate,
    header_template: headerTemplate,
    body_template: bodyTemplate,
    response_mapping: responseMapping,
    success_rule: successRule,
    retry_policy: retryPolicy,
    status: endpointForm.status,
    enabled: endpointForm.enabled
  }
  savingEndpoint.value = true
  try {
    if (endpointForm.id) await adminAPI.merchantIntegrations.updateEndpoint(selectedId.value, endpointForm.id, input)
    else await adminAPI.merchantIntegrations.createEndpoint(selectedId.value, input)
    const fresh = await adminAPI.merchantIntegrations.getById(selectedId.value)
    const index = integrations.value.findIndex(item => item.id === fresh.id)
    if (index >= 0) integrations.value[index] = fresh
    hydrateIntegrationForm(fresh)
    closeEndpointForm()
    appStore.showSuccess(t('admin.merchant.endpointSaved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.endpointError')))
  } finally {
    savingEndpoint.value = false
  }
}

async function toggleEndpoint(endpoint: MerchantAPIEndpoint, enabled: boolean) {
  if (!selectedId.value) return
  try {
    await adminAPI.merchantIntegrations.setEndpointEnabled(selectedId.value, endpoint.id, enabled)
    const fresh = await adminAPI.merchantIntegrations.getById(selectedId.value)
    const index = integrations.value.findIndex(item => item.id === fresh.id)
    if (index >= 0) integrations.value[index] = fresh
    hydrateIntegrationForm(fresh)
    appStore.showSuccess(t('admin.merchant.enabledSaved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.toggleError')))
  }
}

async function testEndpoint(endpoint: MerchantAPIEndpoint) {
  if (!selectedId.value) return
  testingEndpointId.value = endpoint.id
  try {
    testResult.value = await adminAPI.merchantIntegrations.testEndpoint(selectedId.value, endpoint.id)
    appStore.showSuccess(testResult.value.successful ? t('admin.merchant.testPassed') : t('admin.merchant.testFailed'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.merchant.endpointError')))
  } finally {
    testingEndpointId.value = null
  }
}

onMounted(loadIntegrations)
</script>

<style scoped>
.field-label {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  font-size: 0.8125rem;
  font-weight: 500;
  color: rgb(var(--color-content-secondary));
}

.field-hint {
  font-size: 0.6875rem;
  font-weight: 400;
  line-height: 1.4;
  color: rgb(var(--color-content-tertiary));
}
</style>
