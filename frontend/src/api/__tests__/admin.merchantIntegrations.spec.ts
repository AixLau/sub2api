import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, del } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
    delete: del
  }
}))

import {
  remove,
  removeEndpoint,
  testEndpoint,
  type MerchantEndpointTestInput
} from '@/api/admin/merchantIntegrations'

type Assert<T extends true> = T
type IsExact<T, U> = (
  (<G>() => G extends T ? 1 : 2) extends (<G>() => G extends U ? 1 : 2)
    ? ((<G>() => G extends U ? 1 : 2) extends (<G>() => G extends T ? 1 : 2) ? true : false)
    : false
)

const testInputContractExact: Assert<
  IsExact<
    MerchantEndpointTestInput,
    {
      user_id?: number
      start_time?: string
      end_time?: string
    }
  >
> = true

describe('admin merchant integrations api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    del.mockReset()
  })

  it('sends user_id and time bounds to the merchant endpoint test request', async () => {
    const payload: MerchantEndpointTestInput = {
      user_id: 42,
      start_time: '2026-08-05T02:00:00.000Z',
      end_time: '2026-08-05T03:00:00.000Z'
    }
    post.mockResolvedValueOnce({
      data: {
        endpoint_id: 9,
        http_status: 200,
        successful: true,
        message: 'ok'
      }
    })

    const result = await testEndpoint(7, 9, payload)

    expect(post).toHaveBeenCalledWith('/admin/merchant-integrations/7/endpoints/9/test', payload)
    expect(result).toEqual({
      endpoint_id: 9,
      http_status: 200,
      successful: true,
      message: 'ok'
    })
    expect(testInputContractExact).toBe(true)
  })

  it('calls the backend delete endpoints for integrations and endpoints', async () => {
    del.mockResolvedValueOnce({ data: undefined })
    del.mockResolvedValueOnce({ data: undefined })

    await remove(11)
    await removeEndpoint(11, 23)

    expect(del).toHaveBeenNthCalledWith(1, '/admin/merchant-integrations/11')
    expect(del).toHaveBeenNthCalledWith(2, '/admin/merchant-integrations/11/endpoints/23')
  })
})
