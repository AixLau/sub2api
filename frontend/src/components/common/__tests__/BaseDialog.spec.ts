import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

let wrapper: VueWrapper | null = null

async function mountDialog(props: Record<string, unknown> = {}) {
  wrapper = mount(BaseDialog, {
    attachTo: document.body,
    props: {
      show: true,
      title: 'Test dialog',
      ...props
    },
    slots: {
      default: '<button data-testid="body-action">Body action</button>',
      footer: '<button data-testid="footer-action">Footer action</button>'
    }
  })
  await nextTick()
  return wrapper
}

async function mountEmptyDialog() {
  wrapper = mount(BaseDialog, {
    attachTo: document.body,
    props: {
      show: true,
      title: 'Empty dialog',
      showCloseButton: false
    }
  })
  await nextTick()
  return wrapper
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('BaseDialog', () => {
  it('closes when the backdrop itself is clicked by default', async () => {
    const mounted = await mountDialog()
    const overlay = document.querySelector<HTMLElement>('.modal-overlay')
    overlay?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(mounted.emitted('close')).toHaveLength(1)
  })

  it('keeps the backdrop non-dismissible when explicitly disabled', async () => {
    const mounted = await mountDialog({ closeOnClickOutside: false })
    const overlay = document.querySelector<HTMLElement>('.modal-overlay')
    overlay?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(mounted.emitted('close')).toBeUndefined()
  })

  it('does not close when dialog content is clicked', async () => {
    const mounted = await mountDialog()
    document.querySelector<HTMLElement>('[data-testid="body-action"]')
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(mounted.emitted('close')).toBeUndefined()
  })

  it('wraps focus backward from the first control to the last control', async () => {
    await mountDialog()
    const closeButton = document.querySelector<HTMLElement>('[aria-label="Close modal"]')
    const footerButton = document.querySelector<HTMLElement>('[data-testid="footer-action"]')
    closeButton?.focus()

    document.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: true,
      bubbles: true,
      cancelable: true
    }))

    expect(document.activeElement).toBe(footerButton)
  })

  it('wraps focus forward from the last control to the first control', async () => {
    await mountDialog()
    const closeButton = document.querySelector<HTMLElement>('[aria-label="Close modal"]')
    const footerButton = document.querySelector<HTMLElement>('[data-testid="footer-action"]')
    footerButton?.focus()

    document.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true
    }))

    expect(document.activeElement).toBe(closeButton)
  })

  it('keeps focus on the panel when there are no focusable controls', async () => {
    await mountEmptyDialog()
    const panel = document.querySelector<HTMLElement>('.modal-content')

    expect(document.activeElement).toBe(panel)

    document.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true
    }))

		expect(document.activeElement).toBe(panel)
	})

	it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

		expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
		wrapper.unmount()
	})
})
