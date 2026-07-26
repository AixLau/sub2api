/**
 * AnimatedList 冒烟测试
 */
import { describe, it, expect } from 'vitest'
import { defineComponent, h, ref, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import AnimatedList from '../AnimatedList.vue'

/** 用渲染函数包一层，模拟 "直接包裹 v-for 内容" 的用法 */
function mountList(items: ReturnType<typeof ref<number[]>>, props: Record<string, unknown> = {}) {
  return mount(
    defineComponent({
      setup() {
        return () =>
          h(AnimatedList, props, {
            default: () => (items.value ?? []).map((i) => h('li', { key: i, class: 'row' }, String(i)))
          })
      }
    })
  )
}

describe('AnimatedList', () => {
  it('渲染插槽子项，默认根标签为 div', () => {
    const items = ref([1, 2, 3])
    const wrapper = mountList(items)

    expect(wrapper.element.tagName).toBe('DIV')
    expect(wrapper.findAll('.row')).toHaveLength(3)
    expect(wrapper.findAll('.row')[0].text()).toBe('1')
  })

  it('tag prop 生效', () => {
    const items = ref([1])
    const wrapper = mountList(items, { tag: 'ul' })
    expect(wrapper.element.tagName).toBe('UL')
  })

  it('新增 / 删除条目正常渲染（enter hooks 不抛错）', async () => {
    const items = ref([1, 2])
    const wrapper = mountList(items, { stagger: 40 })
    expect(wrapper.findAll('.row')).toHaveLength(2)

    items.value = [...(items.value ?? []), 3]
    await nextTick()
    expect(wrapper.findAll('.row')).toHaveLength(3)

    items.value = [2, 3]
    await nextTick()
    // leave 过渡期间旧节点可能仍在 DOM，只断言目标节点存在
    const texts = wrapper.findAll('.row').map((n) => n.text())
    expect(texts).toContain('2')
    expect(texts).toContain('3')
  })
})
