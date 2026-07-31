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
  it('renders quick amount cards as integer amounts without decimals', () => {
    const wrapper = mountSelector()

    const preset = wrapper.find('[data-testid="quick-amount-1000"]')

    expect(preset.text()).toContain('¥1,000')
    expect(preset.text()).not.toContain('.00')
  })

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

  it('rejects decimal custom amounts instead of updating the selected amount', async () => {
    const wrapper = mountSelector()
    const customInput = wrapper.find<HTMLInputElement>('[data-testid="custom-recharge-amount"]')

    await customInput.trigger('focus')
    await customInput.setValue('125')
    await customInput.setValue('125.5')

    expect(customInput.element.value).toBe('125')
    expect(wrapper.props('modelValue')).toBe(125)
  })

  it('uses danger semantics for an invalid custom amount', async () => {
    const wrapper = mountSelector()
    const customInput = wrapper.find<HTMLInputElement>('[data-testid="custom-recharge-amount"]')

    await wrapper.setProps({ modelValue: 5, error: 'too low' })
    await customInput.trigger('focus')

    expect(customInput.element.closest('label')?.classList).toContain('recharge-choice-card-error')
    expect(wrapper.find('[data-testid="amount-error"]').classes()).toEqual(
      expect.arrayContaining([
        'border-status-danger-border',
        'bg-status-danger-soft',
        'text-status-danger',
      ])
    )
  })
})
