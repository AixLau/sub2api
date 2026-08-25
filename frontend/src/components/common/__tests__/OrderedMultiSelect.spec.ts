import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import OrderedMultiSelect from '../OrderedMultiSelect.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const options = [
  { value: 'first', label: 'First model' },
  { value: 'second', label: 'Second model' },
  { value: 'third', label: 'Third model' },
]

describe('OrderedMultiSelect', () => {
  it('adds a dropdown selection at the end of the fallback order', async () => {
    const wrapper = mount(OrderedMultiSelect, {
      props: { modelValue: ['first'], options },
    })

    wrapper.getComponent({ name: 'Select' }).vm.$emit('change', 'second', options[1])
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['first', 'second']])
  })

  it('moves and removes selected models while preserving an explicit order', async () => {
    const wrapper = mount(OrderedMultiSelect, {
      props: { modelValue: ['first', 'second', 'third'], options },
    })

    const secondRow = wrapper.findAll('[data-test="ordered-multi-select-item"]')[1]
    await secondRow.get('button[aria-label="Move up"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['second', 'first', 'third']])

    const thirdRow = wrapper.findAll('[data-test="ordered-multi-select-item"]')[2]
    await thirdRow.get('button[aria-label="Remove"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['first', 'second']])
  })
})
