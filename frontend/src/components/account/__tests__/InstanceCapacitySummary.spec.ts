import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import InstanceCapacitySummary from '../InstanceCapacitySummary.vue'
import zh from '@/i18n/locales/zh/admin/accounts'
import type { CredentialPrincipal } from '@/api/admin/credentialPrincipals'

const principal = (limit: number): CredentialPrincipal => ({
  id: 1, account_id: 10, group_ids: [], proxy_id: null, name: 'A', requested_limit: limit, account_max_concurrency: limit,
  occupied: 12, configured_capacity: 15, effective_configured_capacity: Math.min(limit, 15), available_capacity: 0,
  health_state: 'AVAILABLE', unknown_occupied: 0, observed_at: '2026-09-19T10:00:00Z', overhang: 0,
  config_version: 2, admin_state: 'ACTIVE', admission_state: 'ACTIVE', verification_state: 'VERIFIED', routing_mode: 'GROUPED',
  instances: ['A', 'B', 'C'].map((name, index) => ({ id: index + 1, account_id: index + 10, name, occupied: index === 2 ? 2 : 5,
    max_concurrency: 5, effective_hard_max: 5, hard_max: 5, weight: 1, state: index === 2 ? 'ACTIVE' : 'FULL',
    generation: 'generation', credential_version: 1, admin_state: 'ACTIVE', credential_state: 'VALID', transport_state: 'HEALTHY', identity_source: 'LOCAL_LOGICAL', overhang: 0, unknown_occupied: 0, active_bindings: 0 }))
})
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual('vue-i18n')), useI18n: () => ({
  t: (key: string, params: Record<string, unknown> = {}) => {
    const text = key.replace('admin.', '').split('.').reduce((value: any, part) => value?.[part], zh) as string
    return (text || key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
  }
}) }))
describe('account instance capacity', () => {
  it.each([12, 20])('separates configured instance limits from account limit %s', (limit) => {
    const wrapper = mount(InstanceCapacitySummary, { props: { principal: principal(limit) } })
    expect(wrapper.text()).toContain(`12/${limit}`)
    for (const value of ['A 5/5', 'B 5/5', 'C 2/5', '实例配置容量合计: 15', `配置有效最大并发: ${Math.min(limit, 15)}`]) expect(wrapper.text()).toContain(value)
    expect(wrapper.find('details').text()).toContain('观测时间')
  })
  it('retains shrinking occupancy and excludes archived instance capacity tags', () => {
    const value = principal(8); value.overhang = 4; value.instances[0]!.archived_at = value.observed_at
    const wrapper = mount(InstanceCapacitySummary, { props: { principal: value } })
    expect(wrapper.text()).toContain('12/8')
    expect(wrapper.text()).toContain('缩容中，超出 4')
    expect(wrapper.text()).toContain('2 个实例')
    expect(wrapper.text()).not.toContain('A 5/5')
  })
})
