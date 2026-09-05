import { apiClient } from '../client'

export type ModerationMode = 'off' | 'observe' | 'pre_block'
export type ModerationProvider = 'openai' | 'zhipu'
export type KeywordBlockingMode = 'keyword_only' | 'keyword_and_api' | 'api_only'
export type ContentModerationEngineMode = 'rule_only' | 'api_only' | 'hybrid' | 'candidate_only'
export type ContentModerationPromptFilterMode = 'off' | 'observe' | 'warn' | 'block'
export type ContentModerationAuditScope = 'user_only' | 'user_and_tool' | 'all_context'
export type ContentModerationAccountScope = 'all' | 'oauth' | 'selected'
export type ContentModerationModelFilterType = 'all' | 'include' | 'exclude'
export type ContentModerationKeywordCategory =
  | 'custom'
  | 'jailbreak'
  | 'cyber'
  | 'minor_safety'
  | 'self_harm'
  | 'violence'
  | 'weapons'
  | 'privacy'
  | 'fraud'
  | 'account_abuse'
  | 'political'
  | 'high_impact_decision'
  | 'regulated_advice'
  | 'copyright'
  | 'biometric'
  | 'biosecurity'
  | 'other'
export type ContentModerationKeywordSeverity = 'low' | 'medium' | 'high' | 'critical'
export type ContentModerationKeywordAction = 'block' | 'observe' | 'warn'

export interface ContentModerationModelFilter {
  type: ContentModerationModelFilterType
  models: string[]
}

export interface ContentModerationKeywordRule {
  keyword: string
  category: ContentModerationKeywordCategory | string
  severity: ContentModerationKeywordSeverity | string
  action: ContentModerationKeywordAction | string
  enabled: boolean
}

export interface ContentModerationSemanticReviewConfig {
  enabled: boolean
  trigger: 'local_review' | 'all' | string
  primary_model: string
  fallback_models: string[]
  escalation_enabled: boolean
  escalation_model: string
  escalation_timeout_ms: number
  escalation_max_input_runes: number
  escalation_reasoning_effort: 'low' | 'medium' | 'high' | 'xhigh'
  timeout_ms: number
  primary_timeout_ms: number
  fallback_timeout_ms: number
  max_attempts_per_model: number
  max_input_runes: number
  max_output_tokens: number
  reasoning_effort: 'low' | 'medium' | 'high' | 'xhigh'
  prompt_injection_reviewer_enabled: boolean
  prompt_injection_max_input_runes: number
  prompt_injection_fail_closed: boolean
}

export interface ContentModerationSemanticReviewModelsResponse {
  models: string[]
}

export interface ContentModerationConfig {
	max_request_body_mib: number
	inflight_memory_budget_mib: number
	request_memory_multiplier: number
	minimum_request_charge_kib: number
	small_request_threshold_mib: number
	small_request_reserve_mib: number
	admission_wait_timeout_ms: number
	image_audit_max_concurrency: number
	request_audit_timeout_ms: number
	resource_protection_status: ResourceProtectionStatus
  enabled: boolean
  mode: ModerationMode
  provider: ModerationProvider
  base_url: string
  model: string
  pass_cache_enabled: boolean
  pass_cache_ttl_seconds: number
  decision_cache_enabled?: boolean
  decision_cache_ttl_seconds?: number
  candidate_fragment_runes?: number
  proxy_id: number | null
  api_key_configured: boolean
  api_key_masked: string
  api_key_count: number
  api_key_masks: string[]
  api_key_statuses: ContentModerationAPIKeyStatus[]
  timeout_ms: number
  sample_rate: number
  all_groups: boolean
  group_ids: number[]
  account_scope?: ContentModerationAccountScope
  account_ids?: number[]
  record_non_hits: boolean
  audit_scope: ContentModerationAuditScope | string
  store_input_excerpt: boolean
  search_input_excerpt: boolean
  thresholds: Record<string, number>
  worker_count: number
  queue_size: number
  block_status: number
  block_message: string
  email_on_hit: boolean
  auto_ban_enabled: boolean
  ban_threshold: number
  violation_window_hours: number
  retry_count: number
  hit_retention_days: number
  non_hit_retention_days: number
  pre_hash_check_enabled: boolean
  blocked_keywords: string[]
  keyword_rules: ContentModerationKeywordRule[]
  keyword_blocking_mode: KeywordBlockingMode
  engine_mode: ContentModerationEngineMode | string
  prompt_filter_mode?: ContentModerationPromptFilterMode | string
  prompt_filter_threshold?: number
  prompt_filter_strict_threshold?: number
  prompt_filter_source_revision?: string
  prompt_filter_source_url?: string
  prompt_filter_source_author?: string
  prompt_filter_source_permission?: string
  semantic_review?: ContentModerationSemanticReviewConfig
  model_filter: ContentModerationModelFilter
  fail_strategy?: ContentModerationFailStrategy
  cyber_policy_exclude_from_ban_count: boolean
}

