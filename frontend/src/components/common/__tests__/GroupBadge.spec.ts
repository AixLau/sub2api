import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import GroupBadge from '../GroupBadge.vue'
import { useAppStore } from '@/stores/app'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('GroupBadge', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('shows only group rate divided by the balance recharge multiplier', () => {
    const appStore = useAppStore()
    appStore.cachedPublicSettings = {
      payment_balance_recharge_multiplier: 10
    } as any

    const wrapper = mount(GroupBadge, {
      props: {
        name: 'Codex高速专线',
        platform: 'openai',
        rateMultiplier: 1.7
      }
    })

    expect(wrapper.text()).toContain('0.17x')
    expect(wrapper.text()).not.toContain('1.7x')
  })

  it('shows only custom rate divided by the balance recharge multiplier', () => {
    const appStore = useAppStore()
    appStore.cachedPublicSettings = {
      payment_balance_recharge_multiplier: 10
    } as any

    const wrapper = mount(GroupBadge, {
      props: {
        name: 'Codex高速专线',
        platform: 'openai',
        rateMultiplier: 1.733,
        userRateMultiplier: 1.5
      }
    })

    expect(wrapper.text()).toContain('0.15x')
    expect(wrapper.text()).not.toContain('1.733x')
    expect(wrapper.text()).not.toContain('1.5x')
  })
})
