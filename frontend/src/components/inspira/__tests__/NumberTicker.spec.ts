/**
 * NumberTicker 冒烟测试
 */
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import NumberTicker from '../NumberTicker.vue'

describe('NumberTicker', () => {
  it('渲染为 span', () => {
    const wrapper = mount(NumberTicker, { props: { value: 42, duration: 0 } })
    expect(wrapper.element.tagName).toBe('SPAN')
  })

  it('duration=0 时立即显示最终格式化值（千分位）', () => {
    const wrapper = mount(NumberTicker, { props: { value: 1234, duration: 0 } })
    expect(wrapper.text()).toBe('1,234')
  })

  it('支持 prefix / suffix', () => {
    const wrapper = mount(NumberTicker, {
      props: { value: 1234, duration: 0, prefix: '$', suffix: '%' }
    })
    expect(wrapper.text()).toBe('$1,234%')
  })

  it('支持 decimalPlaces', () => {
    const wrapper = mount(NumberTicker, {
      props: { value: 1234.5, duration: 0, decimalPlaces: 2 }
    })
    expect(wrapper.text()).toBe('1,234.50')
  })

  it('formatFn 优先于 decimalPlaces 格式化', () => {
    const wrapper = mount(NumberTicker, {
      props: {
        value: 1234,
        duration: 0,
        formatFn: (n: number) => `#${n.toFixed(1)}`
      }
    })
    expect(wrapper.text()).toBe('#1234.0')
  })

  it('props.value 变更不抛错，且 duration=0 时立即更新', async () => {
    const wrapper = mount(NumberTicker, { props: { value: 100, duration: 0 } })
    await wrapper.setProps({ value: 5000 })
    expect(wrapper.text()).toBe('5,000')
  })

  it('默认 duration（带动画）下挂载与卸载不抛错', () => {
    const wrapper = mount(NumberTicker, { props: { value: 999 } })
    expect(() => wrapper.unmount()).not.toThrow()
  })
})