export interface ResourceProtectionStatus {
  runtime_safe_maximum_mib: number
  active_bytes: number
  active_reservations: number
  waiting_requests: number
  active_image_audits: number
}

export interface ContentModerationFailStrategy {
  default: 'open' | 'closed' | string
  trusted_group_ids: number[]
  public_group_ids: number[]
}

export type ContentModerationAPIKeyStatusValue = 'unknown' | 'ok' | 'error' | 'frozen'

export interface ContentModerationAPIKeyStatus {
  index: number
  key_hash: string
  masked: string
  status: ContentModerationAPIKeyStatusValue
  failure_count: number
  success_count: number
  last_error: string
  last_checked_at?: string
  frozen_until?: string
  last_latency_ms: number
  last_http_status: number
  last_tested: boolean
  configured: boolean
}

export interface TestContentModerationAPIKeysPayload {
  api_keys?: string[]
  provider?: ModerationProvider
  base_url?: string
  model?: string
  timeout_ms?: number
  // null/undefined 沿用已保存配置的代理；0 强制直连；>0 指定代理
  proxy_id?: number
  prompt?: string
  images?: string[]
}

export interface TestContentModerationAPIKeysResponse {
  items: ContentModerationAPIKeyStatus[]
  audit_result?: ContentModerationTestAuditResult
  image_count: number
}

export interface TestContentModerationKeywordsPayload {
  prompt: string
}

export interface TestContentModerationKeywordsResponse {
  matched: boolean
  matched_keyword: string
  keyword_category: ContentModerationKeywordCategory | string
  keyword_severity: ContentModerationKeywordSeverity | string
  keyword_action: ContentModerationKeywordAction | string
  effective_keyword_action: ContentModerationKeywordAction | string
  risk_context_type: string
  risk_context_reason: string
  normalized_excerpt: string
}

export interface ContentModerationTestAuditResult {
  flagged: boolean
  highest_category: string
  highest_score: number
  composite_score: number
  category_scores: Record<string, number>
  thresholds: Record<string, number>
}

export interface UpdateContentModerationConfig {
	max_request_body_mib?: number
	inflight_memory_budget_mib?: number
	request_memory_multiplier?: number
	minimum_request_charge_kib?: number
	small_request_threshold_mib?: number
	small_request_reserve_mib?: number
	admission_wait_timeout_ms?: number
	image_audit_max_concurrency?: number
	request_audit_timeout_ms?: number
  enabled?: boolean
  mode?: ModerationMode
  provider?: ModerationProvider
  base_url?: string
  model?: string
  pass_cache_enabled?: boolean
  pass_cache_ttl_seconds?: number
  decision_cache_enabled?: boolean
  decision_cache_ttl_seconds?: number
  candidate_fragment_runes?: number
  // undefined 不修改；0 清除（直连）；>0 指定代理
  proxy_id?: number
  api_key?: string
  api_keys?: string[]
  api_keys_mode?: 'append' | 'replace'
  delete_api_key_hashes?: string[]
  clear_api_key?: boolean
  timeout_ms?: number
  sample_rate?: number
  all_groups?: boolean
  group_ids?: number[]
  account_scope?: ContentModerationAccountScope
  account_ids?: number[]
  record_non_hits?: boolean
  audit_scope?: ContentModerationAuditScope | string
  store_input_excerpt?: boolean
  search_input_excerpt?: boolean
  thresholds?: Record<string, number>
  worker_count?: number
  queue_size?: number
  block_status?: number
  block_message?: string
  email_on_hit?: boolean
  auto_ban_enabled?: boolean
  ban_threshold?: number
  violation_window_hours?: number
  retry_count?: number
  hit_retention_days?: number
  non_hit_retention_days?: number
  pre_hash_check_enabled?: boolean
  blocked_keywords?: string[]
  keyword_rules?: ContentModerationKeywordRule[]
  keyword_blocking_mode?: KeywordBlockingMode
  engine_mode?: ContentModerationEngineMode | string
  prompt_filter_mode?: ContentModerationPromptFilterMode | string
  prompt_filter_threshold?: number
  prompt_filter_strict_threshold?: number
  semantic_review?: ContentModerationSemanticReviewConfig
  model_filter?: ContentModerationModelFilter
  fail_strategy?: ContentModerationFailStrategy
  cyber_policy_exclude_from_ban_count?: boolean
}

