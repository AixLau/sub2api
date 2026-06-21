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
    expect(zh.admin.riskControl.filters.search).toContain('关键词')
    expect(en.admin.riskControl.matchedKeyword).toBe('Matched Keyword')
    expect(en.admin.riskControl.keywordCategory).toBe('Keyword Category')
    expect(en.admin.riskControl.keywordTestMatched).toBe('Matched')
    expect(en.admin.riskControl.filters.search).toContain('keyword')
  })

  it('labels stored moderation input as an excerpt instead of full content', () => {
    expect(zh.admin.riskControl.inputDetailContent).toBe('输入摘要')
    expect(zh.admin.riskControl.inputDetailContent).not.toContain('完整')
    expect(en.admin.riskControl.inputDetailContent).toBe('Input Excerpt')
    expect(en.admin.riskControl.inputDetailContent).not.toContain('Full')
  })
})
