import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import LoadingSpinner from '../LoadingSpinner.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('LoadingSpinner', () => {
  it('renders the default spinner unchanged (no orbit structure)', () => {
    const wrapper = mount(LoadingSpinner)
    const root = wrapper.find('[role="status"]')

    expect(root.exists()).toBe(true)
    expect(root.classes()).toContain('spinner')
    expect(root.classes()).toContain('text-primary-500')
    expect(wrapper.find('.orbit-dot').exists()).toBe(false)
  })

  it('renders orbiting dots for the orbit variant', () => {
    const wrapper = mount(LoadingSpinner, { props: { variant: 'orbit' } })
    const root = wrapper.find('[role="status"]')

    expect(root.classes()).toContain('orbit-spinner')
    expect(wrapper.findAll('.orbit-ring').length).toBe(3)
    expect(wrapper.findAll('.orbit-dot').length).toBe(3)
    expect(wrapper.find('.orbit-center').exists()).toBe(true)
    expect(wrapper.find('.spinner').exists()).toBe(false)
  })
})
