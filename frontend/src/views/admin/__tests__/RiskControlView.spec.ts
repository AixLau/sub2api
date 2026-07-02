import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import type { DOMWrapper, VueWrapper } from '@vue/test-utils'

import RiskControlView from '../RiskControlView.vue'
import type { ContentModerationConfig, UpdateContentModerationConfig } from '@/api/admin/riskControl'

const {
  getConfig,
  updateConfig,
  getStatus,
  listLogs,
  testKeywords,
  getGroups,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  getStatus: vi.fn(),
  listLogs: vi.fn(),
  testKeywords: vi.fn(),
  getGroups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    riskControl: {
      getConfig,
      updateConfig,
      getStatus,
      listLogs,
      testKeywords,
      testAPIKeys: vi.fn(),
      deleteFlaggedHash: vi.fn(),
      clearFlaggedHashes: vi.fn(),
      unbanUser: vi.fn(),
    },
    groups: {
      getAll: getGroups,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_err: unknown, fallback: string) => fallback,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.riskControl.preBlockAPIKeyLoadSummary') {
          return `同步并发 ${params?.active} / 可用 Key ${params?.available}，累计 ${params?.total} 次，worker：${params?.workerActive} / ${params?.workerTotal}`
        }
        return key.replace(/\{(\w+)\}/g, (_, token) => String(params?.[token] ?? `{${token}}`))
      },
    }),
  }
})

const baseConfig = (): ContentModerationConfig => ({
  enabled: true,
  mode: 'pre_block',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  api_key_configured: false,
  api_key_masked: '',
  api_key_count: 0,
  api_key_masks: [],
  api_key_statuses: [],
  timeout_ms: 3000,
  sample_rate: 100,
  all_groups: true,
  group_ids: [],
  record_non_hits: false,
  audit_scope: 'all_context',
  store_input_excerpt: true,
  search_input_excerpt: false,
  worker_count: 4,
  queue_size: 32768,
  block_status: 403,
  block_message: '内容审计命中风险规则，请调整输入后重试',
  email_on_hit: true,
  auto_ban_enabled: true,
  ban_threshold: 10,
  violation_window_hours: 720,
  retry_count: 2,
  hit_retention_days: 180,
  non_hit_retention_days: 3,
  pre_hash_check_enabled: false,
  blocked_keywords: [],
  keyword_rules: [],
  keyword_blocking_mode: 'keyword_and_api',
  engine_mode: 'hybrid',
  thresholds: {
    harassment: 0.98,
    sexual: 0.65,
  },
  model_filter: {
    type: 'all',
    models: [],
  },
  cyber_policy_exclude_from_ban_count: false,
})

