import { describe, expect, it } from 'vitest'

import { resolveAuthRedirectPath } from '@/utils/authRedirect'

describe('resolveAuthRedirectPath', () => {
  it('preserves an internal client setup path with its query string', () => {
    const redirect = '/client-setup?setup_id=setup-123&device_code=ABCD-1234&client=codex'

    expect(resolveAuthRedirectPath(redirect)).toBe(redirect)
  })

  it('uses the first string from a router query array', () => {
    expect(resolveAuthRedirectPath([null, '/profile'])).toBe('/profile')
  })

  it.each([
    'https://example.com/client-setup',
    '//example.com/client-setup',
    '/\\example.com/client-setup',
    'javascript:alert(1)',
    '',
  ])('rejects unsafe or empty redirect %s', (redirect) => {
    expect(resolveAuthRedirectPath(redirect)).toBe('/dashboard')
  })
})
