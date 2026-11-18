import { describe, expect, it } from 'vitest'
import { buildWorkBuddyModels } from '../workbuddyConfig'

const manifest = JSON.stringify({
  models: [
    {
      slug: 'gpt-6.1-terra',
      display_name: 'GPT-6.1 Terra',
      visibility: 'list',
      supported_in_api: true,
      context_window: 500000,
      max_context_window: 872000,
      input_modalities: ['text', 'image'],
      default_reasoning_level: 'low',
      supported_reasoning_levels: [
        { effort: 'none' },
        { effort: 'low' },
        { effort: 'medium' },
        { effort: 'high' }
      ]
    },
    {
      slug: 'gpt-6-astra',
      display_name: 'GPT-6 Astra',
      context_window: 200000,
      input_modalities: ['text'],
      default_reasoning_level: 'none',
      supported_reasoning_levels: [{ effort: 'medium' }, { effort: 'xhigh' }]
    },
    { slug: 'hidden-model', visibility: 'hide' },
    { slug: 'api-off', supported_in_api: false },
    { slug: 'gpt-6.1-terra', display_name: 'duplicate' },
    { display_name: 'no slug' }
  ]
})

const options = { apiBase: 'https://aixlau.me/v1/', apiKey: 'sk-test' }

describe('buildWorkBuddyModels', () => {
  it('converts catalog entries into the WorkBuddy models.json array shape', () => {
    const models = buildWorkBuddyModels(manifest, options)
    expect(models.map((model) => model.id)).toEqual(['gpt-6.1-terra', 'gpt-6-astra'])

    expect(models[0]).toEqual({
      apiKey: 'sk-test',
      contextWindow: { defaultLength: 872000, supportedLengths: [500000, 872000] },
      id: 'gpt-6.1-terra',
      maxInputTokens: 872000,
      maxOutputTokens: 128000,
      name: 'GPT-6.1 Terra · 星链AI',
      reasoning: {
        canDisableThinking: true,
        defaultEffort: 'low',
        supportedEfforts: ['low', 'medium', 'high']
      },
      supportsImages: true,
      supportsReasoning: true,
      supportsToolCall: true,
      url: 'https://aixlau.me/v1/chat/completions',
      vendor: '星链AI'
    })
  })

  it('falls back to a supported default effort and single context length', () => {
    const astra = buildWorkBuddyModels(manifest, options)[1]
    expect(astra.contextWindow).toEqual({ defaultLength: 200000, supportedLengths: [200000] })
    expect(astra.reasoning).toEqual({
      canDisableThinking: false,
      defaultEffort: 'medium',
      supportedEfforts: ['medium', 'xhigh']
    })
    expect(astra.supportsImages).toBe(false)
  })

  it('omits reasoning for models without reasoning levels', () => {
    const [model] = buildWorkBuddyModels(JSON.stringify({ models: [{ slug: 'gpt-6.2' }] }), options)
    expect(model.reasoning).toBeUndefined()
    expect(model.supportsReasoning).toBe(false)
  })

  it('keeps only the GPT-6, 6.1 and 6.2 series', () => {
    const slugs = ['gpt-6', 'gpt-6-luna', 'gpt-6.1-sol', 'gpt-6.2-astra', 'gpt-6.3', 'gpt-60', 'gpt-5.6-luna', 'claude-opus-4-8']
    const models = buildWorkBuddyModels(JSON.stringify({ models: slugs.map((slug) => ({ slug })) }), options)
    expect(models.map((model) => model.id)).toEqual(['gpt-6', 'gpt-6-luna', 'gpt-6.1-sol', 'gpt-6.2-astra'])
  })

  it('returns an empty list for invalid manifests', () => {
    expect(buildWorkBuddyModels('', options)).toEqual([])
    expect(buildWorkBuddyModels('not json', options)).toEqual([])
    expect(buildWorkBuddyModels('{"data":[]}', options)).toEqual([])
  })
})
