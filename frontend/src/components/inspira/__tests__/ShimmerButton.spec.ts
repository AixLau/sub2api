/**
 * ShimmerButton 冒烟测试
 */
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ShimmerButton from '../ShimmerButton.vue'

describe('ShimmerButton', () => {
  it('默认渲染 button 元素', () => {
    const wrapper = mount(ShimmerButton)
    expect(wrapper.element.tagName).toBe('BUTTON')
  })

  it('as="span" 时渲染 span 元素', () => {
    const wrapper = mount(ShimmerButton, { props: { as: 'span' } })
    expect(wrapper.element.tagName).toBe('SPAN')
  })

  it('插槽内容出现', () => {
    const wrapper = mount(ShimmerButton, {
      slots: { default: '<span class="label">Click me</span>' }
    })
    expect(wrapper.find('.label').exists()).toBe(true)
    expect(wrapper.text()).toContain('Click me')
  })
})
