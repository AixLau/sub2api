import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import OpenAIInstanceForm from '../OpenAIInstanceForm.vue'
import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'
import * as api from '@/api/admin/credentialPrincipals'
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useOpenAIOAuth', () => ({ useOpenAIOAuth: () => ({ authUrl: ref(''), sessionId: ref(''), oauthState: ref(''), loading: ref(false), error: ref(''), generateAuthUrl: vi.fn(), exchangeAuthCode: vi.fn(), validateRefreshToken: vi.fn() }) }))
vi.mock('@/api/admin/credentialPrincipals', () => ({ importCredential: vi.fn(), createCredentialAccount: vi.fn(), addCredentialInstance: vi.fn(), getCredentialPrincipal: vi.fn(), reverifyCredentialImport: vi.fn(), credentialErrorReason: () => 'UNAVAILABLE' }))
const make = () => mount(OpenAIInstanceForm, { props: { accountName: 'A', accountLimit: 12, initialName: 'A1', initialMax: 5 }, global: { stubs: { OAuthAuthorizationFlow: true, TotpStepUpDialog: true } } })
beforeEach(() => { sessionStorage.clear(); vi.clearAllMocks(); vi.mocked(api.importCredential).mockResolvedValue({ import_id: 'opaque-import', verification_state: 'VERIFIED', expires_at: '' }) })
describe('shared OpenAI instance authorization', () => {
  it('recovers a lost creation response with the same operation and no stored secrets', async () => {
    vi.mocked(api.createCredentialAccount).mockRejectedValueOnce(new Error('lost response')).mockResolvedValueOnce({ id: 7 })
    const wrapper = make()
    wrapper.findComponent(OAuthAuthorizationFlow).vm.$emit('import-codex-session', JSON.stringify({ tokens: { access_token: 'secret-access', refresh_token: 'secret-refresh' } }))
    await flushPromises()
    const first = vi.mocked(api.createCredentialAccount).mock.calls[0]!
    expect(first[0].account_max_concurrency).toBe(12)
    expect(first[0].instances[0]?.max_concurrency).toBe(5)
    expect(JSON.stringify(sessionStorage)).not.toContain('secret-access')
    expect(JSON.stringify(sessionStorage)).not.toContain('secret-refresh')
    await wrapper.findAll('button').find(b => b.text().includes('resumeOperation'))!.trigger('click')
    await flushPromises()
    expect(vi.mocked(api.createCredentialAccount).mock.calls[1]).toEqual(first)
    expect(wrapper.emitted('completed')).toHaveLength(1)
    expect(sessionStorage.length).toBe(0)
  })
  it('keeps unverified authorizations pending without creating an account', async () => {
    vi.mocked(api.importCredential).mockResolvedValue({ import_id: 'pending', verification_state: 'UNVERIFIED', expires_at: '' })
    const wrapper = make()
    wrapper.findComponent(OAuthAuthorizationFlow).vm.$emit('import-codex-session', JSON.stringify({ access_token: 'secret-access' }))
    await flushPromises()
    expect(api.createCredentialAccount).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('pendingVerification')
  })
})
