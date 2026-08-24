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
  it('uses the reference heading without numbering or supporting description', async () => {
    const wrapper = mountSelector()
    await wrapper.setProps({ showHeader: true })

    expect(wrapper.get('#recharge-amount-title').text())
      .toContain('payment.rechargeUi.selectAmount')
    expect(wrapper.get('#recharge-amount-title').text()).not.toMatch(/^1\./)
    expect(wrapper.text()).not.toContain('payment.rechargeUi.amountHint')
  })

  it('renders quick amount cards as integer amounts without decimals', () => {
    const wrapper = mountSelector()

    const preset = wrapper.find('[data-testid="quick-amount-1000"]')

    expect(preset.text()).toContain('¥1,000')
    expect(preset.text()).not.toContain('.00')
  })

  it('renders the seven presets once and keeps the custom trigger as the eighth grid item', () => {
    const wrapper = mountSelector()

    expect(wrapper.findAll('[data-testid^="quick-amount-"]')).toHaveLength(7)
    expect(wrapper.find('[data-testid="quick-amount-10"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quick-amount-1000"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="custom-recharge-trigger"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="custom-recharge-amount"]').exists()).toBe(false)
  })

  it('recommends ¥100 by default and follows an explicit recommendation override', async () => {
    const wrapper = mountSelector()

    expect(wrapper.find('[data-testid="quick-amount-100"]').text())
      .toContain('payment.rechargeUi.recommended')
    expect(wrapper.find('[data-testid="quick-amount-200"]').text())
      .not.toContain('payment.rechargeUi.recommended')

    await wrapper.setProps({ recommendedAmount: 200 })

    expect(wrapper.find('[data-testid="quick-amount-200"]').text())
      .toContain('payment.rechargeUi.recommended')
    expect(wrapper.find('[data-testid="quick-amount-100"]').text())
      .not.toContain('payment.rechargeUi.recommended')
  })

  it('renders one continuous Bézier soap-card SVG on the selected amount only', async () => {
    const wrapper = mountSelector()
    await wrapper.setProps({ modelValue: 100 })

    const selected = wrapper.get('[data-testid="quick-amount-100"]')

    const soap = selected.get('[data-testid="selected-amount-soap"]')

    expect(selected.classes()).toContain('is-selected')
    expect(wrapper.findAll('[data-testid="selected-amount-soap"]')).toHaveLength(1)
    expect(soap.attributes('viewBox')).toBe('0 0 300 150')
    expect(soap.find('.recharge-amount-option__soap-shape').attributes('d'))
      .toContain('C 92 15, 208 15, 266 8')
    expect(soap.find('.recharge-amount-option__soap-highlight').exists()).toBe(true)
    expect(soap.find('.recharge-amount-option__soap-lightning').exists()).toBe(true)
    expect(selected.text()).toContain('payment.rechargeUi.recommended')

    await wrapper.get('[data-testid="quick-amount-200"]').trigger('click')

    expect(wrapper.get('[data-testid="quick-amount-100"]').classes()).not.toContain('is-selected')
    expect(wrapper.get('[data-testid="quick-amount-200"]').classes()).toContain('is-selected')
    expect(wrapper.get('[data-testid="quick-amount-200"]')
      .find('[data-testid="selected-amount-soap"]').exists()).toBe(true)
  })

  it('keeps custom amount effects decorative and outside the real control', async () => {
    const wrapper = mountSelector()
    const card = wrapper.get('.recharge-custom-card')
    const trigger = wrapper.get('[data-testid="custom-recharge-trigger"]')
    const effects = wrapper.get('[data-testid="custom-amount-effects"]')

    expect(effects.attributes('aria-hidden')).toBe('true')
    expect(effects.find('.recharge-custom-card__dot-field').exists()).toBe(true)
    expect(effects.findAll('.recharge-custom-card__star')).toHaveLength(2)
    expect(card.element.contains(effects.element)).toBe(true)
    expect(trigger.element.contains(effects.element)).toBe(false)

    await trigger.trigger('click')
    const input = wrapper.get('[data-testid="custom-recharge-amount"]')
    await input.setValue('88.5')
    await input.trigger('keydown.enter')

    expect(wrapper.props('modelValue')).toBe(88.5)
    expect(wrapper.get('[data-testid="custom-amount-effects"]').attributes('aria-hidden')).toBe('true')
  })

  it('clears the preset selected state and focuses the input when custom amount is activated', async () => {
    const wrapper = mountSelector()

    const preset = wrapper.find('[data-testid="quick-amount-1000"]')

    expect(preset.classes()).toContain('is-selected')
    expect(wrapper.findAll('.is-selected')).toHaveLength(1)

    await wrapper.get('[data-testid="custom-recharge-trigger"]').trigger('click')

    expect(preset.classes()).not.toContain('is-selected')
    expect(wrapper.findAll('.is-selected')).toHaveLength(0)
    expect(wrapper.get('.recharge-custom-card').classes()).toContain('is-active')
    expect(wrapper.get('[data-testid="custom-recharge-amount"]').exists()).toBe(true)
    expect(wrapper.props('modelValue')).toBeNull()
  })

  it('accepts up to two decimal places and rejects scientific notation', async () => {
    const wrapper = mountSelector()
    await wrapper.get('[data-testid="custom-recharge-trigger"]').trigger('click')
    const customInput = wrapper.find<HTMLInputElement>('[data-testid="custom-recharge-amount"]')

    await customInput.setValue('125.678')

    expect(customInput.element.value).toBe('125.67')
    expect(wrapper.props('modelValue')).toBe(125.67)
    expect(wrapper.emitted('change')?.at(-1)).toEqual([125.67])

    await customInput.setValue('1e3')

    expect(customInput.element.value).toBe('125.67')
    expect(wrapper.props('modelValue')).toBe(125.67)
  })

  it('returns to a preset and clears custom editing state', async () => {
    const wrapper = mountSelector()
    await wrapper.get('[data-testid="custom-recharge-trigger"]').trigger('click')
    await wrapper.get('[data-testid="custom-recharge-amount"]').setValue('25.5')
    await wrapper.get('[data-testid="quick-amount-100"]').trigger('click')

    expect(wrapper.props('modelValue')).toBe(100)
    expect(wrapper.get('[data-testid="quick-amount-100"]').classes()).toContain('is-selected')
    expect(wrapper.find('[data-testid="custom-recharge-amount"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="custom-recharge-trigger"]').exists()).toBe(true)
  })

  it('uses danger semantics for an invalid custom amount', async () => {
    const wrapper = mountSelector()

    await wrapper.setProps({ modelValue: 600000, error: 'too high' })
    const customInput = wrapper.find<HTMLInputElement>('[data-testid="custom-recharge-amount"]')

    expect(wrapper.get('.recharge-custom-card').classes()).toContain('has-error')
    expect(customInput.attributes('aria-describedby')).toBe('recharge-amount-error')
    expect(wrapper.find('[data-testid="amount-error"]').text()).toBe('too high')
  })

  it('silently clamps a below-minimum custom amount after editing finishes', async () => {
    const wrapper = mountSelector()
    await wrapper.setProps({ modelValue: 1, error: 'minimum 10' })
    const customInput = wrapper.get<HTMLInputElement>('[data-testid="custom-recharge-amount"]')

    expect(wrapper.find('[data-testid="amount-error"]').exists()).toBe(false)
    await customInput.trigger('blur')
    await wrapper.setProps({ error: '' })

    expect(customInput.element.value).toBe('10')
    expect(wrapper.props('modelValue')).toBe(10)
    expect(wrapper.find('[data-testid="amount-error"]').exists()).toBe(false)
  })
})
