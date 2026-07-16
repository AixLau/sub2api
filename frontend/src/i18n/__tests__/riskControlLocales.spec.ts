import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('risk control locale copy', () => {
  it('describes worker runtime as audit and pre-block record processing', () => {
    expect(zh.admin.riskControl.workerStatusHint).toContain('前置拦截记录任务')
    expect(zh.admin.riskControl.workerStatusHint).not.toContain('异步观察任务')
    expect(en.admin.riskControl.workerStatusHint).toContain('pre-block record tasks')
    expect(en.admin.riskControl.workerStatusHint).not.toContain('observation tasks')
  })

  it('keeps pre-block audit key summary aware of async worker load', () => {
    expect(zh.admin.riskControl.preBlockAPIKeyLoadSummary).toContain('worker：{workerActive} / {workerTotal}')
    expect(en.admin.riskControl.preBlockAPIKeyLoadSummary).toContain('worker: {workerActive} / {workerTotal}')
  })

  it('does not describe pre-block audit key polling as bypassing the worker pool', () => {
    expect(zh.admin.riskControl.preBlockAPIKeyLoadHint).toBe('同步前置拦截直接轮询可用审核 Key。')
    expect(zh.admin.riskControl.preBlockAPIKeyLoadHint).not.toContain('Worker 池')
    expect(en.admin.riskControl.preBlockAPIKeyLoadHint).not.toContain('worker pool')
  })

  it('includes keyword metadata and dry-run copy', () => {
    expect(zh.admin.riskControl.matchedKeyword).toBe('命中关键词')
    expect(zh.admin.riskControl.keywordCategory).toBe('关键词分类')
    expect(zh.admin.riskControl.keywordTestMatched).toBe('已命中')
    expect(zh.admin.riskControl.keywordRules).toBe('结构化关键词规则')
    expect(zh.admin.riskControl.keywordRuleCount).toContain('{enabled}')
    expect(zh.admin.riskControl.legacyBlockedKeywords).toBe('旧版拦截关键词')
    expect(zh.admin.riskControl.legacyBlockedKeywordCount).toContain('{count}')
    expect(zh.admin.riskControl.keywordRuleEnabled).toBe('已启用')
    expect(zh.admin.riskControl.keywordRuleDisabled).toBe('已停用')
    expect(zh.admin.riskControl.action.keywordReview).toBe('关键词复核')
    expect(zh.admin.riskControl.reviewStatus.pending).toBe('待复核')
    expect(zh.admin.riskControl.riskContexts.codexInternal).toContain('Codex')
    expect(zh.admin.riskControl.effectiveKeywordAction).toBe('最终动作')
    expect(zh.admin.riskControl.filters.search).toContain('关键词')
    expect(en.admin.riskControl.matchedKeyword).toBe('Matched Keyword')
    expect(en.admin.riskControl.keywordCategory).toBe('Keyword Category')
    expect(en.admin.riskControl.keywordTestMatched).toBe('Matched')
    expect(en.admin.riskControl.keywordRules).toBe('Structured keyword rules')
    expect(en.admin.riskControl.keywordRuleCount).toContain('{enabled}')
    expect(en.admin.riskControl.legacyBlockedKeywords).toBe('Legacy blocked keywords')
    expect(en.admin.riskControl.legacyBlockedKeywordCount).toContain('{count}')
    expect(en.admin.riskControl.keywordRuleEnabled).toBe('Enabled')
    expect(en.admin.riskControl.keywordRuleDisabled).toBe('Disabled')
    expect(en.admin.riskControl.action.keywordReview).toBe('Keyword Review')
    expect(en.admin.riskControl.reviewStatus.pending).toBe('Pending')
    expect(en.admin.riskControl.riskContexts.codexInternal).toContain('Codex')
    expect(en.admin.riskControl.effectiveKeywordAction).toBe('Effective action')
    expect(en.admin.riskControl.filters.search).toContain('keyword')
  })

  it('labels stored moderation input as an excerpt instead of full content', () => {
    expect(zh.admin.riskControl.inputDetailContent).toBe('输入摘要')
    expect(zh.admin.riskControl.inputDetailContent).not.toContain('完整')
    expect(en.admin.riskControl.inputDetailContent).toBe('Input Excerpt')
    expect(en.admin.riskControl.inputDetailContent).not.toContain('Full')
  })

  it('localizes extraction failure reasons for administrators', () => {
    expect(zh.admin.riskControl.truncationReason.unsupportedRequiredValue).toBe('请求结构包含不支持的字段类型或值')
    expect(zh.admin.riskControl.truncationReason.other).toBe('其他提取异常')
    expect(en.admin.riskControl.truncationReason.unsupportedRequiredValue).toBe('Unsupported field type or value in request structure')
    expect(en.admin.riskControl.truncationReason.other).toBe('Other extraction error')
  })

  it('localizes audit decision codes and built-in candidate keywords', () => {
    expect(zh.admin.riskControl.moderationCategories.semanticReview).toBe('语义审核')
    expect(zh.admin.riskControl.keywordCategories.accountAbuse).toBe('账号滥用')
    expect(zh.admin.riskControl.keywordSeverities.critical).toBe('严重')
    expect(zh.admin.riskControl.candidateKeywords.reverseEngineeringOperationalRequest).toBe('逆向工程操作请求')
    expect(zh.admin.riskControl.candidateKeywords.webExploitationUnauthorizedHarmRequest).toContain('未授权')
    expect(zh.admin.riskControl.candidateKeywords.reverseEngineeringSensitiveCandidate).toBe('敏感逆向工程候选')
    expect(zh.admin.riskControl.decisionSources.semanticReview).toBe('语义审核')
    expect(en.admin.riskControl.moderationCategories.semanticReview).toBe('Semantic review')
    expect(en.admin.riskControl.candidateKeywords.reverseEngineeringOperationalRequest).toBe('Reverse-engineering operation request')
    expect(en.admin.riskControl.candidateKeywords.pentestUnauthorizedHarmRequest).toContain('Unauthorized')
    expect(en.admin.riskControl.candidateKeywords.persistence).toBe('Persistence topic')
    expect(en.admin.riskControl.decisionSources.semanticReview).toBe('Semantic review')
  })

  it('describes candidate review as a single bounded user fragment', () => {
    expect(zh.admin.riskControl.promptFilterModeHint).toContain('一个用户上下文片段')
    expect(zh.admin.riskControl.promptFilterModeHint).not.toContain('混合模式')
    expect(zh.admin.riskControl.semanticReviewHint).toContain('gpt-5.3-codex-spark')
    expect(en.admin.riskControl.promptFilterModeHint).toContain('one user-context fragment')
    expect(en.admin.riskControl.promptFilterModeHint).not.toContain('hybrid mode')
    expect(en.admin.riskControl.semanticReviewHint).toContain('gpt-5.4-mini')
  })
})