export interface ContentModerationRuntimeStatus {
  build: ContentModerationBuildStatus
  security_baseline: ContentModerationSecurityBaselineStatus
  effective_protection: ContentModerationEffectiveProtectionStatus
  route_coverage: ContentModerationRouteCoverageStatus
  pipeline_coverage: ContentModerationPipelineCoverageStatus
  pipeline_execution: ContentModerationPipelineExecutionStatus
  enabled: boolean
  risk_control_enabled: boolean
  mode: ModerationMode
  provider: ModerationProvider
  model: string
  pass_cache_enabled: boolean
  pass_cache_available: boolean
  pass_cache_degraded_reason?: string
  pass_cache_ttl_seconds: number
  decision_cache_enabled?: boolean
  decision_cache_available?: boolean
  decision_cache_distributed?: boolean
  decision_cache_ttl_seconds?: number
  candidate_fragment_runes?: number
  chunker_version: string
  chunk_max_runes: number
  chunk_overlap_runes: number
  chunk_max_count: number
  worker_count: number
  max_workers: number
  active_workers: number
  idle_workers: number
  queue_size: number
  queue_length: number
  queue_usage_percent: number
  enqueued: number
  dropped: number
  processed: number
  errors: number
  pre_block_active: number
  pre_block_checked: number
  pre_block_allowed: number
  pre_block_blocked: number
  pre_block_errors: number
  pre_block_avg_latency_ms: number
  pre_block_api_key_active: number
  pre_block_api_key_available_count: number
  pre_block_api_key_total_calls: number
  pre_block_api_key_loads: ContentModerationAPIKeyLoad[]
  api_key_statuses: ContentModerationAPIKeyStatus[]
  flagged_hash_count: number
  last_cleanup_at?: string
  last_cleanup_deleted_hit: number
  last_cleanup_deleted_non_hit: number
  semantic_review_usage?: ContentModerationSemanticReviewUsageStats
}

export interface ContentModerationSemanticReviewUsageStats {
  available: boolean
  window_hours: number
  total_calls: number
  primary_calls: number
  fallback_calls: number
  other_calls: number
  input_tokens: number
  output_tokens: number
  avg_latency_ms: number
}

export interface ContentModerationBuildStatus {
  version: string
  commit: string
  date: string
  build_type: string
}

export interface ContentModerationSecurityBaselineStatus {
  policy_schema_version: string
  moderation_extractor_version: string
  minimum_security_baseline_commit: string
  baseline_satisfied: boolean
  baseline_satisfaction_method: string
}

export interface ContentModerationEffectiveProtectionStatus {
  effective_blocking: boolean
  risk_control_enabled: boolean
  moderation_enabled: boolean
  mode: ModerationMode | string
  audit_scope: ContentModerationAuditScope | string
  public_fail_strategy: 'open' | 'closed' | string
  group_coverage: string
  account_coverage: string
  model_coverage: string
  engine_mode: ContentModerationEngineMode | string
  external_api_configured: boolean
  external_api_healthy: boolean
  external_api_usable_key_count: number
  external_api_last_error: string
  high_risk_rules_blocking: boolean
  deterministic_policy_present: boolean
  high_risk_rules_present: boolean
  unsafe_reasons: string[]
}

export interface ContentModerationRouteCoverageStatus {
  manifest_version: string
  manifest_hash: string
  status: string
  required_routes: number
  covered_routes: number
  uncovered_routes: string[]
}

export interface ContentModerationPipelineCoverageStatus {
  manifest_version: string
  version: string
  manifest_hash: string
  status: string
  openai_http: ContentModerationPipelineGroupCoverageStatus
  openai_websocket: ContentModerationOpenAIWebSocketPipelineCoverageStatus
  gateway_pre_forward: ContentModerationPipelineGroupCoverageStatus
}

export interface ContentModerationOpenAIWebSocketPipelineCoverageStatus extends ContentModerationPipelineGroupCoverageStatus {
  responses: ContentModerationPipelineGroupCoverageStatus
  realtime: ContentModerationPipelineGroupCoverageStatus
}

