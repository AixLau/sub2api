type ApiEnvelope<T> = {
  code?: number
  message?: string
  data?: T
}

type Sub2ApiUser = {
  id: number
  username: string
  email: string
  role: 'admin' | 'user'
  [key: string]: unknown
}

type AuthResponse = {
  access_token: string
  refresh_token?: string
  expires_in?: number
  welcome_reward?: number
  user: Sub2ApiUser
}

type VerifyCodeResponse = {
  message?: string
  countdown?: number
}

export type AuthResult =
  | { ok: true; redirectTo: string }
  | { ok: false; message: string }

export type VerifyCodeResult =
  | { ok: true; countdown: number; message: string }
  | { ok: false; message: string }

const API_BASE_URL = '/api/v1'
const emailVerifyRequiredMessage = '请先获取邮箱验证码并填写后再注册。'
const invalidEmailMessage = '请输入有效的邮箱地址。'

function normalizeAuthErrorMessage(message: string, fallback: string): string {
  const normalized = message.trim()
  const lowerMessage = normalized.toLowerCase()

  if (
    lowerMessage.includes('email verification is required') ||
    lowerMessage.includes('email_verify_required')
  ) {
    return emailVerifyRequiredMessage
  }

  if (
    lowerMessage.includes("failed on the 'email' tag") ||
    (lowerMessage.includes('field validation for') && lowerMessage.includes('email'))
  ) {
    return invalidEmailMessage
  }

  return normalized || fallback
}

function unwrapApiResponse<T>(payload: ApiEnvelope<T> | T): T {
  if (payload && typeof payload === 'object' && 'code' in payload) {
    const envelope = payload as ApiEnvelope<T>
    if (envelope.code === 0 && envelope.data) {
      return envelope.data
    }

    throw new Error(envelope.message || '请求失败，请稍后再试。')
  }

  return payload as T
}

function persistAuth(response: AuthResponse): string {
  localStorage.setItem('auth_token', response.access_token)
  localStorage.setItem('auth_user', JSON.stringify(response.user))

  if (response.refresh_token) {
    localStorage.setItem('refresh_token', response.refresh_token)
  }

  if (response.expires_in) {
    localStorage.setItem('token_expires_at', String(Date.now() + response.expires_in * 1000))
  }

  if (
    typeof response.welcome_reward === 'number' &&
    response.welcome_reward >= 1 &&
    response.welcome_reward <= 5
  ) {
    localStorage.setItem(
      'pending_welcome_reward',
      JSON.stringify({ amount: response.welcome_reward, user_id: response.user.id }),
    )
  }

  return response.user.role === 'admin' ? '/admin/dashboard' : '/dashboard'
}

async function postJson<T>(path: string, body: Record<string, unknown>): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  })

  const payload = await response.json().catch(() => ({}))

  if (!response.ok) {
    throw new Error((payload as ApiEnvelope<T>).message || '请求失败，请稍后再试。')
  }

  return unwrapApiResponse<T>(payload)
}

export async function loginWithSub2Api(email: string, password: string): Promise<AuthResult> {
  try {
    const data = await postJson<AuthResponse | { requires_2fa?: boolean }>('/auth/login', {
      email,
      password,
    })

    if ('requires_2fa' in data && data.requires_2fa) {
      return { ok: false, message: '当前账号开启了二次验证，请前往控制台登录页完成验证。' }
    }

    return { ok: true, redirectTo: persistAuth(data as AuthResponse) }
  } catch (error) {
    return {
      ok: false,
      message:
        error instanceof Error
          ? normalizeAuthErrorMessage(error.message, '登录失败，请稍后再试。')
          : '登录失败，请稍后再试。',
    }
  }
}

export async function sendRegisterVerifyCode(email: string): Promise<VerifyCodeResult> {
  try {
    const data = await postJson<VerifyCodeResponse>('/auth/send-verify-code', {
      email,
    })

    return {
      ok: true,
      countdown: data.countdown || 60,
      message: data.message || '验证码已发送，请查收邮箱。',
    }
  } catch (error) {
    return {
      ok: false,
      message:
        error instanceof Error
          ? normalizeAuthErrorMessage(error.message, '验证码发送失败，请稍后再试。')
          : '验证码发送失败，请稍后再试。',
    }
  }
}

export async function registerWithSub2Api(
  email: string,
  password: string,
  verifyCode?: string,
  affiliateCode?: string,
): Promise<AuthResult> {
  try {
    const body: Record<string, unknown> = {
      email,
      password,
    }

    if (verifyCode?.trim()) {
      body.verify_code = verifyCode.trim()
    }

    if (affiliateCode?.trim()) {
      body.aff_code = affiliateCode.trim()
    }

    const data = await postJson<AuthResponse>('/auth/register', body)

    return { ok: true, redirectTo: persistAuth(data) }
  } catch (error) {
    return {
      ok: false,
      message:
        error instanceof Error
          ? normalizeAuthErrorMessage(error.message, '注册失败，请稍后再试。')
          : '注册失败，请稍后再试。',
    }
  }
}

export async function requestPasswordReset(email: string): Promise<AuthResult> {
  try {
    await postJson<{ message?: string }>('/auth/forgot-password', { email })
    return { ok: true, redirectTo: '/login' }
  } catch (error) {
    return {
      ok: false,
      message: error instanceof Error ? error.message : '重置邮件发送失败，请稍后再试。',
    }
  }
}

export async function resetSub2ApiPassword(
  email: string,
  token: string,
  newPassword: string,
): Promise<AuthResult> {
  try {
    await postJson<{ message?: string }>('/auth/reset-password', {
      email,
      token,
      new_password: newPassword,
    })
    return { ok: true, redirectTo: '/login' }
  } catch (error) {
    return {
      ok: false,
      message: error instanceof Error ? error.message : '密码重置失败，请重新获取重置链接。',
    }
  }
}
