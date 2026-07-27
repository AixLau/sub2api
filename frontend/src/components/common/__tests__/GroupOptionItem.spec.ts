import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import GroupOptionItem from '../GroupOptionItem.vue'
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

describe('GroupOptionItem', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('shows group rate divided by the balance recharge multiplier when no custom rate exists', () => {
    const appStore = useAppStore()
    appStore.cachedPublicSettings = {
      payment_balance_recharge_multiplier: 10
    } as any

    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Codex高速专线',
        platform: 'openai',
        rateMultiplier: 1.7
      }
    })

    expect(wrapper.text()).toContain('1.7x')
    expect(wrapper.text()).toContain('0.17x')
    expect(wrapper.text()).toContain('admin.groups.rateLabel')
  })

  it('shows custom rate divided by the balance recharge multiplier without changing the struck original rate', () => {
    const appStore = useAppStore()
    appStore.cachedPublicSettings = {
      payment_balance_recharge_multiplier: 10
    } as any

    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Codex高速专线',
        platform: 'openai',
        rateMultiplier: 1.733,
        userRateMultiplier: 1.5
      }
    })

    expect(wrapper.text()).toContain('1.733x')
    expect(wrapper.text()).toContain('0.15x')
    expect(wrapper.text()).not.toContain('1.5x')
  })
})

describe('GroupOptionItem description layout', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('applies multiline and overflow-safe text styles', () => {
    const description = 'First section\nvery-long-unbroken-description-value-that-must-not-overflow'
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'Example group',
        platform: 'openai',
        description,
      },
      global: {
        stubs: {
          GroupBadge: true,
        },
      },
    })

    const descriptionElement = wrapper
      .findAll('span')
      .find((element) => element.text() === description)

    expect(descriptionElement).toBeDefined()
    expect(descriptionElement?.classes()).toContain('whitespace-pre-line')
    expect(descriptionElement?.classes()).toContain('[overflow-wrap:anywhere]')
    expect(descriptionElement?.classes()).toContain('line-clamp-3')
    expect(wrapper.find('[title]').attributes('title')).toBe(description)
  })
})
