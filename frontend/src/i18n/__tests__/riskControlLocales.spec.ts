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


})
