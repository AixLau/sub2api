/**
 * Timeline 冒烟测试
 *
 * 注意：全局 setup.ts 把 matchMedia mock 成对任何 query 都返回 matches:true，
 * 会命中 prefers-reduced-motion 分支。需要动画路径的用例按需覆写为 matches:false。
 */
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import Timeline, { type TimelineItem } from '../Timeline.vue'

function withMatchMedia<T>(matches: boolean, fn: () => T): T {
  const original = window.matchMedia
  window.matchMedia = ((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn()
  })) as unknown as typeof window.matchMedia
  try {
    return fn()
  } finally {
    window.matchMedia = original
  }
}

const items: TimelineItem[] = [
  {
    time: '2026-07-26 10:00',
    title: 'admin@example.com · user.create',
    description: 'POST /api/users',
    badge: '200',
    tone: 'success'
  },
  {
    time: '2026-07-26 10:05',
    title: 'admin@example.com · user.delete',
    description: 'DELETE /api/users/1',
    badge: '500',
    tone: 'danger'
  },
  {
    time: '2026-07-26 10:10',
    title: 'ops@example.com · settings.update'
  }
]

describe('Timeline', () => {
  it('渲染全部节点，含时间 / 标题 / 描述 / badge', () => {
    const wrapper = mount(Timeline, { props: { items } })

    const nodes = wrapper.findAll('.timeline-item')
    expect(nodes).toHaveLength(3)
    expect(nodes[0].text()).toContain('2026-07-26 10:00')
    expect(nodes[0].text()).toContain('admin@example.com · user.create')
    expect(nodes[0].text()).toContain('POST /api/users')
    expect(nodes[0].text()).toContain('200')
  })

  it('description / badge 缺省时不渲染对应元素', () => {
    const wrapper = mount(Timeline, { props: { items } })
    const last = wrapper.findAll('.timeline-item')[2]
    expect(last.find('.timeline-badge').exists()).toBe(false)
    expect(last.find('.font-mono').exists()).toBe(false)
  })

  it('tone 决定圆点颜色，default 兜底', () => {
    const wrapper = mount(Timeline, { props: { items } })
    const cores = wrapper.findAll('.timeline-dot-core')
    expect(cores[0].classes()).toContain('bg-green-500')
    expect(cores[1].classes()).toContain('bg-red-500')
    expect(cores[2].classes()).toContain('bg-gray-400')
  })

  it('竖线只在非末尾节点渲染', () => {
    const wrapper = mount(Timeline, { props: { items } })
    expect(wrapper.findAll('.timeline-line')).toHaveLength(2)
  })

  it('进场动画按索引 stagger；reduced-motion 时不加动画类', () => {
    const animated = withMatchMedia(false, () =>
      mount(Timeline, { props: { items, stagger: 40 } })
    )
    const nodes = animated.findAll('.timeline-item')
    expect(nodes[0].classes()).toContain('timeline-item-animated')
    expect(nodes[0].attributes('style')).toContain('animation-delay: 0ms')
    expect(nodes[2].attributes('style')).toContain('animation-delay: 80ms')

    const reduced = withMatchMedia(true, () => mount(Timeline, { props: { items } }))
    const reducedNode = reduced.findAll('.timeline-item')[0]
    expect(reducedNode.classes()).not.toContain('timeline-item-animated')
    expect(reducedNode.attributes('style')).toBeUndefined()
  })

  it('空 items 渲染空列表不报错', () => {
    const wrapper = mount(Timeline, { props: { items: [] } })
    expect(wrapper.findAll('.timeline-item')).toHaveLength(0)
  })
})
