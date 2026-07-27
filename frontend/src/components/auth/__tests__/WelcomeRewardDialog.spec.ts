import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import WelcomeRewardDialog from '../WelcomeRewardDialog.vue'

const fireCelebrationMock = vi.hoisted(() => vi.fn())

vi.mock('@/components/inspira/confetti', () => ({
  fireCelebration: fireCelebrationMock
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const BaseDialogStub = defineComponent({
  props: {
    show: Boolean,
    title: String,
    closeOnEscape: Boolean,
    closeOnClickOutside: Boolean,
    showCloseButton: Boolean
  },
  emits: ['close'],
  template: `
    <section>
      <button data-testid="dialog-close" @click="$emit('close')">close</button>
      <slot />
      <slot name="footer" />
    </section>
  `
})

const ScratchToRevealStub = defineComponent({
  props: {
    coverColor: String,
    coverImage: String,
    coverTextColor: String
  },
  emits: ['complete'],
  template: `
    <button data-testid="scratch" @click="$emit('complete')">
      <slot />
    </button>
  `
})

function mountDialog() {
  return mount(WelcomeRewardDialog, {
    props: { show: true, amount: 4, skinId: 'lucky-passage' },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ScratchToReveal: ScratchToRevealStub,
        Icon: true
      }
    }
  })
}

describe('WelcomeRewardDialog', () => {
  beforeEach(() => {
    fireCelebrationMock.mockReset()
  })

  it('requires the user to reveal the reward before finishing', async () => {
    const wrapper = mountDialog()

    await wrapper.get('[data-testid="dialog-close"]').trigger('click')
    expect(wrapper.emitted('finish')).toBeUndefined()

    await wrapper.get('[data-testid="scratch"]').trigger('click')
    expect(fireCelebrationMock).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('¥4.00')
    expect(wrapper.text()).toContain('welcomeReward.revealedHint')

    await wrapper.get('button.btn-primary').trigger('click')
    expect(wrapper.emitted('finish')).toHaveLength(1)
  })

  it('fires the celebration only once', async () => {
    const wrapper = mountDialog()
    const scratch = wrapper.get('[data-testid="scratch"]')

    await scratch.trigger('click')
    await scratch.trigger('click')

    expect(fireCelebrationMock).toHaveBeenCalledTimes(1)
  })

  it('passes the selected IP skin to the scratch cover', () => {
    const wrapper = mountDialog()
    const scratch = wrapper.getComponent(ScratchToRevealStub)

    expect(scratch.props('coverImage')).toContain('lucky-passage.jpg')
    expect(scratch.props('coverColor')).toBe('#13263a')
    expect(scratch.props('coverTextColor')).toBe('#fff5dd')
  })
})
