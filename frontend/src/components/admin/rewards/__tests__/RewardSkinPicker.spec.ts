import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RewardSkinPicker from '../RewardSkinPicker.vue'

const mocks = vi.hoisted(() => ({
  dimensions: {
    width: 1320,
    height: 500
  },
  messages: {
    'admin.rewards.editor.skins.canvasUnavailableResizeRequired':
      'This browser cannot crop images. Choose an image already 1320 × 500 and no larger than 1 MB, or use a browser with Canvas support.',
    'admin.rewards.editor.skins.canvasFallback':
      'Canvas is unavailable in this browser. The original image is already 1320 × 500 and will be uploaded without cropping.'
  } as Record<string, string>
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => mocks.messages[key] ?? key
  })
}))

class MockImage {
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  naturalWidth = mocks.dimensions.width
  naturalHeight = mocks.dimensions.height

  set src(_value: string) {
    queueMicrotask(() => this.onload?.())
  }
}

function mountPicker() {
  return mount(RewardSkinPicker, {
    props: {
      modelValue: [],
      skins: [],
      showSelection: false
    },
    global: {
      stubs: {
        Icon: true
      }
    }
  })
}

async function selectImage(wrapper: VueWrapper, file: File) {
  const input = wrapper.get<HTMLInputElement>('input[type="file"]')
  Object.defineProperty(input.element, 'files', {
    configurable: true,
    value: [file]
  })
  await input.trigger('change')
  await flushPromises()
}

describe('RewardSkinPicker Canvas fallback', () => {
  beforeEach(() => {
    mocks.dimensions.width = 1320
    mocks.dimensions.height = 500
    vi.stubGlobal('Image', MockImage)
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)

    let objectUrlSequence = 0
    vi.spyOn(URL, 'createObjectURL').mockImplementation(
      () => `blob:reward-skin-${++objectUrlSequence}`
    )
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('uploads the original exact-sized file when Canvas is unavailable', async () => {
    const wrapper = mountPicker()
    const file = new File([new Uint8Array(256)], 'exact-skin.png', {
      type: 'image/png'
    })

    await selectImage(wrapper, file)

    expect(URL.createObjectURL).toHaveBeenCalledTimes(2)
    expect(URL.createObjectURL).toHaveBeenNthCalledWith(1, file)
    expect(URL.createObjectURL).toHaveBeenNthCalledWith(2, file)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:reward-skin-1')
    expect(wrapper.text()).toContain(mocks.messages['admin.rewards.editor.skins.canvasFallback'])

    const uploadButton = wrapper.get('button')
    expect(uploadButton.attributes('disabled')).toBeUndefined()
    await uploadButton.trigger('click')

    expect(wrapper.emitted('upload')).toEqual([
      [
        file,
        {
          name: 'exact-skin',
          alt_text: 'exact skin',
          description: ''
        },
        true
      ]
    ])
  })

  it('rejects a differently sized image with actionable guidance', async () => {
    mocks.dimensions.width = 1200
    const wrapper = mountPicker()
    const file = new File([new Uint8Array(256)], 'needs-crop.png', {
      type: 'image/png'
    })

    await selectImage(wrapper, file)

    expect(wrapper.text()).toContain(
      mocks.messages['admin.rewards.editor.skins.canvasUnavailableResizeRequired']
    )
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.emitted('upload')).toBeUndefined()
  })

  it('clears a previous valid file before rejecting a new invalid image', async () => {
    const wrapper = mountPicker()
    const validFile = new File([new Uint8Array(256)], 'valid.png', {
      type: 'image/png'
    })
    await selectImage(wrapper, validFile)
    expect(wrapper.find('button').exists()).toBe(true)

    mocks.dimensions.height = 480
    const invalidFile = new File([new Uint8Array(256)], 'invalid.png', {
      type: 'image/png'
    })
    await selectImage(wrapper, invalidFile)

    expect(wrapper.text()).toContain(
      mocks.messages['admin.rewards.editor.skins.canvasUnavailableResizeRequired']
    )
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('valid.png')
    expect(wrapper.emitted('upload')).toBeUndefined()
  })
})
