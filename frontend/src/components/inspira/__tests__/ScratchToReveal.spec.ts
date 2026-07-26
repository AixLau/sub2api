/**
 * ScratchToReveal 冒烟测试
 * jsdom 下 canvas getContext 返回 null：不渲染覆盖层，内容直接可见并 emit complete
 */
import { describe, it, expect } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import ScratchToReveal from '../ScratchToReveal.vue'

describe('ScratchToReveal', () => {
  it('挂载与卸载不抛错，插槽内容渲染', () => {
    const wrapper = mount(ScratchToReveal, {
      slots: { default: '<p class="prize">$10</p>' }
    })
    expect(wrapper.find('.prize').exists()).toBe(true)
    expect(wrapper.find('.prize').text()).toBe('$10')
    expect(() => wrapper.unmount()).not.toThrow()
  })

  it('jsdom 无 canvas context 时移除覆盖层并 emit complete', async () => {
    const wrapper = mount(ScratchToReveal, {
      slots: { default: '<p class="prize">reward</p>' }
    })
    await nextTick()
    await nextTick()
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.find('.prize').exists()).toBe(true)
    expect(wrapper.emitted('complete')).toBeTruthy()
    wrapper.unmount()
  })

  it('自定义 props 挂载不抛错', () => {
    const wrapper = mount(ScratchToReveal, {
      props: { coverColor: '#14b8a6', coverText: 'scratch me', threshold: 0.3, radius: 12 },
      slots: { default: '<span>content</span>' }
    })
    expect(() => wrapper.unmount()).not.toThrow()
  })
})
