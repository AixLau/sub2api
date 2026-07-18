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
  reviewLog,
  getRawRequest,
  getGroups,
	listAccounts,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  getStatus: vi.fn(),
  listLogs: vi.fn(),
  testKeywords: vi.fn(),
  reviewLog: vi.fn(),
  getRawRequest: vi.fn(),
  getGroups: vi.fn(),
	listAccounts: vi.fn(),
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
      reviewLog,
      getRawRequest,
      testAPIKeys: vi.fn(),
      deleteFlaggedHash: vi.fn(),
      clearFlaggedHashes: vi.fn(),
      unbanUser: vi.fn(),
    },
    groups: {
      getAll: getGroups,
    },
	accounts: {
		list: listAccounts,
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
  account_scope: 'all',
  account_ids: [],
  record_non_hits: false,
  audit_scope: 'user_only',
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
  engine_mode: 'candidate_only',
  decision_cache_enabled: true,
  decision_cache_ttl_seconds: 600,
  candidate_fragment_runes: 2000,
	semantic_review: {
	  enabled: true,
	  trigger: 'local_review',
	  primary_model: 'gpt-5.3-codex-spark',
	  fallback_models: ['gpt-5.4-mini'],
	  timeout_ms: 8000,
	  primary_timeout_ms: 5000,
	  fallback_timeout_ms: 3000,
	  max_attempts_per_model: 1,
	  max_input_runes: 2000,
	  max_output_tokens: 512,
	  reasoning_effort: 'low',
	  prompt_injection_reviewer_enabled: true,
	  prompt_injection_max_input_runes: 12000,
	  prompt_injection_fail_closed: true,
	},
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
    reviewLog.mockReset()
    getRawRequest.mockReset()
    getGroups.mockReset()
	listAccounts.mockReset()
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
      effective_keyword_action: '',
      risk_context_type: '',
      risk_context_reason: '',
      normalized_excerpt: '',
    })
    getGroups.mockResolvedValue([])
	listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 0 })
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
    reviewLog.mockImplementation(async (id: number, payload: { status: string; note?: string }) => ({
      id,
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
      action: 'keyword_review',
      flagged: false,
      highest_category: 'keyword',
      highest_score: 1,
      category_scores: {},
      threshold_snapshot: {},
      input_excerpt: 'please sell api key',
      matched_keyword: 'sell api key',
      keyword_category: 'account_abuse',
      keyword_severity: 'critical',
      keyword_action: 'block',
      effective_keyword_action: 'observe',
      risk_context_type: 'meta_discussion',
      risk_context_reason: 'policy_or_keyword_rule_discussion',
      review_status: payload.status,
      review_note: payload.note ?? '',
      reviewed_by: 1,
      reviewed_at: '2026-06-19T08:01:00Z',
      raw_request_available: false,
      raw_request_bytes: 0,
      raw_request_truncated: false,
      upstream_latency_ms: null,
      error: '',
      violation_count: 0,
      auto_banned: false,
      email_sent: false,
      user_status: 'active',
      queue_delay_ms: null,
      created_at: '2026-06-19T08:00:00Z',
    }))
  })

  it('does not render the upstream protection status card', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      effective_protection: {
        effective_blocking: true,
        risk_control_enabled: true,
        moderation_enabled: true,
        mode: 'pre_block',
        audit_scope: 'user_only',
        public_fail_strategy: 'closed',
        group_coverage: 'all_public_groups',
        model_coverage: 'all',
        engine_mode: 'candidate_only',
        external_api_configured: false,
        external_api_healthy: false,
        external_api_usable_key_count: 0,
        external_api_last_error: '',
        high_risk_rules_blocking: true,
        deterministic_policy_present: true,
        high_risk_rules_present: true,
        unsafe_reasons: [],
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

    expect(wrapper.text()).not.toContain('admin.riskControl.protectionTitle')
    expect(wrapper.text()).not.toContain('admin.riskControl.protectionExternalSemanticFallback')
  })

  it('shows semantic reviewer calls and filters platform review records', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      semantic_review_usage: {
        available: true,
        window_hours: 24,
        total_calls: 12,
        primary_calls: 9,
        fallback_calls: 3,
        other_calls: 0,
        input_tokens: 4800,
        output_tokens: 600,
        avg_latency_ms: 742,
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

    const usage = wrapper.get('[data-test="semantic-review-usage"]')
    expect(usage.text()).toContain('12')
    expect(usage.text()).toContain('gpt-5.3-codex-spark')
    expect(usage.text()).toContain('gpt-5.4-mini')
    expect(usage.text()).toContain('742 ms')

    await findButtonByText(wrapper, 'admin.riskControl.semanticUsage.viewRecords').trigger('click')
    await flushPromises()
    expect(listLogs).toHaveBeenLastCalledWith(expect.objectContaining({
      decision_source: 'semantic_review',
      from: expect.any(String),
    }))
  })

  it('shows actionable admin risk totals and filters records from a metric', async () => {
    listLogs.mockImplementation(async (params: Record<string, unknown>) => {
      let total = 0
      if (params.result === 'blocked' && params.page_size === 1) total = 7
      if (params.result === 'hit' && params.page_size === 1) total = 11
      if (params.review_status === 'pending' && params.page_size === 1) total = 3
      if (params.result === 'error' && params.page_size === 1) total = 2
      return { items: [], total, page: 1, page_size: Number(params.page_size ?? 20), pages: total > 0 ? 1 : 0 }
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

    expect(wrapper.get('[data-test="admin-metric-blocked"]').text()).toContain('7')
    expect(wrapper.get('[data-test="admin-metric-hit"]').text()).toContain('11')
    expect(wrapper.get('[data-test="admin-metric-pending"]').text()).toContain('3')
    expect(wrapper.get('[data-test="admin-metric-error"]').text()).toContain('2')

    await wrapper.get('[data-test="admin-metric-blocked"]').trigger('click')
    await flushPromises()

    expect(listLogs).toHaveBeenCalledWith(expect.objectContaining({
      page_size: 20,
      result: 'blocked',
      from: expect.any(String),
    }))
  })

  it('shows normalized semantic reviewer output in record details', async () => {
    listLogs.mockResolvedValue({
      items: [{
        id: 91,
        created_at: '2026-07-14T01:00:54Z',
        user_email: 'admin@example.com',
        action: 'semantic_review_allow',
        flagged: false,
        highest_category: 'benign_task_generation_guidance',
        highest_score: 0.99,
        input_excerpt: 'semantic excerpt',
        decision_source: 'semantic_review',
        moderation_provider: 'platform_openai',
        moderation_model: 'gpt-5.3-codex-spark',
        error: JSON.stringify({
          semantic_review_verdict: 'allow',
          semantic_review_intent: 'benign',
          semantic_review_confidence: 0.99,
          semantic_review_categories: ['benign_task_generation_guidance'],
        }),
      }],
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

    await findButtonByText(wrapper, 'semantic excerpt').trigger('click')

    expect(wrapper.text()).toContain('admin.riskControl.modelResponse')
    expect(wrapper.text()).toContain('admin.riskControl.modelResponseFields.intent')
    expect(wrapper.text()).toContain('benign')
    expect(wrapper.text()).toContain('99.0%')
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
          keyword_action: 'block',
          effective_keyword_action: 'block',
          risk_context_type: 'actual_request',
          risk_context_reason: 'request_intent_marker',
          review_status: '',
          review_note: '',
          reviewed_by: null,
          reviewed_at: null,
          raw_request_available: false,
          raw_request_bytes: 0,
          raw_request_truncated: false,
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
    expect(wrapper.text()).toContain('admin.riskControl.keywordCategories.accountAbuse')
    expect(wrapper.text()).toContain('admin.riskControl.keywordSeverities.critical')

    await findButtonByText(wrapper, 'please sell api key').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.keywordMetadata')
    expect(wrapper.text()).toContain('admin.riskControl.keywordCategory')
    expect(wrapper.text()).toContain('admin.riskControl.keywordSeverity')
  })

  it('loads full raw request content from a risk audit log detail', async () => {
    listLogs.mockResolvedValue({
      items: [
        {
          id: 26190,
          request_id: 'def739c1-3389-45ba-acb1-2c47977b82c4',
          user_id: 244,
          user_email: '1914823683@qq.com',
          api_key_id: 9,
          api_key_name: 'H',
          group_id: 5,
          group_name: 'Codex高速专线',
          endpoint: '/responses',
          provider: 'openai',
          model: 'gpt-5.4',
          mode: 'pre_upstream',
          action: 'cyber_policy_session_blocked',
          flagged: true,
          highest_category: 'cyber_policy_session_blocked',
          highest_score: 1,
          category_scores: {},
          threshold_snapshot: {},
          input_excerpt: 'cyber_policy_session_blocked',
          matched_keyword: '',
          keyword_category: '',
          keyword_severity: '',
          keyword_action: '',
          effective_keyword_action: '',
          risk_context_type: 'actual_request',
          risk_context_reason: 'openai_cyber_policy_session_block',
          review_status: '',
          review_note: '',
          reviewed_by: null,
          reviewed_at: null,
          raw_request_available: true,
          raw_request_bytes: 103,
          raw_request_truncated: false,
          upstream_latency_ms: null,
          error: 'cyber_policy_session_blocked',
          truncate_reasons: ['max_total_runes'],
          violation_count: 0,
          auto_banned: false,
          email_sent: false,
          user_status: 'active',
          queue_delay_ms: null,
          created_at: '2026-07-06T13:55:56Z',
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getRawRequest.mockResolvedValue({
      log_id: 26190,
      request_id: 'def739c1-3389-45ba-acb1-2c47977b82c4',
      body: '{"model":"gpt-5.4","input":[{"role":"user","content":"please inspect this OpenAI cyber policy block"}]}',
      body_bytes: 103,
      truncated: false,
      created_at: '2026-07-06T13:55:56Z',
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

    expect(wrapper.text()).toContain('admin.riskControl.action.cyberPolicySessionBlocked')
    await findButtonByText(wrapper, 'cyber_policy_session_blocked').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.viewRawRequest')
    expect(wrapper.text()).toContain('admin.riskControl.truncationReasons')
    expect(wrapper.text()).toContain('admin.riskControl.truncationReason.maxTotalRunes')
    expect(wrapper.text()).not.toContain('max_total_runes')
    expect(wrapper.text()).toContain('admin.riskControl.rawRequestMeta')

    await findButtonByText(wrapper, 'admin.riskControl.viewRawRequest').trigger('click')
    await flushPromises()

    expect(getRawRequest).toHaveBeenCalledWith(26190)
    expect(wrapper.text()).toContain('please inspect this OpenAI cyber policy block')
  })

  it('runs keyword tests without saving config or writing logs', async () => {
    testKeywords.mockResolvedValue({
      matched: true,
      matched_keyword: 'sell api key',
      keyword_category: 'account_abuse',
      keyword_severity: 'critical',
      keyword_action: 'block',
      effective_keyword_action: 'observe',
      risk_context_type: 'meta_discussion',
      risk_context_reason: 'policy_or_keyword_rule_discussion',
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
    const promptInjectionStatus = wrapper.get('[data-test="prompt-injection-reviewer-status"]')
    expect(promptInjectionStatus.text()).toContain('admin.riskControl.promptInjectionReviewerStatus')
    expect(promptInjectionStatus.text()).toContain('admin.riskControl.promptInjectionFailClosedStatus')
    expect(promptInjectionStatus.text()).toContain('12,000')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.keywords').trigger('click')
    await wrapper.get('[data-test="keyword-test-prompt"]').setValue('please s e l l api key now')
    await findButtonByText(wrapper, 'admin.riskControl.runKeywordTest').trigger('click')
    await flushPromises()

    expect(testKeywords).toHaveBeenCalledWith({ prompt: 'please s e l l api key now' })
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.riskControl.keywordTestMatched')
    expect(wrapper.text()).toContain('sell api key')
    expect(wrapper.text()).toContain('admin.riskControl.keywordCategories.accountAbuse')
    expect(wrapper.text()).toContain('admin.riskControl.keywordSeverities.critical')
    expect(wrapper.text()).toContain('admin.riskControl.action.block')
    expect(wrapper.text()).toContain('please sell api key now')
  })

  it('marks keyword review records as false positives', async () => {
    listLogs.mockResolvedValue({
      items: [
        {
          id: 99,
          request_id: 'req-review',
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
          action: 'keyword_review',
          flagged: false,
          highest_category: 'keyword',
          highest_score: 1,
          category_scores: {},
          threshold_snapshot: {},
          input_excerpt: 'audit keyword discussion',
          matched_keyword: '儿童性虐待材料',
          keyword_category: 'minor_safety',
          keyword_severity: 'critical',
          keyword_action: 'block',
          effective_keyword_action: 'observe',
          risk_context_type: 'meta_discussion',
          risk_context_reason: 'policy_or_keyword_rule_discussion',
          review_status: 'pending',
          review_note: '',
          reviewed_by: null,
          reviewed_at: null,
          raw_request_available: false,
          raw_request_bytes: 0,
          raw_request_truncated: false,
          upstream_latency_ms: null,
          error: '',
          violation_count: 0,
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

    expect(wrapper.text()).toContain('admin.riskControl.action.keywordReview')
    expect(wrapper.text()).toContain('admin.riskControl.reviewStatusLabel')
    await findButtonByText(wrapper, 'admin.riskControl.markFalsePositive').trigger('click')
    await flushPromises()

    expect(reviewLog).toHaveBeenCalledWith(99, {
      status: 'false_positive',
      note: 'admin.riskControl.defaultFalsePositiveNote',
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.riskControl.reviewSaved')
  })

  it('shows structured keyword rules and preserves them when saving config', async () => {
    const config = baseConfig()
    config.keyword_rules = [
      {
        keyword: 'child sexual abuse material',
        category: 'minor_safety',
        severity: 'critical',
        action: 'block',
        enabled: true,
      },
      {
        keyword: 'suicide method',
        category: 'self_harm',
        severity: 'critical',
        action: 'block',
        enabled: false,
      },
    ]
    getConfig.mockResolvedValue(config)

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

    expect(wrapper.text()).toContain('admin.riskControl.keywordRules')
    expect(wrapper.text()).toContain('admin.riskControl.keywordRuleCount')
    expect(wrapper.text()).toContain('admin.riskControl.legacyBlockedKeywords')
    expect(wrapper.text()).toContain('admin.riskControl.legacyBlockedKeywordCount')
    expect(wrapper.text()).not.toContain('admin.riskControl.blockedKeywordCount')
    expect(wrapper.text()).toContain('child sexual abuse material')
    expect(wrapper.text()).toContain('admin.riskControl.keywordCategories.minorSafety')
    expect(wrapper.text()).toContain('admin.riskControl.keywordSeverities.critical')
    expect(wrapper.text()).not.toContain('admin.riskControl.keywordAction')
    expect(wrapper.text()).toContain('admin.riskControl.keywordRuleEnabled')
    expect(wrapper.text()).toContain('suicide method')
    expect(wrapper.text()).toContain('admin.riskControl.keywordCategories.selfHarm')
    expect(wrapper.text()).toContain('admin.riskControl.keywordRuleDisabled')

    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      keyword_rules: config.keyword_rules,
      engine_mode: 'candidate_only',
      keyword_blocking_mode: 'keyword_and_api',
      record_non_hits: false,
      audit_scope: 'user_only',
	      semantic_review: expect.objectContaining({
	        enabled: true,
	        trigger: 'local_review',
	        timeout_ms: 8000,
	        primary_timeout_ms: 5000,
	        fallback_timeout_ms: 3000,
	        max_attempts_per_model: 1,
	        max_input_runes: 2000,
	        max_output_tokens: 512,
	        reasoning_effort: 'low',
	        prompt_injection_reviewer_enabled: true,
	        prompt_injection_max_input_runes: 12000,
	        prompt_injection_fail_closed: true,
	      }),
    }))
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

	it('saves OAuth credential account scope without selected account IDs', async () => {
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
		await findButtonByText(wrapper, 'admin.riskControl.accountScopeOAuth').trigger('click')
		await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
		await flushPromises()

		expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
			account_scope: 'oauth',
			account_ids: [],
		}))
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

  it('renders operator-readable protection summary and keeps pipeline diagnostics collapsed by default', async () => {
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
              forward_adapter_descriptors: [
                { stage: 'forward', pipeline: 'openai_http', name: 'OpenAIHTTPForwardStage' },
              ],
              stage_adapter_descriptors: [
                { stage: 'billing', pipeline: 'openai_http', name: 'OpenAIHTTPBillingStage' },
                { stage: 'routing', pipeline: 'openai_http', name: 'OpenAIHTTPRoutingStage' },
                { stage: 'forward', pipeline: 'openai_http', name: 'OpenAIHTTPForwardStage' },
                { stage: 'usage', pipeline: 'openai_http', name: 'OpenAIHTTPUsageStage' },
              ],
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

    const summary = wrapper.get('[data-test="pipeline-operator-summary"]')
    expect(summary.text()).toContain('admin.riskControl.protectionChainTitle')
    expect(summary.text()).toContain('admin.riskControl.protectionChainCoverage')
    expect(summary.text()).toContain('mismatch · 1/2')
    expect(summary.text()).toContain('admin.riskControl.protectionChainRecentTraffic')
    expect(summary.text()).toContain('3')
    expect(summary.text()).toContain('admin.riskControl.protectionChainErrors')
    expect(summary.text()).toContain('2')
    expect(summary.text()).toContain('admin.riskControl.protectionChainObservedChecks')
    expect(summary.text()).toContain('1/7')
    expect(summary.text()).toContain('admin.riskControl.protectionChainUnobservedSummary')
    expect(summary.text()).toContain('2')

    expect(summary.text()).not.toContain('stage-hash-123')
    expect(summary.text()).not.toContain('OpenAIGatewayHandler.Responses')
    expect(summary.text()).not.toContain('billing:OpenAIHTTPBillingStage@openai_http')

    expect(wrapper.find('[data-test="pipeline-advanced-diagnostics"]').exists()).toBe(false)

    await wrapper.get('[data-test="pipeline-advanced-toggle"]').trigger('click')

    const diagnostics = wrapper.get('[data-test="pipeline-advanced-diagnostics"]')
    expect(diagnostics.text()).toContain('2026-06-29.2')
    expect(diagnostics.text()).toContain('openai-http-pre-forward-v2')
    expect(diagnostics.text()).toContain('stage-hash-123')
    expect(diagnostics.text()).toContain('POST /v1/responses OpenAIGatewayHandler.Responses moderation')
    expect(diagnostics.text()).toContain('POST /v1/responses OpenAIGatewayHandler.Responses usage')
    expect(diagnostics.text()).toContain('billing:OpenAIHTTPBillingStage@openai_http')
    expect(diagnostics.text()).toContain('forward:OpenAIHTTPForwardStage@openai_http')
  })
})
