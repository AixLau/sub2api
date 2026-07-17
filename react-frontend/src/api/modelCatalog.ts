export type PublicModelPricing = {
  billing_mode: 'token' | 'per_request' | 'image' | 'video' | string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: Array<{
    min_tokens: number
    max_tokens: number | null
    tier_label?: string
    input_price: number | null
    output_price: number | null
    cache_write_price: number | null
    cache_read_price: number | null
    per_request_price: number | null
  }>
}

export type PublicModel = {
  name: string
  platform: string
  pricing: PublicModelPricing | null
}

type ModelCatalogEnvelope = {
  code: number
  message?: string
  data?: {
    models?: PublicModel[]
  }
}

export async function fetchPublicModels(signal?: AbortSignal): Promise<PublicModel[]> {
  const response = await fetch('/api/v1/models/public', {
    signal,
    headers: { Accept: 'application/json' },
  })
  const payload = (await response.json().catch(() => null)) as ModelCatalogEnvelope | null

  if (!response.ok || !payload || payload.code !== 0 || !Array.isArray(payload.data?.models)) {
    throw new Error(payload?.message || '模型目录暂时无法加载')
  }

  return payload.data.models
}
