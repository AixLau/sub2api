import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import QrCodeSkeleton from '../QrCodeSkeleton.vue'

describe('QrCodeSkeleton', () => {
  it('renders a decorative QR-shaped placeholder at the requested size', () => {
    const wrapper = mount(QrCodeSkeleton, { props: { size: 220, icon: '/alipay.svg' } })
    const root = wrapper.get('[data-test="payment-qr-skeleton"]')
    expect(root.attributes('aria-hidden')).toBe('true')
    expect(root.attributes('style')).toContain('width: 220px')
    expect(wrapper.get('svg').attributes('width')).toBe('220')
    // Three finder patterns plus pseudo-random modules.
    expect(wrapper.findAll('svg path')).toHaveLength(3)
    expect(wrapper.findAll('svg rect').length).toBeGreaterThan(50)
    expect(wrapper.get('img').attributes('src')).toBe('/alipay.svg')
  })

  it('is deterministic between renders', () => {
    const a = mount(QrCodeSkeleton).get('svg').html()
    const b = mount(QrCodeSkeleton).get('svg').html()
    expect(a).toBe(b)
  })

  it('stops the shimmer for users who prefer reduced motion', () => {
    const wrapper = mount(QrCodeSkeleton)
    expect(wrapper.get('.qr-skeleton-shimmer').classes()).toContain('motion-reduce:hidden')
  })
})
