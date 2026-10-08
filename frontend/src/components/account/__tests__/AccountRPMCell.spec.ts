import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountRPMCell from '../AccountRPMCell.vue'
import type { AccountListItem } from '@/types'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const account = { id: 42, platform: 'openai', type: 'oauth' } as AccountListItem

describe('account RPM cell', () => {
  it('shows zero and unlimited counts without inventing a limit', () => {
    const wrapper = mount(AccountRPMCell, { props: { account, current: 0, loading: false } })
    expect(wrapper.text()).toBe('0')
  })
  it('distinguishes loading and unavailable counts', async () => {
    const wrapper = mount(AccountRPMCell, { props: { account, current: null, loading: true } })
    expect(wrapper.text()).toBe('…')
    await wrapper.setProps({ loading: false })
    expect(wrapper.text()).toBe('—')
    expect(wrapper.attributes('title')).toContain('unavailable')
  })
  it('retains the effective Anthropic limit and strategy', () => {
    const wrapper = mount(AccountRPMCell, { props: { account: { ...account, platform: 'anthropic', base_rpm: 10 }, current: 8, loading: false } })
    expect(wrapper.text()).toContain('8')
    expect(wrapper.text()).toContain('10')
    expect(wrapper.text()).toContain('[T]')
    expect(wrapper.html()).toContain('bg-yellow-100')
  })
})