export interface ContentModerationPipelineGroupCoverageStatus {
  version: string
  pipeline: string
  status: string
  required_routes: number
  covered_routes: number
  uncovered_routes: string[]
  stage_coverage: ContentModerationPipelineStageCoverageStatus[]
  routes: ContentModerationPipelineRouteCoverageStatus[]
}

export interface ContentModerationPipelineStageCoverageStatus {
  stage: string
  required_routes: number
  covered_routes: number
  uncovered_routes: string[]
}

export interface ContentModerationPipelineRouteCoverageStatus {
  method: string
  path: string
  handler: string
  protocol: string
  pipeline: string
  covered: boolean
  forward_adapters?: string[]
  forward_adapter_descriptors?: ContentModerationRouteAdapterDescriptor[]
  stage_adapter_descriptors?: ContentModerationRouteAdapterDescriptor[]
  uncovered_stages?: string[]
  stages: ContentModerationPipelineRouteStageCoverageStatus[]
}

export interface ContentModerationRouteAdapterDescriptor {
  stage: string
  pipeline: string
  name: string
}

export interface ContentModerationPipelineRouteStageCoverageStatus {
  stage: string
  required: boolean
  covered: boolean
}

export interface ContentModerationPipelineExecutionStatus {
  total_count: number
  error_count: number
  recent_window_seconds: number
  recent_window_count: number
  recent_window_error_count: number
  last_observed_at?: string
  executions: ContentModerationPipelineExecutionObservation[]
  routes: ContentModerationPipelineRouteExecutionObservation[]
  stage_observation_coverage?: ContentModerationPipelineExecutionStageObservationCoverage
}

export interface ContentModerationPipelineExecutionStageObservationCoverage {
  status: string
  expected_stages: number
  observed_stages: number
  unobserved_stages: string[]
}

export interface ContentModerationPipelineExecutionObservation {
  pipeline: string
  stage: string
  source: string
  method?: string
  path?: string
  handler?: string
  protocol?: string
  count: number
  error_count: number
  last_observed_at?: string
}

export interface ContentModerationPipelineRouteExecutionObservation {
  pipeline: string
  method?: string
  path?: string
  handler?: string
  protocol?: string
  count: number
  error_count: number
  recent_count: number
  recent_error_count: number
  last_observed_at?: string
  stages: ContentModerationPipelineExecutionObservation[]
}

export interface ContentModerationAPIKeyLoad {
  index: number
  key_hash: string
  masked: string
  status: ContentModerationAPIKeyStatusValue
  active: number
  total: number
  success: number
  errors: number
  avg_latency_ms: number
  last_latency_ms: number
  last_http_status: number
}

export interface ContentModerationLog {
  id: number
  request_id: string
  user_id: number | null
  user_email: string
  api_key_id: number | null
  api_key_name: string
  group_id: number | null
  group_name: string
  account_id?: number | null
  account_name?: string
  account_type?: string
  endpoint: string
  provider: string
  model: string
  mode: string
  action: string
  flagged: boolean
  highest_category: string
  highest_score: number
  matched_keyword: string
  category_scores: Record<string, number>
  threshold_snapshot: Record<string, number>
  input_excerpt: string
  truncate_reasons?: string[]
  upstream_latency_ms: number | null
  error: string
  metadata: Record<string, unknown>
  violation_count: number
  auto_banned: boolean
  email_sent: boolean
  user_status: string
  queue_delay_ms: number | null
  keyword_category: string
  keyword_severity: string
  keyword_action: string
  effective_keyword_action: string
  risk_context_type: string
  risk_context_reason: string
  review_status: string
  review_note: string
  reviewed_by: number | null
  reviewed_at: string | null
  raw_request_available: boolean
  raw_request_bytes: number
  raw_request_truncated: boolean
  decision_source?: string
  moderation_provider?: string
  moderation_model?: string
  source_origin?: string
  selected_source?: string
  selected_source_role?: string
  selected_fragment_runes?: number
  decision_cache_hit?: boolean
  duplicate_retry_count?: number
  user_violation_eligible?: boolean
  evidence_available?: boolean
  created_at: string
}

export interface ListContentModerationLogsParams {
  page?: number
  page_size?: number
  result?: string
  decision_source?: string
  review_status?: string
  group_id?: number
  endpoint?: string
  search?: string
  from?: string
  to?: string
}

