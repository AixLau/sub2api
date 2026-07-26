/**
 * 装饰性组件冒烟测试：挂载与卸载不抛错
 * （jsdom 下 canvas getContext 返回 null 也不应抛错）
 */
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import FlickeringGrid from '../FlickeringGrid.vue'
import AuroraBackground from '../AuroraBackground.vue'
import BorderBeam from '../BorderBeam.vue'
import CardSpotlight from '../CardSpotlight.vue'

describe('FlickeringGrid', () => {
  it('挂载与卸载不抛错', () => {
    const wrapper = mount(FlickeringGrid)
    expect(wrapper.find('canvas').exists()).toBe(true)
    expect(() => wrapper.unmount()).not.toThrow()
  })

  it('自定义 props 挂载不抛错', () => {
    const wrapper = mount(FlickeringGrid, {
      props: { squareSize: 8, gridGap: 4, color: '#ff0000', maxOpacity: 0.5 }
    })
    expect(() => wrapper.unmount()).not.toThrow()
  })
})

describe('AuroraBackground', () => {
  it('挂载与卸载不抛错', () => {
    const wrapper = mount(AuroraBackground)
    expect(wrapper.findAll('.aurora-blob')).toHaveLength(3)
    expect(() => wrapper.unmount()).not.toThrow()
  })
})

describe('BorderBeam', () => {
  it('挂载与卸载不抛错，CSS 变量按 props 设置', () => {
    const wrapper = mount(BorderBeam, { props: { size: 100, duration: 6 } })
    const style = wrapper.attributes('style') ?? ''
    expect(style).toContain('--beam-size: 100px')
    expect(style).toContain('--beam-duration: 6s')
    expect(() => wrapper.unmount()).not.toThrow()
  })
})

describe('CardSpotlight', () => {
  it('挂载与卸载不抛错，鼠标事件不抛错', async () => {
    const wrapper = mount(CardSpotlight, {
      slots: { default: '<p class="content">card</p>' }
    })
    expect(wrapper.find('.content').exists()).toBe(true)

    await wrapper.trigger('mouseenter')
    await wrapper.trigger('mousemove', { clientX: 10, clientY: 20 })
    await wrapper.trigger('mouseleave')

    expect(() => wrapper.unmount()).not.toThrow()
  })
})
