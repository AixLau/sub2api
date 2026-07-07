type ApiEnvelope<T> = {
  code?: number
  message?: string
  data?: T
}

export type LoginAgreementDocument = {
  id: string
  title: string
  content_md?: string
}

export type LoginAgreementSettings = {
  enabled: boolean
  mode: 'modal' | 'checkbox'
  updatedAt: string
  revision: string
  documents: LoginAgreementDocument[]
}

type PublicSettingsResponse = {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}

const AGREEMENT_STORAGE_KEY = 'sub2api_login_agreement_consent'
const API_BASE_URL = '/api/v1'

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

function fallbackRevision(updatedAt: string, documents: LoginAgreementDocument[]): string {
  return `${updatedAt}:${documents.map((doc) => `${doc.id}:${doc.title}`).join('|')}`
}

export async function fetchLoginAgreementSettings(): Promise<LoginAgreementSettings> {
  const response = await fetch(`${API_BASE_URL}/settings/public`, {
    credentials: 'include',
  })
  const payload = await response.json().catch(() => ({}))

  if (!response.ok) {
    throw new Error((payload as ApiEnvelope<PublicSettingsResponse>).message || '请求失败，请稍后再试。')
  }

  const settings = unwrapApiResponse<PublicSettingsResponse>(payload)
  const documents = Array.isArray(settings.login_agreement_documents)
    ? settings.login_agreement_documents.filter((doc) => doc.title?.trim())
    : []
  const updatedAt = settings.login_agreement_updated_at || ''

  return {
    enabled: settings.login_agreement_enabled === true && documents.length > 0,
    mode: settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal',
    updatedAt,
    revision:
      settings.login_agreement_revision || fallbackRevision(updatedAt, documents),
    documents,
  }
}

export function hasAcceptedLoginAgreement(revision: string): boolean {
  if (!revision) {
    return false
  }

  try {
    const raw = localStorage.getItem(AGREEMENT_STORAGE_KEY)
    if (!raw) {
      return false
    }

    const parsed = JSON.parse(raw) as { revision?: string }
    return parsed.revision === revision
  } catch {
    return false
  }
}

export function persistLoginAgreementAcceptance(revision: string): void {
  if (!revision) {
    return
  }

  localStorage.setItem(
    AGREEMENT_STORAGE_KEY,
    JSON.stringify({
      revision,
      accepted_at: new Date().toISOString(),
    }),
  )
}
