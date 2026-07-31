import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { userAPI } from '@/api'
import type { RewardGrant } from '@/types'

export const REWARD_QUEUE_OPEN_EVENT = 'reward-queue-open'

const DEFERRED_STORAGE_PREFIX = 'deferred_reward_grants_v1'

function sortGrants(grants: RewardGrant[]): RewardGrant[] {
  return [...grants].sort((left, right) => {
    if (left.priority !== right.priority) return right.priority - left.priority

    const leftExpiry = left.expires_at ? Date.parse(left.expires_at) : Number.POSITIVE_INFINITY
    const rightExpiry = right.expires_at ? Date.parse(right.expires_at) : Number.POSITIVE_INFINITY
    const safeLeftExpiry = Number.isFinite(leftExpiry) ? leftExpiry : Number.POSITIVE_INFINITY
    const safeRightExpiry = Number.isFinite(rightExpiry) ? rightExpiry : Number.POSITIVE_INFINITY
    if (safeLeftExpiry !== safeRightExpiry) return safeLeftExpiry - safeRightExpiry

    return left.grant_id - right.grant_id
  })
}

function storageKey(userID: number): string {
  return `${DEFERRED_STORAGE_PREFIX}:${userID}`
}

function readDeferredGrantIDs(userID: number): Set<number> {
  try {
    const raw = sessionStorage.getItem(storageKey(userID))
    const values = raw ? JSON.parse(raw) : []
    if (!Array.isArray(values)) return new Set()
    return new Set(
      values.filter((value): value is number => Number.isSafeInteger(value) && value > 0)
    )
  } catch {
    return new Set()
  }
}

export const useRewardStore = defineStore('rewards', () => {
  const grants = ref<RewardGrant[]>([])
  const currentGrantID = ref<number | null>(null)
  const loading = ref(false)
  const deferredGrantIDs = ref<Set<number>>(new Set())

  let activeUserID: number | null = null
  let fetchGeneration = 0
  let inFlightFetch: Promise<void> | null = null
  const viewedGrantIDs = new Set<number>()

  const pendingCount = computed(() => grants.value.length)
  const currentGrant = computed(
    () => grants.value.find((grant) => grant.grant_id === currentGrantID.value) ?? null
  )

  function persistDeferredGrantIDs() {
    if (activeUserID === null) return
    try {
      sessionStorage.setItem(
        storageKey(activeUserID),
        JSON.stringify([...deferredGrantIDs.value].sort((a, b) => a - b))
      )
    } catch {
      // Session storage is an enhancement; the in-memory queue remains usable.
    }
  }

  function setActiveUser(userID: number) {
    if (activeUserID === userID) return
    activeUserID = userID
    grants.value = []
    currentGrantID.value = null
    deferredGrantIDs.value = readDeferredGrantIDs(userID)
    viewedGrantIDs.clear()
    fetchGeneration++
    inFlightFetch = null
    loading.value = false
  }

  function reportView(grantID: number) {
    if (viewedGrantIDs.has(grantID)) return
    viewedGrantIDs.add(grantID)
    userAPI.viewReward(grantID).catch((error) => {
      viewedGrantIDs.delete(grantID)
      console.error('Failed to record reward view:', error)
    })
  }

  function showGrant(grant: RewardGrant) {
    currentGrantID.value = grant.grant_id
    reportView(grant.grant_id)
  }

  function openNext() {
    if (currentGrant.value) return
    const next = grants.value.find((grant) => !deferredGrantIDs.value.has(grant.grant_id))
    if (next) showGrant(next)
  }

  async function fetchPending(userID: number, force = false): Promise<void> {
    setActiveUser(userID)
    if (inFlightFetch && !force) return inFlightFetch

    const requestGeneration = ++fetchGeneration
    const requestUserID = userID
    const request = (async () => {
      loading.value = true
      try {
        const pending = await userAPI.getPendingRewards()
        if (requestGeneration !== fetchGeneration || activeUserID !== requestUserID) return

        grants.value = sortGrants(pending)
        const pendingIDs = new Set(grants.value.map((grant) => grant.grant_id))
        deferredGrantIDs.value = new Set(
          [...deferredGrantIDs.value].filter((grantID) => pendingIDs.has(grantID))
        )
        persistDeferredGrantIDs()

        if (currentGrantID.value !== null && !pendingIDs.has(currentGrantID.value)) {
          currentGrantID.value = null
        }
        openNext()
      } finally {
        if (requestGeneration === fetchGeneration) {
          loading.value = false
          inFlightFetch = null
        }
      }
    })()

    inFlightFetch = request
    return request
  }

  function deferCurrent() {
    const grant = currentGrant.value
    if (!grant) return
    deferredGrantIDs.value = new Set(deferredGrantIDs.value).add(grant.grant_id)
    currentGrantID.value = null
    persistDeferredGrantIDs()
    openNext()
  }

  function reopen() {
    if (currentGrant.value) return
    const grant = grants.value[0]
    if (!grant) return
    const nextDeferred = new Set(deferredGrantIDs.value)
    nextDeferred.delete(grant.grant_id)
    deferredGrantIDs.value = nextDeferred
    persistDeferredGrantIDs()
    showGrant(grant)
  }

  function finishCurrent() {
    const grant = currentGrant.value
    if (!grant) return
    grants.value = grants.value.filter((candidate) => candidate.grant_id !== grant.grant_id)
    const nextDeferred = new Set(deferredGrantIDs.value)
    nextDeferred.delete(grant.grant_id)
    deferredGrantIDs.value = nextDeferred
    currentGrantID.value = null
    persistDeferredGrantIDs()
    openNext()
  }

  function reset() {
    activeUserID = null
    grants.value = []
    currentGrantID.value = null
    deferredGrantIDs.value = new Set()
    viewedGrantIDs.clear()
    fetchGeneration++
    inFlightFetch = null
    loading.value = false
  }

  return {
    grants,
    currentGrant,
    loading,
    pendingCount,
    fetchPending,
    deferCurrent,
    reopen,
    finishCurrent,
    reset,
  }
})
