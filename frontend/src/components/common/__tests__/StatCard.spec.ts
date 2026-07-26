import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StatCard from '../StatCard.vue'

const NumberTickerStub = {
  props: ['value', 'prefix', 'suffix', 'formatFn'],
  template: '<span data-testid="ticker">{{ prefix }}{{ formatFn ? formatFn(value) : value }}{{ suffix }}</span>'
}

describe('StatCard', () => {
  it('renders compact numeric cards with formatting, affixes, and footer content', () => {
    const wrapper = mount(StatCard, {
      props: {
        compact: true,
        title: 'Revenue',
        value: 12.5,
        prefix: '$',
        suffix: ' USD',
        formatValue: (value: number) => value.toFixed(2)
      },
      slots: {
        icon: '<span data-testid="icon">icon</span>',
        footer: '<span data-testid="footer">available</span>'
      },
      global: {
        stubs: {
          NumberTicker: NumberTickerStub
        }
      }
    })

    expect(wrapper.classes()).toContain('card')
    expect(wrapper.get('[data-testid="ticker"]').text()).toBe('$12.50 USD')
    expect(wrapper.get('[data-testid="icon"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="footer"]').text()).toBe('available')
  })

  it('renders string values without a ticker', () => {
    const wrapper = mount(StatCard, {
      props: {
        title: 'Status',
        value: 'Unavailable'
      },
      global: {
        stubs: {
          NumberTicker: NumberTickerStub
        }
      }
    })

    expect(wrapper.text()).toContain('Unavailable')
    expect(wrapper.find('[data-testid="ticker"]').exists()).toBe(false)
  })
})
