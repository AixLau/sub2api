<template>
  <div class="space-y-5">
    <div v-if="showSelection">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.rewards.editor.skins.selectedTitle') }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.rewards.editor.skins.selectedHint') }}
          </p>
        </div>
        <span class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.rewards.editor.skins.totalWeight', { value: totalWeight }) }}
        </span>
      </div>

      <div
        v-if="availableSkins.length"
        class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2"
      >
        <label
          v-for="skin in availableSkins"
          :key="skin.id"
          :class="[
            'grid cursor-pointer grid-cols-[112px_1fr] gap-3 rounded-lg border p-3 transition-colors',
            allocationFor(skin.id)
              ? 'border-primary-300 bg-primary-50/40 dark:border-primary-700 dark:bg-primary-900/10'
              : 'border-gray-200 bg-white hover:border-gray-300 dark:border-dark-700 dark:bg-dark-900 dark:hover:border-dark-600'
          ]"
        >
          <div class="aspect-[1320/500] overflow-hidden rounded-md bg-gray-100 dark:bg-dark-800">
            <img
              :src="skin.image_url"
              :alt="skin.alt_text"
              class="h-full w-full object-cover"
            />
          </div>
          <div class="min-w-0">
            <div class="flex items-start gap-2">
              <input
                type="checkbox"
                class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                :checked="!!allocationFor(skin.id)"
                @change="toggleSkin(skin.id, ($event.target as HTMLInputElement).checked)"
              />
              <div class="min-w-0">
                <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ skin.name }}</p>
                <p class="truncate text-xs text-gray-500 dark:text-dark-400">{{ skin.alt_text }}</p>
              </div>
            </div>
            <div v-if="allocationFor(skin.id)" class="mt-2">
              <label class="input-label">{{ t('admin.rewards.editor.skins.weight') }}</label>
              <input
                :value="allocationFor(skin.id)?.weight"
                type="number"
                min="1"
                step="1"
                class="input h-8"
                @click.stop
                @input="setWeight(skin.id, Number(($event.target as HTMLInputElement).value))"
              />
            </div>
          </div>
        </label>
      </div>
      <div
        v-else
        class="mt-3 rounded-lg border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
      >
        {{ t('admin.rewards.editor.skins.empty') }}
      </div>
    </div>

    <div :class="showSelection ? 'border-t border-gray-200 pt-5 dark:border-dark-700' : ''">
      <div class="flex items-center justify-between gap-3">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.rewards.editor.skins.uploadTitle') }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.rewards.editor.skins.uploadHint') }}
          </p>
        </div>
        <label class="btn btn-secondary cursor-pointer">
          <Icon name="upload" size="sm" class="mr-1" />
          {{ t('admin.rewards.editor.skins.chooseImage') }}
          <input
            type="file"
            class="hidden"
            accept="image/png,image/jpeg,image/webp"
            @change="selectFile"
          />
        </label>
      </div>

      <div v-if="previewUrl" class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1.35fr)_minmax(260px,1fr)]">
        <div>
          <div class="aspect-[1320/500] overflow-hidden rounded-lg border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-900">
            <img
              :src="previewUrl"
              :alt="uploadForm.alt_text"
              class="h-full w-full object-cover"
            />
          </div>
          <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-dark-400">
            <span>{{ processedFile?.name }}</span>
            <span>{{ formatFileSize(processedFile?.size ?? 0) }}</span>
            <span>{{ canvasFallback ? t('admin.rewards.editor.skins.originalDimensions') : '1320 x 500' }}</span>
          </div>
          <p
            v-if="canvasFallback"
            class="mt-2 rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
          >
            {{ t('admin.rewards.editor.skins.canvasFallback') }}
          </p>
        </div>

        <div class="space-y-3">
          <div>
            <label class="input-label">{{ t('admin.rewards.editor.skins.name') }}</label>
            <input v-model.trim="uploadForm.name" type="text" class="input" maxlength="80" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.rewards.editor.skins.altText') }}</label>
            <input v-model.trim="uploadForm.alt_text" type="text" class="input" maxlength="160" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.rewards.editor.skins.description') }}</label>
            <textarea v-model.trim="uploadForm.description" rows="2" class="input"></textarea>
          </div>
          <button
            type="button"
            class="btn btn-primary w-full"
            :disabled="uploading || !canUpload"
            @click="submitUpload"
          >
            <Icon name="upload" size="sm" class="mr-1" />
            {{ uploading ? t('admin.rewards.editor.skins.uploading') : t('admin.rewards.editor.skins.upload') }}
          </button>
        </div>
      </div>

      <p v-if="processing" class="mt-3 text-sm text-gray-500 dark:text-dark-400">
        {{ t('admin.rewards.editor.skins.processing') }}
      </p>
      <p v-if="error" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type {
  RewardSkin,
  RewardSkinAllocation,
  RewardSkinUploadMetadata
} from '@/api/admin/rewards'

