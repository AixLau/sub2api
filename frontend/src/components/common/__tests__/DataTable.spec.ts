import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DataTable from '../DataTable.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const stubDesktopMatchMedia = () => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: true,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
}

const stubMobileMatchMedia = () => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn()
    }))
  })
}

describe('DataTable', () => {
  beforeEach(() => {
    stubDesktopMatchMedia()
    localStorage.clear()
  })

  it('renders paired sort arrows and highlights the active direction', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [
          { key: 'name', label: 'Name', sortable: true },
          { key: 'created_at', label: 'Created', sortable: true }
        ],
        data: [
          { id: 1, name: 'Beta', created_at: '2026-01-02T00:00:00Z' },
          { id: 2, name: 'Alpha', created_at: '2026-01-01T00:00:00Z' }
        ],
        defaultSortKey: 'name',
        defaultSortOrder: 'asc'
      },
      slots: {
        'header-name': '<span data-test="custom-name-header">Name</span>'
      }
    })

    await wrapper.vm.$nextTick()

    const nameHeader = wrapper.findAll('th')[0]
    expect(nameHeader.find('[data-test="custom-name-header"]').exists()).toBe(true)
    expect(nameHeader.attributes('aria-sort')).toBe('ascending')
    expect(nameHeader.findAll('svg')).toHaveLength(2)
    expect(nameHeader.findAll('svg')[0].classes()).toContain('text-primary-600')
    expect(nameHeader.findAll('svg')[1].classes()).toContain('text-gray-300')

    await nameHeader.trigger('click')
    await wrapper.vm.$nextTick()

    expect(nameHeader.attributes('aria-sort')).toBe('descending')
    expect(nameHeader.findAll('svg')[0].classes()).toContain('text-gray-300')
    expect(nameHeader.findAll('svg')[1].classes()).toContain('text-primary-600')
  })

  it('renders every row with no virtual padding spacer for small datasets (virtualization off)', async () => {
    const data = Array.from({ length: 8 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data
      }
    })

    await wrapper.vm.$nextTick()

    // Virtualization is OFF for a small list…
    expect((wrapper.vm as any).shouldVirtualize).toBe(false)
    // …every row is in the DOM…
    expect(wrapper.findAll('tbody tr[data-index]')).toHaveLength(data.length)
    // …and there are no aria-hidden virtual padding spacer rows.
    expect(wrapper.findAll('tbody tr[aria-hidden="true"]')).toHaveLength(0)
  })

  it('switches to windowed rendering once row count exceeds virtualizeThreshold', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        virtualizeThreshold: 3
      }
    })

    await wrapper.vm.$nextTick()

    // Virtualization is ON: the mode-switch decision flipped…
    expect((wrapper.vm as any).shouldVirtualize).toBe(true)
    // …and the virtualizer drives off the full row count.
    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    expect(instance.options.count).toBe(data.length)
  })

  it('keys the virtualizer size cache by row identity, not index (avoids stale heights on sort/filter)', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ id: 100 + i, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        rowKey: 'id',
        virtualizeThreshold: 3
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    // getItemKey must resolve to the row's stable key (id), not the positional index.
    expect(instance.options.getItemKey(0)).toBe(100)
    expect(instance.options.getItemKey(5)).toBe(105)
  })

  it('clears stale row and element caches when pagination replaces the row ID set', async () => {
    const firstPage = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, name: `First ${i + 1}` }))
    const secondPage = Array.from({ length: 100 }, (_, i) => ({ id: i + 101, name: `Second ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        rowKey: 'id',
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const firstPageIDs = firstPage.map(row => row.id)
    ;(instance as any).itemSizeCache = new Map(firstPageIDs.map(id => [id, 156]))
    instance.elementsCache.clear()
    for (const id of firstPageIDs) {
      instance.elementsCache.set(id, document.createElement('tr'))
    }
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    const sizeCache = (instance as any).itemSizeCache as Map<number, number>
    expect(sizeCache.size).toBeLessThanOrEqual(secondPage.length)
    expect(instance.elementsCache.size).toBeLessThanOrEqual(secondPage.length)
    expect(firstPageIDs.some(id => sizeCache.has(id))).toBe(false)
    expect(firstPageIDs.some(id => instance.elementsCache.has(id))).toBe(false)
    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('clears stale caches when equal-length pages replace rows without stable keys', async () => {
    const firstPage = Array.from({ length: 12 }, (_, i) => ({ name: `First ${i + 1}` }))
    const secondPage = Array.from({ length: 12 }, (_, i) => ({ name: `Second ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('conservatively clears caches when duplicate row-key multiplicity changes', async () => {
    const firstPage = [
      { id: 1, name: 'First A' },
      { id: 1, name: 'First B' },
      { id: 2, name: 'First C' }
    ]
    const secondPage = [
      { id: 1, name: 'Second A' },
      { id: 2, name: 'Second B' },
      { id: 2, name: 'Second C' }
    ]
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: firstPage,
        rowKey: 'id',
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureElementSpy = vi.spyOn(instance, 'measureElement')

    await wrapper.setProps({ data: secondPage })
    await wrapper.vm.$nextTick()

    expect(measureElementSpy.mock.calls.some(([node]) => node === null)).toBe(true)
  })

  it('preserves cache when rows without stable keys only reorder the same objects', async () => {
    const data = Array.from({ length: 12 }, (_, i) => ({ name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    const measureSpy = vi.spyOn(instance, 'measure')

    await wrapper.setProps({ data: [...data].reverse() })
    await wrapper.vm.$nextTick()

    expect(measureSpy).not.toHaveBeenCalled()
  })

  it('preserves stable row height cache when the same row IDs are only reordered', async () => {
    const data = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` }))
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data,
        rowKey: 'id',
        virtualizeThreshold: 1
      }
    })

    await wrapper.vm.$nextTick()

    const exposed = (wrapper.vm as any).virtualizer
    const instance = exposed?.value ?? exposed
    ;(instance as any).itemSizeCache = new Map(data.map(row => [row.id, 156]))
    const measureSpy = vi.spyOn(instance, 'measure')

    await wrapper.setProps({ data: [...data].reverse() })
    await wrapper.vm.$nextTick()

    const sizeCache = (instance as any).itemSizeCache as Map<number, number>
    expect(measureSpy).not.toHaveBeenCalled()
    expect(sizeCache.size).toBe(100)
  })

  it('emits controlled current-page selection while preserving off-page keys', async () => {
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: [
          { id: 1, name: 'One' },
          { id: 2, name: 'Two' }
        ],
        rowKey: 'id',
        selectable: true,
        selectedKeys: [99]
      }
    })

    await wrapper.get('[data-test="select-all"]').setValue(true)

    const selectedAll = wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]
    expect(selectedAll).toEqual([99, 1, 2])

    await wrapper.setProps({ selectedKeys: selectedAll as number[] })
    const rowCheckboxes = wrapper.findAll<HTMLInputElement>('[data-test="select-row"]')
    expect(rowCheckboxes.every((checkbox) => checkbox.element.checked)).toBe(true)

    await rowCheckboxes[0].setValue(false)

    expect(wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]).toEqual([99, 2])
    expect(wrapper.emitted('selectionChange')?.at(-1)?.[0]).toEqual([99, 2])
  })

  it('keeps the single usage field shrinkable in a 320px mobile card', () => {
    stubMobileMatchMedia()
    const viewport = document.createElement('div')
    viewport.style.width = '320px'
    document.body.appendChild(viewport)
    const wrapper = mount(DataTable, {
      attachTo: viewport,
      props: {
        columns: [{ key: 'usage', label: 'Usage' }],
        data: [{ id: 1, usage: 'snapshot' }],
        rowKey: 'id'
      },
      slots: {
        'cell-usage': '<div data-test="usage-cell">snapshot</div>'
      }
    })

    expect(viewport.style.width).toBe('320px')
    expect(wrapper.findAll('[data-field="usage"]')).toHaveLength(1)
    expect(wrapper.find('[data-field="ollama_cloud_usage"]').exists()).toBe(false)
    const field = wrapper.get('[data-field="usage"]')
    expect(field.classes()).toContain('min-w-0')
    expect(field.get('div').classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))
    expect(wrapper.findAll('[data-test="usage-cell"]')).toHaveLength(1)

    wrapper.unmount()
    viewport.remove()
  })

  it('offers current-page select all in the mobile card layout', async () => {
    stubMobileMatchMedia()
    const wrapper = mount(DataTable, {
      props: {
        columns: [{ key: 'name', label: 'Name' }],
        data: [
          { id: 1, name: 'One' },
          { id: 2, name: 'Two' }
        ],
        rowKey: 'id',
        selectable: true,
        selectedKeys: [99]
      }
    })

    await wrapper.get('[data-test="select-all-mobile"]').setValue(true)

    expect(wrapper.emitted('update:selectedKeys')?.at(-1)?.[0]).toEqual([99, 1, 2])
  })

  describe('row entrance animation', () => {
    // Desktop viewport, but prefers-reduced-motion does NOT match (motion allowed).
    const stubMotionSafeDesktopMatchMedia = () => {
      Object.defineProperty(window, 'matchMedia', {
        writable: true,
        value: vi.fn().mockImplementation((query: string) => ({
          matches: !query.includes('prefers-reduced-motion'),
          media: query,
          onchange: null,
          addEventListener: vi.fn(),
          removeEventListener: vi.fn(),
          addListener: vi.fn(),
          removeListener: vi.fn(),
          dispatchEvent: vi.fn()
        }))
      })
    }

    const makeRows = (count: number, offset = 0) =>
      Array.from({ length: count }, (_, i) => ({ id: offset + i + 1, name: `Row ${offset + i + 1}` }))

    const mountTable = (data: Array<Record<string, unknown>>, extraProps: Record<string, unknown> = {}) =>
      mount(DataTable, {
        props: {
          columns: [{ key: 'name', label: 'Name', sortable: true }],
          data,
          rowKey: 'id',
          ...extraProps
        }
      })

    const dataRows = (wrapper: ReturnType<typeof mount>) => wrapper.findAll('tbody tr[data-index]')
    const animatedRows = (wrapper: ReturnType<typeof mount>) =>
      dataRows(wrapper).filter((row) => row.classes().includes('dt-row-entrance'))

    afterEach(() => {
      vi.useRealTimers()
    })

    it('staggers row entrance on initial load, capped at the first 15 rows', async () => {
      stubMotionSafeDesktopMatchMedia()
      const wrapper = mountTable(makeRows(20))
      await wrapper.vm.$nextTick()

      const rows = dataRows(wrapper)
      expect(rows).toHaveLength(20)
      expect(rows[0].classes()).toContain('dt-row-entrance')
      expect(rows[1].attributes('style')).toContain('animation-delay: 25ms')
      expect(rows[14].classes()).toContain('dt-row-entrance')
      expect(rows[14].attributes('style')).toContain('animation-delay: 350ms')
      // Rows beyond the cap appear instantly.
      expect(rows[15].classes()).not.toContain('dt-row-entrance')
      expect(rows[15].attributes('style') ?? '').not.toContain('animation-delay')

      wrapper.unmount()
    })

    it('does not replay the entrance on sort or selection changes', async () => {
      vi.useFakeTimers()
      stubMotionSafeDesktopMatchMedia()
      const wrapper = mountTable(makeRows(5), { selectable: true, selectedKeys: [] })
      await wrapper.vm.$nextTick()
      expect(animatedRows(wrapper).length).toBeGreaterThan(0)

      // Entrance window expires; rows return to their inert state.
      vi.advanceTimersByTime(2000)
      await wrapper.vm.$nextTick()
      expect(animatedRows(wrapper)).toHaveLength(0)

      // Client-side sort reorders the same identity set: must stay instant.
      await wrapper.findAll('th')[1].trigger('click')
      await wrapper.vm.$nextTick()
      expect(animatedRows(wrapper)).toHaveLength(0)

      // Selection toggle is an unrelated reactive update: must stay instant.
      await wrapper.findAll('[data-test="select-row"]')[0].setValue(true)
      await wrapper.setProps({ selectedKeys: [1] })
      await wrapper.vm.$nextTick()
      expect(animatedRows(wrapper)).toHaveLength(0)

      wrapper.unmount()
    })

    it('replays the entrance when pagination swaps the row identity set', async () => {
      vi.useFakeTimers()
      stubMotionSafeDesktopMatchMedia()
      const wrapper = mountTable(makeRows(5))
      await wrapper.vm.$nextTick()

      vi.advanceTimersByTime(2000)
      await wrapper.vm.$nextTick()
      expect(animatedRows(wrapper)).toHaveLength(0)

      await wrapper.setProps({ data: makeRows(5, 100) })
      await wrapper.vm.$nextTick()
      await wrapper.vm.$nextTick()

      const rows = dataRows(wrapper)
      expect(rows[0].classes()).toContain('dt-row-entrance')
      expect(rows[2].attributes('style')).toContain('animation-delay: 50ms')

      wrapper.unmount()
    })

    it('renders rows instantly when animateRows is false', async () => {
      stubMotionSafeDesktopMatchMedia()
      const wrapper = mountTable(makeRows(5), { animateRows: false })
      await wrapper.vm.$nextTick()

      expect(dataRows(wrapper)).toHaveLength(5)
      expect(animatedRows(wrapper)).toHaveLength(0)

      await wrapper.setProps({ data: makeRows(5, 100) })
      await wrapper.vm.$nextTick()
      await wrapper.vm.$nextTick()
      expect(animatedRows(wrapper)).toHaveLength(0)

      wrapper.unmount()
    })

    it('renders rows instantly under prefers-reduced-motion', async () => {
      // Outer beforeEach stubs matchMedia with matches: true for every query,
      // so prefers-reduced-motion matches: the JS guard must disable the entrance.
      const wrapper = mountTable(makeRows(5))
      await wrapper.vm.$nextTick()

      expect(dataRows(wrapper)).toHaveLength(5)
      expect(animatedRows(wrapper)).toHaveLength(0)

      await wrapper.setProps({ data: makeRows(5, 100) })
      await wrapper.vm.$nextTick()
      await wrapper.vm.$nextTick()
      expect(animatedRows(wrapper)).toHaveLength(0)

      wrapper.unmount()
    })
  })

  describe('horizontal scroll edge fade hints', () => {
    const columns = [
      { key: 'name', label: 'Name' },
      { key: 'status', label: 'Status' }
    ]
    const data = [
      { id: 1, name: 'Alpha', status: 'ok' },
      { id: 2, name: 'Beta', status: 'ok' }
    ]

    const nextFrame = () =>
      new Promise((resolve) => {
        if (typeof requestAnimationFrame === 'function') requestAnimationFrame(() => resolve(null))
        else setTimeout(resolve, 0)
      })

    it('keeps both edge fades hidden when content does not overflow', async () => {
      const wrapper = mount(DataTable, { props: { columns, data, rowKey: 'id' } })
      await wrapper.vm.$nextTick()
      await wrapper.vm.$nextTick()

      // 渐变层常驻 DOM（靠 opacity 过渡显隐），但内容不溢出时不得带可见类
      expect(wrapper.findAll('.dt-edge-fade')).toHaveLength(2)
      expect(wrapper.find('.dt-edge-fade-visible').exists()).toBe(false)

      wrapper.unmount()
    })

    it('shows the matching fade for the scroll position when content overflows', async () => {
      const wrapper = mount(DataTable, { props: { columns, data, rowKey: 'id' } })
      await wrapper.vm.$nextTick()

      const scroller = wrapper.get('.table-wrapper')
      const el = scroller.element as HTMLElement
      Object.defineProperty(el, 'scrollWidth', { value: 600, configurable: true })
      Object.defineProperty(el, 'clientWidth', { value: 300, configurable: true })
      Object.defineProperty(el, 'scrollLeft', { value: 0, writable: true, configurable: true })

      // 在最左端：只有右侧提示可见
      await scroller.trigger('scroll')
      await nextFrame()
      await wrapper.vm.$nextTick()
      expect(wrapper.get('.dt-edge-fade-right').classes()).toContain('dt-edge-fade-visible')
      expect(wrapper.get('.dt-edge-fade-left').classes()).not.toContain('dt-edge-fade-visible')

      // 滚到中间：两侧提示都可见
      el.scrollLeft = 150
      await scroller.trigger('scroll')
      await nextFrame()
      await wrapper.vm.$nextTick()
      expect(wrapper.get('.dt-edge-fade-left').classes()).toContain('dt-edge-fade-visible')
      expect(wrapper.get('.dt-edge-fade-right').classes()).toContain('dt-edge-fade-visible')

      // 滚到最右端：右侧提示淡出，只剩左侧
      el.scrollLeft = 300
      await scroller.trigger('scroll')
      await nextFrame()
      await wrapper.vm.$nextTick()
      expect(wrapper.get('.dt-edge-fade-left').classes()).toContain('dt-edge-fade-visible')
      expect(wrapper.get('.dt-edge-fade-right').classes()).not.toContain('dt-edge-fade-visible')

      wrapper.unmount()
    })
  })
})
