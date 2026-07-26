/**
 * GlitchText 冒烟测试
 */
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import GlitchText from '../GlitchText.vue'

describe('GlitchText', () => {
  it('渲染 text 且 data-text 属性正确', () => {
    const wrapper = mount(GlitchText, { props: { text: 'Sub2API' } })
    expect(wrapper.text()).toBe('Sub2API')
    expect(wrapper.attributes('data-text')).toBe('Sub2API')
  })
})
