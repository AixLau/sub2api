import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useRewardStore } from '@/stores/rewards'
import type { RewardGrant } from '@/types'

const apiMocks = vi.hoisted(() => ({
  getPendingRewards: vi.fn(),
  viewReward: vi.fn(),
}))

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

vi.mock('@/api', () => ({
  userAPI: apiMocks,
}))

function grant(
  grantID: number,
  priority: number,
  expiresAt: string | null
): RewardGrant {
  return {
    grant_id: grantID,
    campaign_id: 10,
    title: `Reward ${grantID}`,
    hint: '',
    cover_text: '',
    skin: null,
    priority,
    expires_at: expiresAt,
  }
}

describe('useRewardStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    sessionStorage.clear()
    apiMocks.getPendingRewards.mockReset()
    apiMocks.viewReward.mockReset()
    apiMocks.viewReward.mockResolvedValue(undefined)
  })

  it('sorts by priority, then expiry, and opens only the first grant', async () => {
    apiMocks.getPendingRewards.mockResolvedValue([
      grant(1, 3, '2026-08-04T00:00:00Z'),
      grant(2, 8, null),
      grant(3, 8, '2026-08-02T00:00:00Z'),
      grant(4, 8, '2026-08-01T00:00:00Z'),
    ])
    const store = useRewardStore()

    await store.fetchPending(42)

    expect(store.grants.map((item) => item.grant_id)).toEqual([4, 3, 2, 1])
    expect(store.currentGrant?.grant_id).toBe(4)
    expect(store.pendingCount).toBe(4)
    expect(apiMocks.viewReward).toHaveBeenCalledWith(4)
    expect(apiMocks.viewReward).toHaveBeenCalledTimes(1)
  })

  it('defers grants for the current browser session and reopens the queue head', async () => {
    apiMocks.getPendingRewards.mockResolvedValue([
      grant(1, 10, null),
      grant(2, 5, null),
    ])
    const store = useRewardStore()
    await store.fetchPending(42)

    store.deferCurrent()
    expect(store.currentGrant?.grant_id).toBe(2)
    store.deferCurrent()
    expect(store.currentGrant).toBeNull()

    store.reopen()
    expect(store.currentGrant?.grant_id).toBe(1)
    expect(sessionStorage.getItem('deferred_reward_grants_v1:42')).toBe('[2]')
  })

  it('restores deferred grant ids after the store is recreated', async () => {
    sessionStorage.setItem('deferred_reward_grants_v1:42', '[1]')
    apiMocks.getPendingRewards.mockResolvedValue([
      grant(1, 10, null),
      grant(2, 5, null),
    ])
    const store = useRewardStore()

    await store.fetchPending(42)

    expect(store.currentGrant?.grant_id).toBe(2)
  })

  it('removes a completed grant and advances the queue', async () => {
    apiMocks.getPendingRewards.mockResolvedValue([
      grant(1, 10, null),
      grant(2, 5, null),
    ])
    const store = useRewardStore()
    await store.fetchPending(42)

    store.finishCurrent()

    expect(store.pendingCount).toBe(1)
    expect(store.currentGrant?.grant_id).toBe(2)
  })

  it('keeps the newest forced fetch when responses finish out of order', async () => {
    const first = deferred<RewardGrant[]>()
    const second = deferred<RewardGrant[]>()
    apiMocks.getPendingRewards
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)
    const store = useRewardStore()

    const firstRequest = store.fetchPending(42)
    const secondRequest = store.fetchPending(42, true)
    second.resolve([grant(2, 10, null)])
    await secondRequest
    first.resolve([grant(1, 20, null)])
    await firstRequest

    expect(store.grants.map((item) => item.grant_id)).toEqual([2])
    expect(store.currentGrant?.grant_id).toBe(2)
    expect(store.loading).toBe(false)
  })

  it('ignores a pending response from the previous user', async () => {
    const firstUser = deferred<RewardGrant[]>()
    const secondUser = deferred<RewardGrant[]>()
    apiMocks.getPendingRewards
      .mockImplementationOnce(() => firstUser.promise)
      .mockImplementationOnce(() => secondUser.promise)
    const store = useRewardStore()

    const firstRequest = store.fetchPending(42)
    const secondRequest = store.fetchPending(43)
    firstUser.resolve([grant(1, 20, null)])
    await firstRequest
    expect(store.grants).toEqual([])

    secondUser.resolve([grant(2, 10, null)])
    await secondRequest
    expect(store.grants.map((item) => item.grant_id)).toEqual([2])
    expect(store.currentGrant?.grant_id).toBe(2)
  })

  it('does not revive the queue when a request completes after reset', async () => {
    const pending = deferred<RewardGrant[]>()
    apiMocks.getPendingRewards.mockImplementationOnce(() => pending.promise)
    const store = useRewardStore()

    const request = store.fetchPending(42)
    store.reset()
    pending.resolve([grant(1, 20, null)])
    await request

    expect(store.grants).toEqual([])
    expect(store.currentGrant).toBeNull()
    expect(store.loading).toBe(false)
  })
})