const MAX_BYTES = 1024 * 1024
const TARGET_WIDTH = 1320
const TARGET_HEIGHT = 500
const ACCEPTED_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp'])

class CanvasUnavailableResizeRequiredError extends Error {
  readonly code = 'CANVAS_UNAVAILABLE_RESIZE_REQUIRED'

  constructor() {
    super('canvas unavailable and source image does not match the required dimensions')
    this.name = 'CanvasUnavailableResizeRequiredError'
  }
}

const props = withDefaults(defineProps<{
  modelValue: RewardSkinAllocation[]
  skins: RewardSkin[]
  uploading?: boolean
  resetToken?: number
  showSelection?: boolean
}>(), {
  uploading: false,
  resetToken: 0,
  showSelection: true
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: RewardSkinAllocation[]): void
  (event: 'upload', file: File, metadata: RewardSkinUploadMetadata, canvasFallback: boolean): void
}>()

const { t } = useI18n()
const processing = ref(false)
const error = ref('')
const processedFile = ref<File | null>(null)
const previewUrl = ref('')
const canvasFallback = ref(false)

const uploadForm = reactive({
  name: '',
  alt_text: '',
  description: ''
})

const availableSkins = computed(() => props.skins.filter((skin) => skin.status === 'enabled'))
const totalWeight = computed(() =>
  props.modelValue.reduce((sum, allocation) => sum + Math.max(0, Number(allocation.weight) || 0), 0)
)
const canUpload = computed(() =>
  !!processedFile.value && !!uploadForm.name.trim() && !!uploadForm.alt_text.trim()
)

watch(() => props.resetToken, resetUpload)

function allocationFor(skinId: number) {
  return props.modelValue.find((allocation) => allocation.skin_id === skinId)
}

function updateAllocations(mutator: (draft: RewardSkinAllocation[]) => void) {
  const draft = props.modelValue.map((allocation) => ({ ...allocation }))
  mutator(draft)
  emit('update:modelValue', draft)
}

function toggleSkin(skinId: number, selected: boolean) {
  updateAllocations((draft) => {
    const index = draft.findIndex((allocation) => allocation.skin_id === skinId)
    if (selected && index < 0) draft.push({ skin_id: skinId, weight: 1 })
    if (!selected && index >= 0) draft.splice(index, 1)
  })
}

function setWeight(skinId: number, weight: number) {
  updateAllocations((draft) => {
    const allocation = draft.find((item) => item.skin_id === skinId)
    if (allocation) allocation.weight = Math.max(1, Math.round(Number.isFinite(weight) ? weight : 1))
  })
}

