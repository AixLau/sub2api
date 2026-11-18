import { PROVIDER_DISPLAY_NAME } from '@/utils/providerBrand'

/**
 * WorkBuddy 的 ~/.workbuddy/models.json 是一个顶层数组，每项描述一个
 * OpenAI Chat Completions 兼容的自定义模型。这里把网关返回的 Codex 模型
 * 目录（/v1/models?client_version=...）转换成这个格式，模型 ID、上下文窗口、
 * 推理档位都取自当前分组的真实目录，避免手写列表与分组不一致。
 */
export interface WorkBuddyModel {
  apiKey: string
  contextWindow: {
    defaultLength: number
    supportedLengths: number[]
  }
  id: string
  maxInputTokens: number
  maxOutputTokens: number
  name: string
  reasoning?: {
    canDisableThinking: boolean
    defaultEffort: string
    supportedEfforts: string[]
  }
  supportsImages: boolean
  supportsReasoning: boolean
  supportsToolCall: boolean
  url: string
  vendor: string
}

export interface WorkBuddyConfigOptions {
  /** 以 /v1 结尾的网关地址 */
  apiBase: string
  apiKey: string
}

// Codex 目录不含输出上限；与 WorkBuddy 内置配置保持一致。
const DEFAULT_MAX_OUTPUT_TOKENS = 128000
const DEFAULT_CONTEXT_WINDOW = 200000

// 只给 WorkBuddy 推当前主力的 GPT-6 / 6.1 / 6.2 系列（如 gpt-6-astra、gpt-6.1-terra），
// 旧系列和其他厂商模型不进列表，避免小白用户在一长串模型里选错。
// public/workbuddy/setup.sh 与 setup.ps1 的模型规则必须保持一致。
export const WORKBUDDY_MODEL_PATTERN = /^gpt-6(?:\.[12])?(?:-|$)/i

export function isWorkBuddyModel(slug: string): boolean {
  return WORKBUDDY_MODEL_PATTERN.test(slug.trim())
}

interface ManifestModel {
  slug?: unknown
  display_name?: unknown
  visibility?: unknown
  supported_in_api?: unknown
  context_window?: unknown
  max_context_window?: unknown
  input_modalities?: unknown
  default_reasoning_level?: unknown
  supported_reasoning_levels?: unknown
}

function str(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function positiveInt(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0
}

function readEfforts(levels: unknown): string[] {
  if (!Array.isArray(levels)) return []
  const efforts: string[] = []
  for (const level of levels) {
    const effort = str((level as { effort?: unknown } | null)?.effort)
    if (effort && !efforts.includes(effort)) efforts.push(effort)
  }
  return efforts
}

function toWorkBuddyModel(model: ManifestModel, options: WorkBuddyConfigOptions): WorkBuddyModel | null {
  const id = str(model.slug)
  if (!id || !isWorkBuddyModel(id)) return null
  if (str(model.visibility) === 'hide' || model.supported_in_api === false) return null

  const contextWindow = positiveInt(model.context_window) || DEFAULT_CONTEXT_WINDOW
  const maxContextWindow = Math.max(positiveInt(model.max_context_window), contextWindow)
  // 与 WorkBuddy 内置配置一致：默认用最大窗口，同时允许切回标准窗口。
  const supportedLengths = maxContextWindow > contextWindow ? [contextWindow, maxContextWindow] : [contextWindow]

  const allEfforts = readEfforts(model.supported_reasoning_levels)
  const supportedEfforts = allEfforts.filter((effort) => effort !== 'none')
  const configuredDefault = str(model.default_reasoning_level)
  const defaultEffort = supportedEfforts.includes(configuredDefault)
    ? configuredDefault
    : supportedEfforts.includes('medium')
      ? 'medium'
      : supportedEfforts[0] ?? ''

  const modalities = Array.isArray(model.input_modalities) ? model.input_modalities.map(str) : ['text', 'image']
  const displayName = str(model.display_name) || id

  const result: WorkBuddyModel = {
    apiKey: options.apiKey,
    contextWindow: {
      defaultLength: maxContextWindow,
      supportedLengths
    },
    id,
    maxInputTokens: maxContextWindow,
    maxOutputTokens: DEFAULT_MAX_OUTPUT_TOKENS,
    name: `${displayName} · ${PROVIDER_DISPLAY_NAME}`,
    supportsImages: modalities.includes('image'),
    supportsReasoning: supportedEfforts.length > 0,
    supportsToolCall: true,
    url: `${options.apiBase.replace(/\/+$/, '')}/chat/completions`,
    vendor: PROVIDER_DISPLAY_NAME
  }
  if (supportedEfforts.length > 0) {
    result.reasoning = {
      canDisableThinking: allEfforts.includes('none'),
      defaultEffort,
      supportedEfforts
    }
  }
  return result
}

export function buildWorkBuddyModels(
  manifestContent: string | null | undefined,
  options: WorkBuddyConfigOptions
): WorkBuddyModel[] {
  if (!manifestContent) return []
  let payload: unknown
  try {
    payload = JSON.parse(manifestContent)
  } catch {
    return []
  }
  const models = (payload as { models?: unknown } | null)?.models
  if (!Array.isArray(models)) return []

  const seen = new Set<string>()
  const result: WorkBuddyModel[] = []
  for (const model of models) {
    if (typeof model !== 'object' || model === null) continue
    const converted = toWorkBuddyModel(model as ManifestModel, options)
    if (!converted || seen.has(converted.id)) continue
    seen.add(converted.id)
    result.push(converted)
  }
  return result
}

export function buildWorkBuddyModelsJson(
  manifestContent: string | null | undefined,
  options: WorkBuddyConfigOptions
): string {
  return JSON.stringify(buildWorkBuddyModels(manifestContent, options), null, 2)
}
