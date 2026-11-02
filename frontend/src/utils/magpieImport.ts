import type { GroupPlatform } from '@/types'
import { PROVIDER_ICON_URL } from '@/utils/providerBrand'

/** Provider name shown in Magpie. */
export const MAGPIE_PROVIDER_NAME = '星链AI'
/** Stable id agents use as `<id>/<model>`; matches the setup scripts' provider id. */
export const MAGPIE_PROVIDER_ID = 'xinglian'

export interface MagpieImportLinkInput {
  /** Gateway address (api_base_url or the page origin), with or without /v1. */
  baseUrl: string
  /** Console origin used for the website and key-management links. */
  siteOrigin: string
  platform?: GroupPlatform | null
  apiKey: string
}

/** models.dev provider ids that describe what each group platform serves. */
const MAGPIE_CATALOGS: Partial<Record<GroupPlatform, string>> = {
  anthropic: 'anthropic',
  openai: 'openai',
  gemini: 'google',
  grok: 'xai',
  deepseek: 'deepseek'
}

function gatewayRoot(baseUrl: string): string {
  return baseUrl.replace(/\/+$/, '').replace(/\/v1$/, '')
}

/** Platforms whose keys are not LLM chat keys and so cannot back a Magpie provider. */
export function supportsMagpieImport(platform?: GroupPlatform | null): boolean {
  return platform !== 'typesafe'
}

/**
 * "Add to magpie" web link (https://usemagpie.ai/import#…). Parameters live in
 * the fragment, so the key never reaches usemagpie.ai's server. Every group
 * accepts /v1/chat/completions, /v1/responses and /v1/messages (the gateway
 * converts between them), so each agent in Magpie can use its native API.
 */
export function buildMagpieImportLink(input: MagpieImportLinkInput): string {
  const root = gatewayRoot(input.baseUrl)
  const siteOrigin = input.siteOrigin.replace(/\/+$/, '')
  const platform = input.platform || 'anthropic'
  const params = new URLSearchParams({ name: MAGPIE_PROVIDER_NAME, id: MAGPIE_PROVIDER_ID })

  switch (platform) {
    case 'gemini':
      params.set('gemini', `${root}/v1beta`)
      break
    case 'antigravity':
      params.set('anthropic', `${root}/antigravity`)
      params.set('gemini', `${root}/antigravity/v1beta`)
      break
    default:
      params.set('chat', `${root}/v1`)
      params.set('responses', `${root}/v1`)
      params.set('anthropic', root)
  }

  params.set('key', input.apiKey)
  const catalog = MAGPIE_CATALOGS[platform]
  if (catalog) params.set('catalog', catalog)
  params.set('website', siteOrigin)
  params.set('keys', `${siteOrigin}/keys`)
  params.set('icon', PROVIDER_ICON_URL)

  return `https://usemagpie.ai/import#${params.toString()}`
}