async function selectFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  error.value = ''
  clearProcessedFile()
  if (!ACCEPTED_TYPES.has(file.type)) {
    error.value = t('admin.rewards.editor.skins.invalidType')
    return
  }
  if (file.size > MAX_BYTES) {
    error.value = t('admin.rewards.editor.skins.tooLarge')
    return
  }

  processing.value = true
  try {
    const result = await centerCrop(file)
    setProcessedFile(result.file, result.fallback)
    const baseName = file.name.replace(/\.[^.]+$/, '')
    uploadForm.name = baseName.slice(0, 80)
    uploadForm.alt_text = baseName.replace(/[-_]+/g, ' ').slice(0, 160)
    uploadForm.description = ''
  } catch (cause) {
    if (cause instanceof CanvasUnavailableResizeRequiredError) {
      error.value = t('admin.rewards.editor.skins.canvasUnavailableResizeRequired')
    } else {
      console.error('Failed to crop reward skin:', cause)
      error.value = t('admin.rewards.editor.skins.processFailed')
    }
  } finally {
    processing.value = false
  }
}

async function centerCrop(file: File): Promise<{ file: File; fallback: boolean }> {
  const image = await decodeImage(file)
  const canvas = document.createElement?.('canvas')
  const context = canvas?.getContext?.('2d')
  if (!canvas || !context || typeof canvas.toBlob !== 'function') {
    if (
      image.naturalWidth === TARGET_WIDTH &&
      image.naturalHeight === TARGET_HEIGHT &&
      file.size <= MAX_BYTES
    ) {
      return { file, fallback: true }
    }
    throw new CanvasUnavailableResizeRequiredError()
  }

  canvas.width = TARGET_WIDTH
  canvas.height = TARGET_HEIGHT

  const targetRatio = TARGET_WIDTH / TARGET_HEIGHT
  const sourceRatio = image.naturalWidth / image.naturalHeight
  let sourceX = 0
  let sourceY = 0
  let sourceWidth = image.naturalWidth
  let sourceHeight = image.naturalHeight

  if (sourceRatio > targetRatio) {
    sourceWidth = image.naturalHeight * targetRatio
    sourceX = (image.naturalWidth - sourceWidth) / 2
  } else {
    sourceHeight = image.naturalWidth / targetRatio
    sourceY = (image.naturalHeight - sourceHeight) / 2
  }

  context.drawImage(
    image,
    sourceX,
    sourceY,
    sourceWidth,
    sourceHeight,
    0,
    0,
    TARGET_WIDTH,
    TARGET_HEIGHT
  )

  const webpSupported = canvas.toDataURL('image/webp').startsWith('data:image/webp')
  const outputType = webpSupported ? 'image/webp' : 'image/jpeg'
  const extension = webpSupported ? 'webp' : 'jpg'
  const baseName = file.name.replace(/\.[^.]+$/, '')

  for (const quality of [0.88, 0.76, 0.64, 0.52]) {
    const blob = await canvasToBlob(canvas, outputType, quality)
    if (blob && blob.size <= MAX_BYTES) {
      return {
        file: new File([blob], `${baseName}.${extension}`, {
          type: outputType,
          lastModified: Date.now()
        }),
        fallback: false
      }
    }
  }
  throw new Error('cropped skin exceeds maximum size')
}

function decodeImage(file: File): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    const url = URL.createObjectURL(file)
    image.onload = () => {
      URL.revokeObjectURL(url)
      resolve(image)
    }
    image.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error('image decode failed'))
    }
    image.src = url
  })
}

function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, type, quality))
}

function setProcessedFile(file: File, fallback: boolean) {
  clearProcessedFile()
  processedFile.value = file
  previewUrl.value = URL.createObjectURL(file)
  canvasFallback.value = fallback
}

function clearProcessedFile() {
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
  previewUrl.value = ''
  processedFile.value = null
  canvasFallback.value = false
}

function submitUpload() {
  if (!processedFile.value || !canUpload.value) return
  emit(
    'upload',
    processedFile.value,
    {
      name: uploadForm.name.trim(),
      alt_text: uploadForm.alt_text.trim(),
      description: uploadForm.description.trim()
    },
    canvasFallback.value
  )
}

function resetUpload() {
  clearProcessedFile()
  error.value = ''
  uploadForm.name = ''
  uploadForm.alt_text = ''
  uploadForm.description = ''
}

function formatFileSize(bytes: number) {
  return `${Math.max(0, bytes / 1024).toFixed(0)} KB`
}

onUnmounted(() => {
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
})
</script>
