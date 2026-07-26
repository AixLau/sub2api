import { afterEach, describe, expect, it, vi } from 'vitest'
import { installRipple } from '../ripple'

/**
 * 全局 setup(src/__tests__/setup.ts)把 matchMedia mock 成对所有查询都
 * matches: true,会命中 prefers-reduced-motion 的 no-op 分支。
 * 这里按用例显式 stub matchMedia,并在 afterEach 恢复。
 */
function stubMatchMedia(matches: boolean): () => void {
  const original = window.matchMedia
  window.matchMedia = ((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn()
  })) as unknown as typeof window.matchMedia
  return () => {
    window.matchMedia = original
  }
}

function createBtn(): HTMLButtonElement {
  const btn = document.createElement('button')
  btn.className = 'btn btn-primary'
  document.body.appendChild(btn)
  return btn
}

function clickAt(el: Element, clientX = 10, clientY = 8): void {
  el.dispatchEvent(new MouseEvent('click', { bubbles: true, clientX, clientY }))
}

describe('installRipple', () => {
  let uninstall: (() => void) | null = null
  let restoreMatchMedia: (() => void) | null = null

  afterEach(() => {
    uninstall?.()
    uninstall = null
    restoreMatchMedia?.()
    restoreMatchMedia = null
    document.body.innerHTML = ''
    vi.useRealTimers()
  })

  it('installs without throwing and returns an uninstall function', () => {
    restoreMatchMedia = stubMatchMedia(false)
    expect(() => {
      uninstall = installRipple()
    }).not.toThrow()
    expect(typeof uninstall).toBe('function')
  })

  it('creates a ripple node on .btn click and removes it after animationend', () => {
    restoreMatchMedia = stubMatchMedia(false)
    uninstall = installRipple()
    const btn = createBtn()

    clickAt(btn)

    const wave = btn.querySelector('.ripple-wave')
    expect(wave).not.toBeNull()
    expect(wave!.getAttribute('aria-hidden')).toBe('true')

    wave!.dispatchEvent(new Event('animationend'))
    expect(btn.querySelector('.ripple-wave')).toBeNull()
  })

  it('creates a ripple when the click lands on a child of .btn', () => {
    restoreMatchMedia = stubMatchMedia(false)
    uninstall = installRipple()
    const btn = createBtn()
    const icon = document.createElement('span')
    btn.appendChild(icon)

    clickAt(icon)

    expect(btn.querySelector('.ripple-wave')).not.toBeNull()
  })

  it('removes the ripple via fallback timer when animationend never fires', () => {
    vi.useFakeTimers()
    restoreMatchMedia = stubMatchMedia(false)
    uninstall = installRipple()
    const btn = createBtn()

    clickAt(btn)
    expect(btn.querySelector('.ripple-wave')).not.toBeNull()

    vi.advanceTimersByTime(700)
    expect(btn.querySelector('.ripple-wave')).toBeNull()
  })

  it('does not create a ripple on disabled buttons', () => {
    restoreMatchMedia = stubMatchMedia(false)
    uninstall = installRipple()
    const btn = createBtn()
    btn.disabled = true

    clickAt(btn)

    expect(btn.querySelector('.ripple-wave')).toBeNull()
  })

  it('does not create a ripple on aria-disabled elements', () => {
    restoreMatchMedia = stubMatchMedia(false)
    uninstall = installRipple()
    const link = document.createElement('a')
    link.className = 'btn btn-secondary'
    link.setAttribute('aria-disabled', 'true')
    document.body.appendChild(link)

    clickAt(link)

    expect(link.querySelector('.ripple-wave')).toBeNull()
  })

  it('ignores clicks outside .btn elements', () => {
    restoreMatchMedia = stubMatchMedia(false)
    uninstall = installRipple()
    const plain = document.createElement('div')
    document.body.appendChild(plain)

    clickAt(plain)

    expect(document.querySelector('.ripple-wave')).toBeNull()
  })

  it('is a no-op under prefers-reduced-motion', () => {
    restoreMatchMedia = stubMatchMedia(true)
    uninstall = installRipple()
    const btn = createBtn()

    clickAt(btn)

    expect(btn.querySelector('.ripple-wave')).toBeNull()
  })

  it('stops creating ripples after uninstall', () => {
    restoreMatchMedia = stubMatchMedia(false)
    uninstall = installRipple()
    uninstall()
    uninstall = null
    const btn = createBtn()

    clickAt(btn)

    expect(btn.querySelector('.ripple-wave')).toBeNull()
  })
})
