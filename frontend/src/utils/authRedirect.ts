const AUTH_REDIRECT_BASE = 'https://sub2api.local'
const DEFAULT_AUTH_REDIRECT = '/dashboard'

export function resolveAuthRedirectPath(
  value: unknown,
  fallback = DEFAULT_AUTH_REDIRECT
): string {
  const rawValue = Array.isArray(value)
    ? value.find((item): item is string => typeof item === 'string')
    : value
  if (typeof rawValue !== 'string') {
    return fallback
  }

  const candidate = rawValue.trim()
  if (!candidate.startsWith('/') || candidate.startsWith('//')) {
    return fallback
  }

  try {
    const base = new URL(AUTH_REDIRECT_BASE)
    const target = new URL(candidate, base)
    if (target.origin !== base.origin) {
      return fallback
    }
    return `${target.pathname}${target.search}${target.hash}`
  } catch {
    return fallback
  }
}
