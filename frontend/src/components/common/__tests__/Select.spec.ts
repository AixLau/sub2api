import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

import Select from '../Select.vue'

describe('Select', () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 68,
      y: 318,
      top: 318,
      right: 762,
      bottom: 382,
      left: 68,
      width: 694,
      height: 64,
      toJSON: () => ({})
    } as DOMRect)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('keeps the teleported dropdown at the trigger width', async () => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: {
        modelValue: null,
        options: [
          {
            value: 1,
            label: 'A long option whose content must not widen the dropdown'
          }
        ]
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    await wrapper.get('.select-trigger').trigger('click')
    await flushPromises()

    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown?.style.left).toBe('68px')
    expect(dropdown?.style.width).toBe('694px')
    expect(dropdown?.style.minWidth).toBe('')

    wrapper.unmount()
  })
})
