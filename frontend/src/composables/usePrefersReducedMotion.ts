import { computed, readonly, ref } from 'vue'

export const REDUCED_MOTION_STORAGE_KEY = 'sub2api-reduced-motion'

const applicationReducedMotion = ref(false)
const systemReducedMotion = ref(false)
const prefersReducedMotion = computed(
  () => applicationReducedMotion.value || systemReducedMotion.value
)

let initialized = false
let mediaQuery: MediaQueryList | null = null

function readStoredPreference(): boolean {
  try {
    return typeof localStorage !== 'undefined'
      && localStorage.getItem(REDUCED_MOTION_STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

function readSystemPreference(): boolean {
  try {
    return typeof window !== 'undefined'
      && typeof window.matchMedia === 'function'
      && window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

function syncRootClass(): void {
  if (typeof document !== 'undefined') {
    document.documentElement.classList.toggle('reduce-motion', prefersReducedMotion.value)
  }
}

function handleSystemPreference(event: MediaQueryListEvent): void {
  systemReducedMotion.value = event.matches
  syncRootClass()
}

function handleStorage(event: StorageEvent): void {
  if (event.key !== REDUCED_MOTION_STORAGE_KEY) return
  applicationReducedMotion.value = event.newValue === 'true'
  syncRootClass()
}

export function initializeMotionPreference(): void {
  applicationReducedMotion.value = readStoredPreference()
  systemReducedMotion.value = readSystemPreference()

  if (!initialized && typeof window !== 'undefined') {
    initialized = true
    if (typeof window.matchMedia === 'function') {
      mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
      if (typeof mediaQuery.addEventListener === 'function') {
        mediaQuery.addEventListener('change', handleSystemPreference)
      } else {
        mediaQuery.addListener(handleSystemPreference)
      }
    }
    window.addEventListener('storage', handleStorage)
  }

  syncRootClass()
}

export function setApplicationReducedMotion(enabled: boolean): void {
  applicationReducedMotion.value = enabled
  try {
    if (enabled) {
      localStorage.setItem(REDUCED_MOTION_STORAGE_KEY, 'true')
    } else {
      localStorage.removeItem(REDUCED_MOTION_STORAGE_KEY)
    }
  } catch {
    // The in-memory preference still applies when storage is unavailable.
  }
  syncRootClass()
}

export function isReducedMotionPreferred(): boolean {
  return applicationReducedMotion.value || readStoredPreference() || readSystemPreference()
}

export function usePrefersReducedMotion() {
  initializeMotionPreference()
  return {
    applicationReducedMotion: readonly(applicationReducedMotion),
    systemReducedMotion: readonly(systemReducedMotion),
    prefersReducedMotion: readonly(prefersReducedMotion),
    setApplicationReducedMotion
  }
}
