import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RechargeAmountSelector from '../recharge/RechargeAmountSelector.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

function mountSelector() {
  const wrapper = mount(RechargeAmountSelector, {
    props: {
      modelValue: 1000,
      min: 10,
      max: 500000,
      showHeader: false,
      showPresetMeta: false,
      formatAmount: (value: number) => `¥${value.toLocaleString('en-US')}.00`,
      'onUpdate:modelValue': (value: number | null) => wrapper.setProps({ modelValue: value }),
    },
  })
  return wrapper
}

describe('RechargeAmountSelector', () => {
  it('clears the preset selected state when custom amount is focused', async () => {
    const wrapper = mountSelector()

    const preset = wrapper.find('[data-testid="quick-amount-1000"]')
    const customInput = wrapper.find('[data-testid="custom-recharge-amount"]')

    expect(preset.classes()).toContain('recharge-choice-card-selected')
    expect(wrapper.findAll('.recharge-choice-card-selected')).toHaveLength(1)
    expect(preset.find('svg').exists()).toBe(false)

    await customInput.trigger('focus')

    expect(preset.classes()).not.toContain('recharge-choice-card-selected')
    expect(wrapper.findAll('.recharge-choice-card-selected')).toHaveLength(1)
  })
})
