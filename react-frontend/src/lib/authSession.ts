type PersistedUser = {
  role?: unknown
}

export function getPersistedDashboardPath(): '/admin/dashboard' | '/dashboard' | null {
  try {
    const token = localStorage.getItem('auth_token')
    const savedUser = localStorage.getItem('auth_user')

    if (!token?.trim() || !savedUser) {
      return null
    }

    const user = JSON.parse(savedUser) as PersistedUser | null
    if (!user || typeof user !== 'object') {
      return null
    }

    return user.role === 'admin' ? '/admin/dashboard' : '/dashboard'
  } catch {
    return null
  }
}