export interface ContentModerationLogsResponse {
  items: ContentModerationLog[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface ContentModerationUnbanUserResponse {
  user_id: number
  status: string
}

export interface ReviewContentModerationLogPayload {
  status: 'pending' | 'false_positive' | 'confirmed_violation' | string
  note?: string
}

export interface ContentModerationRawRequest {
  log_id: number
  request_id: string
  body: string
  body_bytes: number
  truncated: boolean
  created_at: string
}

export interface ContentModerationEvidence {
  log_id: number
  request_id: string
  selection: Record<string, unknown>
  payload: string
  payload_hmac: string
  payload_runes: number
  created_at: string
}

export interface DeleteFlaggedHashResponse {
  input_hash: string
  deleted: boolean
}

export interface ClearFlaggedHashesResponse {
  deleted: number
}

export async function getConfig(): Promise<ContentModerationConfig> {
  const { data } = await apiClient.get<ContentModerationConfig>('/admin/risk-control/config')
  return data
}

export async function getSemanticReviewModels(): Promise<string[]> {
  const { data } = await apiClient.get<ContentModerationSemanticReviewModelsResponse>(
    '/admin/risk-control/semantic-review/models'
  )
  return Array.isArray(data.models) ? data.models : []
}

export async function updateConfig(
  payload: UpdateContentModerationConfig
): Promise<ContentModerationConfig> {
  const { data } = await apiClient.put<ContentModerationConfig>('/admin/risk-control/config', payload)
  return data
}

export async function getStatus(): Promise<ContentModerationRuntimeStatus> {
  const { data } = await apiClient.get<ContentModerationRuntimeStatus>('/admin/risk-control/status')
  return data
}

export async function testAPIKeys(
  payload: TestContentModerationAPIKeysPayload = {}
): Promise<TestContentModerationAPIKeysResponse> {
  const { data } = await apiClient.post<TestContentModerationAPIKeysResponse>('/admin/risk-control/api-keys/test', payload)
  return data
}

export async function testKeywords(
  payload: TestContentModerationKeywordsPayload
): Promise<TestContentModerationKeywordsResponse> {
  const { data } = await apiClient.post<TestContentModerationKeywordsResponse>(
    '/admin/risk-control/keywords/test',
    payload
  )
  return data
}

export async function listLogs(
  params: ListContentModerationLogsParams = {}
): Promise<ContentModerationLogsResponse> {
  const { data } = await apiClient.get<ContentModerationLogsResponse>('/admin/risk-control/logs', {
    params,
  })
  return data
}

export async function unbanUser(userID: number): Promise<ContentModerationUnbanUserResponse> {
  const { data } = await apiClient.post<ContentModerationUnbanUserResponse>(
    `/admin/risk-control/users/${userID}/unban`
  )
  return data
}

export async function reviewLog(
  logID: number,
  payload: ReviewContentModerationLogPayload
): Promise<ContentModerationLog> {
  const { data } = await apiClient.patch<ContentModerationLog>(
    `/admin/risk-control/logs/${logID}/review`,
    payload
  )
  return data
}

export async function getRawRequest(logID: number): Promise<ContentModerationRawRequest> {
  const { data } = await apiClient.get<ContentModerationRawRequest>(
    `/admin/risk-control/logs/${logID}/raw-request`
  )
  return data
}

export async function getEvidence(logID: number): Promise<ContentModerationEvidence> {
  const { data } = await apiClient.get<ContentModerationEvidence>(
    `/admin/risk-control/logs/${logID}/evidence`
  )
  return data
}

export async function deleteFlaggedHash(inputHash: string): Promise<DeleteFlaggedHashResponse> {
  const { data } = await apiClient.delete<DeleteFlaggedHashResponse>('/admin/risk-control/hashes', {
    data: { input_hash: inputHash },
  })
  return data
}

export async function clearFlaggedHashes(): Promise<ClearFlaggedHashesResponse> {
  const { data } = await apiClient.delete<ClearFlaggedHashesResponse>('/admin/risk-control/hashes/all')
  return data
}

export const riskControlAPI = {
  getConfig,
  getSemanticReviewModels,
  updateConfig,
  getStatus,
  testAPIKeys,
  testKeywords,
  listLogs,
  unbanUser,
  reviewLog,
  getRawRequest,
  getEvidence,
  deleteFlaggedHash,
  clearFlaggedHashes,
}

export default riskControlAPI
