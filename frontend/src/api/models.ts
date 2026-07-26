/**
 * Public model catalog API (no auth required).
 * Backend route: GET /api/v1/models/public
 */

import { apiClient } from './client'

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

export async function fetchPublicModels(signal?: AbortSignal): Promise<PublicModel[]> {
  const { data } = await apiClient.get<{ models?: PublicModel[] }>('/models/public', { signal })
  return Array.isArray(data?.models) ? data.models : []
}
