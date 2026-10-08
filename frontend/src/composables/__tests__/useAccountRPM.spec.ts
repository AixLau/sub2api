import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { getRPM } from '@/api/admin/accounts'
import { useAccountRPM } from '../useAccountRPM'

vi.mock('@/api/admin/accounts', () => ({ getRPM: vi.fn() }))
let scope: EffectScope
const mockGet = vi.mocked(getRPM)
function visibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: state })
  document.dispatchEvent(new Event('visibilitychange'))
}
function setup() {
  const ids = ref([42, 43])
  const visible = ref(true)
  scope = effectScope()
  const rpm = scope.run(() => useAccountRPM(ids, visible))!
  return { ids, visible, ...rpm }
}
beforeEach(() => {
  vi.useFakeTimers()
  visibility('visible')
  mockGet.mockReset().mockResolvedValue({ rpm: { '42': 7, '43': 0 } })
})
afterEach(() => { scope?.stop(); vi.useRealTimers(); vi.restoreAllMocks() })

describe('account RPM polling', () => {
  it('loads immediately and every five seconds without a list refresh', async () => {
    const rpm = setup()
    expect(rpm.loading.value).toBe(true)
    await flushPromises()
    expect(rpm.values.value).toEqual({ '42': 7, '43': 0 })
    await vi.advanceTimersByTimeAsync(4999)
    expect(mockGet).toHaveBeenCalledTimes(1)
    mockGet.mockResolvedValue({ rpm: { '42': 0, '43': 0 } })
    await vi.advanceTimersByTimeAsync(1)
    expect(mockGet).toHaveBeenCalledTimes(2)
    expect(rpm.values.value['42']).toBe(0)
  })
  it('pauses when hidden, empty or unmounted, and immediately refreshes on resume', async () => {
    const rpm = setup(); await flushPromises()
    rpm.visible.value = false; await nextTick()
    await vi.advanceTimersByTimeAsync(10000)
    expect(mockGet).toHaveBeenCalledTimes(1)
    rpm.visible.value = true; await flushPromises()
    expect(mockGet).toHaveBeenCalledTimes(2)
    visibility('hidden'); await nextTick()
    await vi.advanceTimersByTimeAsync(10000)
    expect(mockGet).toHaveBeenCalledTimes(2)
    visibility('visible'); await flushPromises()
    expect(mockGet).toHaveBeenCalledTimes(3)
    rpm.ids.value = []; await nextTick()
    await vi.advanceTimersByTimeAsync(5000)
    expect(mockGet).toHaveBeenCalledTimes(3)
    scope.stop(); await vi.advanceTimersByTimeAsync(5000)
    expect(mockGet).toHaveBeenCalledTimes(3)
  })
  it('cancels old pages and ignores late responses without overlapping polls', async () => {
    let resolveOld!: (value: { rpm: Record<string, number> }) => void
    mockGet.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const rpm = setup()
    const oldSignal = mockGet.mock.calls[0][1]!.signal!
    await vi.advanceTimersByTimeAsync(10000)
    expect(mockGet).toHaveBeenCalledTimes(1)
    mockGet.mockResolvedValue({ rpm: { '44': 2 } })
    rpm.ids.value = [44]; await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(rpm.values.value).toEqual({ '44': 2 })
    resolveOld({ rpm: { '42': 99 } }); await flushPromises()
    expect(rpm.values.value).toEqual({ '44': 2 })
  })
  it('distinguishes unavailable data from zero and recovers on the next poll', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const rpm = setup(); await flushPromises()
    mockGet.mockRejectedValueOnce(new Error('unavailable'))
    await rpm.refresh()
    expect(rpm.values.value).toEqual({})
    expect(rpm.loading.value).toBe(false)
    mockGet.mockResolvedValue({ rpm: { '42': 0 } })
    await vi.advanceTimersByTimeAsync(5000)
    expect(rpm.values.value).toEqual({ '42': 0 })
  })
})
