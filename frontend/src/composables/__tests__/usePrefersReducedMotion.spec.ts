import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  REDUCED_MOTION_STORAGE_KEY,
  initializeMotionPreference,
  setApplicationReducedMotion,
  usePrefersReducedMotion
} from '@/composables/usePrefersReducedMotion'

function mockMatchMedia(matches: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: query === '(prefers-reduced-motion: reduce)' ? matches : false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
}

describe('usePrefersReducedMotion', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('reduce-motion')
    setApplicationReducedMotion(false)
    mockMatchMedia(false)
    initializeMotionPreference()
  })

  it('persists the application override and syncs the root class', () => {
    const { applicationReducedMotion, prefersReducedMotion } = usePrefersReducedMotion()

    setApplicationReducedMotion(true)

    expect(applicationReducedMotion.value).toBe(true)
    expect(prefersReducedMotion.value).toBe(true)
    expect(localStorage.getItem(REDUCED_MOTION_STORAGE_KEY)).toBe('true')
    expect(document.documentElement.classList.contains('reduce-motion')).toBe(true)

    setApplicationReducedMotion(false)

    expect(localStorage.getItem(REDUCED_MOTION_STORAGE_KEY)).toBeNull()
    expect(document.documentElement.classList.contains('reduce-motion')).toBe(false)
  })

  it('combines the device preference with the application override', () => {
    mockMatchMedia(true)
    initializeMotionPreference()

    const { applicationReducedMotion, systemReducedMotion, prefersReducedMotion } =
      usePrefersReducedMotion()

    expect(applicationReducedMotion.value).toBe(false)
    expect(systemReducedMotion.value).toBe(true)
    expect(prefersReducedMotion.value).toBe(true)
    expect(document.documentElement.classList.contains('reduce-motion')).toBe(true)
  })
})
