/**
 * confetti 封装冒烟测试
 *
 * 注意：全局 setup.ts 把 matchMedia mock 成对任何 query 都返回 matches:true，
 * 会命中 prefers-reduced-motion 分支导致 confetti 正确地 no-op。
 * 这里按需覆写为 matches:false 以走到真实喷射路径。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const confettiMock = vi.hoisted(() => vi.fn())

vi.mock('canvas-confetti', () => ({
  default: confettiMock
}))

import { fireConfetti, fireCelebration } from '../confetti'

const originalMatchMedia = window.matchMedia

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn()
  })) as unknown as typeof window.matchMedia
})

afterEach(() => {
  window.matchMedia = originalMatchMedia
  confettiMock.mockClear()
  vi.useRealTimers()
})

describe('confetti', () => {
  it('fireConfetti 在 jsdom 下调用不抛错，并触发底层 confetti', async () => {
    expect(() => fireConfetti({ particleCount: 10 })).not.toThrow()
    // 内部为懒加载 + 异步执行，等待微任务落定
    await vi.waitFor(() => {
      expect(confettiMock).toHaveBeenCalledWith(
        expect.objectContaining({ particleCount: 10 })
      )
    })
  })

  it('fireCelebration 在 jsdom 下调用不抛错', async () => {
    vi.useFakeTimers()
    expect(() => fireCelebration()).not.toThrow()

    // 等懒加载 promise 完成后推进定时器，确保延时喷射也不抛错
    await vi.advanceTimersByTimeAsync(2000)
    expect(confettiMock).toHaveBeenCalled()
  })
})
