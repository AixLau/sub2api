/**
 * MultiStepLoader 冒烟测试
 */
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MultiStepLoader from '../MultiStepLoader.vue'

const steps = [
  { title: 'Step A', description: 'desc a' },
  { title: 'Step B' },
  { title: 'Step C' }
]

describe('MultiStepLoader', () => {
  it('渲染全部步骤并按 current 划分状态', () => {
    const wrapper = mount(MultiStepLoader, {
      props: { steps, current: 1 }
    })

    const items = wrapper.findAll('.msl-step')
    expect(items).toHaveLength(3)

    // 已完成 / 进行中 / 未来
    expect(items[0].classes()).toContain('msl-done')
    expect(items[1].classes()).toContain('msl-active')
    expect(items[2].classes()).toContain('msl-pending')

    // 已完成步骤有勾,当前步骤有转圈,未来步骤是灰点
    expect(items[0].find('.msl-check').exists()).toBe(true)
    expect(items[1].find('.msl-spinner').exists()).toBe(true)
    expect(items[2].find('.msl-dot').exists()).toBe(true)

    // 文案与描述渲染
    expect(wrapper.text()).toContain('Step A')
    expect(wrapper.text()).toContain('desc a')
  })

  it('current 变更后状态随之推进', async () => {
    const wrapper = mount(MultiStepLoader, {
      props: { steps, current: 0 }
    })
    expect(wrapper.findAll('.msl-step')[0].classes()).toContain('msl-active')

    await wrapper.setProps({ current: 2 })
    const items = wrapper.findAll('.msl-step')
    expect(items[0].classes()).toContain('msl-done')
    expect(items[1].classes()).toContain('msl-done')
    expect(items[2].classes()).toContain('msl-active')
    expect(items[2].find('.msl-spinner').exists()).toBe(true)
  })

  it('error 态当前步骤显示红叉,其他步骤不受影响', () => {
    const wrapper = mount(MultiStepLoader, {
      props: { steps, current: 1, error: true }
    })
    const items = wrapper.findAll('.msl-step')
    expect(items[1].classes()).toContain('msl-error')
    expect(items[1].find('.msl-cross').exists()).toBe(true)
    expect(items[1].find('.msl-spinner').exists()).toBe(false)
    expect(items[0].classes()).toContain('msl-done')
    expect(items[2].classes()).toContain('msl-pending')
  })
})
