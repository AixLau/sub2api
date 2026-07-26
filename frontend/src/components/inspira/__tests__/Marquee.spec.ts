/**
 * Marquee 冒烟测试
 */
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import Marquee from '../Marquee.vue'

describe('Marquee', () => {
  it('默认插槽内容渲染两份，其中一份 aria-hidden', () => {
    const wrapper = mount(Marquee, {
      slots: { default: '<span class="item">hello</span>' }
    })

    const tracks = wrapper.findAll('.marquee-track')
    expect(tracks).toHaveLength(2)

    // 两份轨道都包含插槽内容
    expect(tracks[0].find('.item').exists()).toBe(true)
    expect(tracks[1].find('.item').exists()).toBe(true)

    // 恰好一份是 aria-hidden 的复制轨道
    const hidden = tracks.filter((t) => t.attributes('aria-hidden') === 'true')
    expect(hidden).toHaveLength(1)
  })

  it('duration / reverse props 生效', () => {
    const wrapper = mount(Marquee, {
      props: { duration: 10, reverse: true },
      slots: { default: '<span>x</span>' }
    })
    expect(wrapper.attributes('style')).toContain('--marquee-duration: 10s')
    expect(wrapper.find('.marquee-track').classes()).toContain('marquee-reverse')
  })
})
