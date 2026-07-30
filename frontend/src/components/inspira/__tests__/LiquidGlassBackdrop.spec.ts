import { shallowMount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LiquidGlassBackdrop from '../LiquidGlassBackdrop.vue'

describe('LiquidGlassBackdrop', () => {
  it('renders the shared preview background as a decorative, non-interactive layer', () => {
    const wrapper = shallowMount(LiquidGlassBackdrop)

    expect(wrapper.get('[data-testid="liquid-glass-backdrop"]').attributes('aria-hidden')).toBe(
      'true'
    )
    expect(wrapper.get('[data-testid="liquid-glass-backdrop"]').classes()).toContain(
      'pointer-events-none'
    )
    expect(wrapper.find('.liquid-glass-backdrop__orb').exists()).toBe(false)
    expect(wrapper.find('.liquid-glass-backdrop__dots').exists()).toBe(false)
  })
})
