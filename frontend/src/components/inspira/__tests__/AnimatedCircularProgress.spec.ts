/**
 * AnimatedCircularProgress 冒烟测试
 */
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AnimatedCircularProgress from '../AnimatedCircularProgress.vue'

function getProgressCircle(wrapper: ReturnType<typeof mount>) {
  // 第二个 circle 是进度弧
  return wrapper.findAll('circle')[1]
}

describe('AnimatedCircularProgress', () => {
  it('渲染 SVG 圆环(轨道 + 进度弧)与进度语义', () => {
    const wrapper = mount(AnimatedCircularProgress, { props: { value: 50, duration: 0 } })
    expect(wrapper.findAll('circle').length).toBe(2)
    expect(wrapper.attributes('role')).toBe('progressbar')
    expect(wrapper.attributes('aria-valuenow')).toBe('50')
  })

  it('默认尺寸 64,可自定义 size', () => {
    const wrapper = mount(AnimatedCircularProgress, { props: { value: 10, duration: 0 } })
    expect(wrapper.find('svg').attributes('width')).toBe('64')
    const custom = mount(AnimatedCircularProgress, {
      props: { value: 10, size: 40, duration: 0 }
    })
    expect(custom.find('svg').attributes('width')).toBe('40')
  })

  it('duration=0 时进度弧立即到位(50% -> dashoffset = 周长一半)', () => {
    const wrapper = mount(AnimatedCircularProgress, {
      props: { value: 50, size: 64, strokeWidth: 6, duration: 0 }
    })
    const circle = getProgressCircle(wrapper)
    const circumference = 2 * Math.PI * ((64 - 6) / 2)
    expect(Number(circle.attributes('stroke-dasharray'))).toBeCloseTo(circumference, 3)
    expect(Number(circle.attributes('stroke-dashoffset'))).toBeCloseTo(circumference / 2, 3)
  })

  it('value > 100 时封顶画满环(dashoffset = 0),数值仍显示原值', () => {
    const wrapper = mount(AnimatedCircularProgress, { props: { value: 130, duration: 0 } })
    const circle = getProgressCircle(wrapper)
    expect(Number(circle.attributes('stroke-dashoffset'))).toBeCloseTo(0, 3)
    expect(wrapper.text()).toContain('130%')
  })

  it('阈值自动配色:<70 teal、70-90 amber、>=90 rose', () => {
    const teal = mount(AnimatedCircularProgress, { props: { value: 30, duration: 0 } })
    expect(getProgressCircle(teal).attributes('stroke')).toBe('#14b8a6')
    const amber = mount(AnimatedCircularProgress, { props: { value: 75, duration: 0 } })
    expect(getProgressCircle(amber).attributes('stroke')).toBe('#f59e0b')
    const rose = mount(AnimatedCircularProgress, { props: { value: 95, duration: 0 } })
    expect(getProgressCircle(rose).attributes('stroke')).toBe('#f43f5e')
  })

  it('显式 color 覆盖阈值配色', () => {
    const wrapper = mount(AnimatedCircularProgress, {
      props: { value: 95, color: '#123456', duration: 0 }
    })
    expect(getProgressCircle(wrapper).attributes('stroke')).toBe('#123456')
  })

  it('showValue=false 时不渲染中心数值', () => {
    const wrapper = mount(AnimatedCircularProgress, {
      props: { value: 50, showValue: false, duration: 0 }
    })
    expect(wrapper.text()).toBe('')
  })

  it('默认 duration(带动画)下挂载、变更 value、卸载不抛错', async () => {
    const wrapper = mount(AnimatedCircularProgress, { props: { value: 20 } })
    await wrapper.setProps({ value: 80 })
    expect(() => wrapper.unmount()).not.toThrow()
  })
})
