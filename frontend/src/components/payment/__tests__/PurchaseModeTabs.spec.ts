import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import PurchaseModeTabs from '../recharge/PurchaseModeTabs.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const tabs = [
  { key: 'recharge' as const, label: 'Balance recharge' },
  {
    key: 'subscription' as const,
    label: 'Subscription',
    recommended: true,
    panelId: 'subscription-options',
  },
]

describe('PurchaseModeTabs', () => {
  it('keeps the public tab ids and links each tab to its panel', () => {
    const wrapper = mount(PurchaseModeTabs, {
      props: { modelValue: 'recharge', tabs },
      attachTo: document.body,
    })

    const recharge = wrapper.get('[data-testid="purchase-mode-recharge"]')
    const subscription = wrapper.get('[data-testid="purchase-mode-subscription"]')

    expect(recharge.attributes()).toMatchObject({
      id: 'purchase-tab-recharge',
      'aria-controls': 'purchase-panel-recharge',
      'aria-selected': 'true',
      'aria-pressed': 'true',
      tabindex: '0',
    })
    expect(subscription.attributes('aria-controls')).toBe('subscription-options')
    expect(subscription.attributes('tabindex')).toBe('-1')
    expect(subscription.text()).toContain('payment.rechargeUi.recommended')

    wrapper.unmount()
  })

  it.each([
    ['ArrowRight', 'subscription'],
    ['ArrowLeft', 'subscription'],
    ['End', 'subscription'],
    ['Home', 'recharge'],
  ])('uses %s for automatic tab selection and focus', async (key, expected) => {
    const wrapper = mount(PurchaseModeTabs, {
      props: { modelValue: 'recharge', tabs },
      attachTo: document.body,
    })
    const recharge = wrapper.get<HTMLButtonElement>('[data-testid="purchase-mode-recharge"]')

    await recharge.trigger('keydown', { key })
    await nextTick()

    const emittedValues = wrapper.emitted('update:modelValue')?.flat()
    if (expected === 'recharge') {
      expect(emittedValues).toBeUndefined()
    } else {
      expect(emittedValues).toEqual([expected])
    }
    expect(document.activeElement).toBe(
      wrapper.get(`[data-testid="purchase-mode-${expected}"]`).element,
    )

    wrapper.unmount()
  })

  it('renders the remaining tab when only one purchase mode is available', () => {
    const wrapper = mount(PurchaseModeTabs, {
      props: {
        modelValue: 'subscription',
        tabs: [tabs[1]],
      },
    })

    expect(wrapper.get('[role="tablist"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="purchase-mode-recharge"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="purchase-mode-subscription"]').attributes('aria-selected'))
      .toBe('true')
  })
})