const runtimeStatus = () => ({
  enabled: true,
  risk_control_enabled: true,
  mode: 'pre_block',
  worker_count: 4,
  max_workers: 32,
  active_workers: 0,
  idle_workers: 4,
  queue_size: 32768,
  queue_length: 0,
  queue_usage_percent: 0,
  enqueued: 0,
  dropped: 0,
  processed: 0,
  errors: 0,
  pre_block_active: 0,
  pre_block_checked: 0,
  pre_block_allowed: 0,
  pre_block_blocked: 0,
  pre_block_errors: 0,
  pre_block_avg_latency_ms: 0,
  pre_block_api_key_active: 0,
  pre_block_api_key_available_count: 0,
  pre_block_api_key_total_calls: 0,
  pre_block_api_key_loads: [],
  api_key_statuses: [],
  flagged_hash_count: 0,
  last_cleanup_deleted_hit: 0,
  last_cleanup_deleted_non_hit: 0,
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const BaseDialogStub = defineComponent({
  props: {
    show: {
      type: Boolean,
      default: false,
    },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})
const ModelWhitelistSelectorStub = defineComponent({
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const onInput = (event: Event) => {
      const value = (event.target as HTMLInputElement).value
      emit(
        'update:modelValue',
        value
          .split(/[,\n]/)
          .map((item) => item.trim())
          .filter(Boolean)
      )
    }
    return () =>
      h('input', {
        'data-test': 'model-filter-input',
        value: (props.modelValue as string[]).join('\n'),
        onInput,
      })
  },
})

function findButtonByText(wrapper: VueWrapper, text: string): DOMWrapper<HTMLButtonElement> {
  const button = wrapper.findAll<HTMLButtonElement>('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`button not found: ${text}`)
  }
  return button
}

describe('admin RiskControlView', () => {
  beforeEach(() => {
    getConfig.mockReset()
    updateConfig.mockReset()
    getStatus.mockReset()
    listLogs.mockReset()
    testKeywords.mockReset()
    getGroups.mockReset()
    showError.mockReset()
    showSuccess.mockReset()

    getConfig.mockResolvedValue(baseConfig())
    getStatus.mockResolvedValue(runtimeStatus())
    listLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    testKeywords.mockResolvedValue({
      matched: false,
      matched_keyword: '',
      keyword_category: '',
      keyword_severity: '',
      keyword_action: '',
      normalized_excerpt: '',
    })
    getGroups.mockResolvedValue([])
    updateConfig.mockImplementation(async (payload: UpdateContentModerationConfig) => ({
      ...baseConfig(),
      ...payload,
      model_filter: payload.model_filter ?? baseConfig().model_filter,
      api_key_configured: false,
      api_key_masked: '',
      api_key_count: 0,
      api_key_masks: [],
      api_key_statuses: [],
    }))
  })

  it('renders keyword metadata in records and input detail', async () => {
    listLogs.mockResolvedValue({
      items: [
        {
          id: 42,
          request_id: 'req-keyword',
          user_id: 7,
          user_email: 'risk@example.com',
          api_key_id: 3,
          api_key_name: 'Team Key',
          group_id: 2,
          group_name: 'Default',
          endpoint: '/v1/responses',
          provider: 'openai',
          model: 'gpt-5',
          mode: 'pre_block',
          action: 'keyword_block',
          flagged: true,
          highest_category: 'keyword',
          highest_score: 1,
          category_scores: {},
          threshold_snapshot: {},
          input_excerpt: 'please sell api key',
          matched_keyword: 'sell api key',
          keyword_category: 'account_abuse',
          keyword_severity: 'critical',
          upstream_latency_ms: null,
          error: '',
          violation_count: 1,
          auto_banned: false,
          email_sent: false,
          user_status: 'active',
          queue_delay_ms: null,
          created_at: '2026-06-19T08:00:00Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.matchedKeyword')
    expect(wrapper.text()).toContain('sell api key')
    expect(wrapper.text()).toContain('account_abuse')
    expect(wrapper.text()).toContain('critical')

    await findButtonByText(wrapper, 'please sell api key').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.keywordMetadata')
    expect(wrapper.text()).toContain('admin.riskControl.keywordCategory')
    expect(wrapper.text()).toContain('admin.riskControl.keywordSeverity')
  })

  it('runs keyword tests without saving config or writing logs', async () => {
    testKeywords.mockResolvedValue({
      matched: true,
      matched_keyword: 'sell api key',
      keyword_category: 'account_abuse',
      keyword_severity: 'critical',
      keyword_action: 'block',
      normalized_excerpt: 'please sell api key now',
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.keywords').trigger('click')
    await wrapper.get('[data-test="keyword-test-prompt"]').setValue('please s e l l api key now')
    await findButtonByText(wrapper, 'admin.riskControl.runKeywordTest').trigger('click')
    await flushPromises()

    expect(testKeywords).toHaveBeenCalledWith({ prompt: 'please s e l l api key now' })
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.riskControl.keywordTestMatched')
    expect(wrapper.text()).toContain('sell api key')
    expect(wrapper.text()).toContain('account_abuse')
    expect(wrapper.text()).toContain('critical')
    expect(wrapper.text()).toContain('block')
    expect(wrapper.text()).toContain('please sell api key now')
  })

  it('saves the selected model filter mode and models', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.modelFilterInclude').trigger('click')
    await wrapper.get('[data-test="model-filter-input"]').setValue('gpt-5.5, gpt-5.4')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      model_filter: {
        type: 'include',
        models: ['gpt-5.5', 'gpt-5.4'],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('submits edited risk control thresholds when saving moderation config', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.riskThresholds').trigger('click')
    await wrapper.get('[data-test="risk-threshold-sexual"]').setValue('72')
    await wrapper.get('[data-test="risk-threshold-harassment"]').setValue('99')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      thresholds: expect.objectContaining({
        sexual: 0.72,
        harassment: 0.99,
      }),
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('describes worker runtime as async audit and pre-block record processing', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      mode: 'observe',
      processed: 12,
      queue_length: 2,
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.workerStatusHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('2 / 32,768')
  })

  it('shows pre-block synchronous moderation metrics separately from worker queue', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      pre_block_active: 2,
      pre_block_checked: 128,
      pre_block_allowed: 120,
      pre_block_blocked: 8,
      pre_block_errors: 1,
      pre_block_avg_latency_ms: 86,
      pre_block_api_key_active: 2,
      pre_block_api_key_available_count: 2,
      pre_block_api_key_total_calls: 128,
      active_workers: 3,
      worker_count: 7,
      pre_block_api_key_loads: [
        {
          index: 0,
          key_hash: 'hash-one',
          masked: 'sk-...one',
          status: 'ok',
          active: 1,
          total: 72,
          success: 70,
          errors: 2,
          avg_latency_ms: 84,
          last_latency_ms: 80,
          last_http_status: 200,
        },
        {
          index: 1,
          key_hash: 'hash-two',
          masked: 'sk-...two',
          status: 'ok',
          active: 1,
          total: 56,
          success: 56,
          errors: 0,
          avg_latency_ms: 90,
          last_latency_ms: 92,
          last_http_status: 200,
        },
      ],
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.workerStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('128')
    expect(wrapper.text()).toContain('120')
    expect(wrapper.text()).toContain('8')
    expect(wrapper.text()).toContain('86 ms')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockAPIKeyLoad')
    expect(wrapper.text()).toContain('sk-...one')
    expect(wrapper.text()).toContain('sk-...two')
    expect(wrapper.text()).toContain('72')
    expect(wrapper.text()).toContain('56')
    expect(wrapper.text()).toContain('同步并发 2 / 可用 Key 2，累计 128 次，worker：3 / 7')

    const runtimeCards = wrapper.get('[data-test="pre-block-runtime-cards"]')
    const syncCard = wrapper.get('[data-test="pre-block-sync-card"]')
    const apiKeyLoadCard = wrapper.get('[data-test="pre-block-api-key-load-card"]')

    expect(runtimeCards.classes()).toEqual(expect.arrayContaining([
      'grid',
      'grid-cols-1',
      'xl:grid-cols-[minmax(0,520px)_minmax(0,1fr)]',
    ]))
    expect(syncCard.element.parentElement).toBe(runtimeCards.element)
    expect(apiKeyLoadCard.element.parentElement).toBe(runtimeCards.element)
    expect(syncCard.classes()).toContain('card')
    expect(apiKeyLoadCard.classes()).toContain('card')
    expect(syncCard.get('h2').text()).toBe('admin.riskControl.preBlockSyncStatus')
    expect(syncCard.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(apiKeyLoadCard.get('h2').text()).toBe('admin.riskControl.preBlockAPIKeyLoad')
    expect(apiKeyLoadCard.text()).toContain('admin.riskControl.preBlockAPIKeyLoadHint')
    expect(wrapper.get('[data-test="pre-block-api-key-load-list"]').classes()).toEqual(expect.arrayContaining([
      'max-h-[280px]',
      'overflow-y-auto',
    ]))
  })

  it('renders OpenAI pipeline manifest metadata and route stage coverage', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      pipeline_coverage: {
        manifest_version: '2026-06-29.2',
        version: 'openai-http-pre-forward-v2',
        manifest_hash: 'stage-hash-123',
        status: 'mismatch',
        openai_http: {
          version: 'openai-http-pre-forward-v2',
          pipeline: 'openai_http',
          required_routes: 2,
          covered_routes: 1,
          uncovered_routes: ['POST /v1/responses'],
          stage_coverage: [
            {
              stage: 'moderation',
              required_routes: 2,
              covered_routes: 2,
              uncovered_routes: [],
            },
            {
              stage: 'cyber',
              required_routes: 2,
              covered_routes: 2,
              uncovered_routes: [],
            },
            {
              stage: 'image',
              required_routes: 1,
              covered_routes: 0,
              uncovered_routes: ['POST /v1/responses'],
            },
          ],
          routes: [
            {
              method: 'POST',
              path: '/v1/chat/completions',
              handler: 'OpenAIGatewayHandler.ChatCompletions',
              protocol: 'openai_chat_completions',
              pipeline: 'openai_http',
              covered: true,
              stages: [
                { stage: 'moderation', required: true, covered: true },
                { stage: 'cyber', required: true, covered: true },
              ],
            },
            {
              method: 'POST',
              path: '/v1/responses',
              handler: 'OpenAIGatewayHandler.Responses',
              protocol: 'openai_responses',
              pipeline: 'openai_http',
              covered: false,
              forward_adapters: ['OpenAIHTTPForwardStage'],
              uncovered_stages: ['image'],
              stages: [
                { stage: 'moderation', required: true, covered: true },
                { stage: 'cyber', required: true, covered: true },
                { stage: 'image', required: true, covered: false },
              ],
            },
          ],
        },
      },
      pipeline_execution: {
        total_count: 7,
        error_count: 2,
        recent_window_seconds: 300,
        recent_window_count: 3,
        recent_window_error_count: 1,
        stage_observation_coverage: {
          status: 'mismatch',
          expected_stages: 7,
          observed_stages: 1,
          unobserved_stages: [
            'POST /v1/responses OpenAIGatewayHandler.Responses moderation',
            'POST /v1/responses OpenAIGatewayHandler.Responses usage',
          ],
        },
        executions: [
          {
            pipeline: 'openai_http',
            stage: 'forward',
            source: 'OpenAIGatewayPipeline.RunHTTPExecutableStage',
            method: 'POST',
            path: '/v1/responses',
            handler: 'OpenAIGatewayHandler.Responses',
            protocol: 'openai_responses',
            count: 7,
            error_count: 2,
            recent_count: 3,
            recent_error_count: 1,
          },
        ],
        routes: [
          {
            pipeline: 'openai_http',
            method: 'POST',
            path: '/v1/responses',
            handler: 'OpenAIGatewayHandler.Responses',
            protocol: 'openai_responses',
            count: 7,
            error_count: 2,
            recent_count: 3,
            recent_error_count: 1,
            stages: [
              {
                pipeline: 'openai_http',
                stage: 'forward',
                source: 'OpenAIGatewayPipeline.RunHTTPExecutableStage',
                method: 'POST',
                path: '/v1/responses',
                handler: 'OpenAIGatewayHandler.Responses',
                protocol: 'openai_responses',
                count: 7,
                error_count: 2,
                recent_count: 3,
                recent_error_count: 1,
              },
            ],
          },
        ],
      },
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    const matrix = wrapper.get('[data-test="pipeline-coverage-matrix"]')
    expect(matrix.text()).toContain('2026-06-29.2')
    expect(matrix.text()).toContain('openai-http-pre-forward-v2')
    expect(matrix.text()).toContain('stage-hash-123')
    expect(matrix.text()).toContain('mismatch')
    expect(matrix.text()).toContain('POST /v1/responses')
    expect(matrix.text()).toContain('openai_http')
    expect(matrix.text()).toContain('moderation')
    expect(matrix.text()).toContain('cyber')
    expect(matrix.text()).toContain('image')
    expect(matrix.text()).toContain('openai_responses')
    expect(matrix.text()).toContain('admin.riskControl.pipelineExecutionRoutes')
    expect(matrix.text()).toContain('admin.riskControl.pipelineExecutionObservedStages')
    expect(matrix.text()).toContain('1/7')
    expect(matrix.text()).toContain('POST /v1/responses OpenAIGatewayHandler.Responses moderation')
    expect(matrix.text()).toContain('POST /v1/responses OpenAIGatewayHandler.Responses usage')
    expect(matrix.text()).toContain('OpenAIHTTPForwardStage')
  })
})
