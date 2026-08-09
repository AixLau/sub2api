<template>
  <div class="flex items-start gap-4">
    <!-- Preview Box -->
    <div class="flex-shrink-0">
      <div
        class="iu-dropzone flex items-center justify-center overflow-hidden rounded-xl border-2 border-dashed border-gray-300 bg-gray-50 dark:border-dark-600 dark:bg-dark-800"
        :class="[previewSizeClass, { 'border-solid': !!modelValue, 'is-dragover': isDragOver }]"
        @dragenter="handleDragEnter"
        @dragover.prevent
        @dragleave="handleDragLeave"
        @drop="handleDrop"
      >
        <!-- SVG mode: render inline -->
        <span
          v-if="mode === 'svg' && modelValue"
          :key="modelValue"
          class="iu-media iu-preview-in text-gray-600 dark:text-gray-300 [&>svg]:h-full [&>svg]:w-full"
          :class="innerSizeClass"
          v-html="sanitizedValue"
        ></span>
        <!-- Image mode: show as img -->
        <img
          v-else-if="mode === 'image' && modelValue"
          :key="modelValue"
          :src="modelValue"
          alt=""
          class="iu-media iu-preview-in h-full w-full object-contain"
        />
        <!-- Empty placeholder -->
        <svg
          v-else
          class="iu-media text-gray-400 dark:text-dark-500"
          :class="placeholderSizeClass"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
          />
        </svg>
      </div>
    </div>

    <!-- Controls -->
    <div class="flex-1 space-y-2">
      <div class="flex items-center gap-2">
        <label class="btn btn-secondary btn-sm cursor-pointer">
          <input
            type="file"
            :accept="acceptTypes"
            class="hidden"
            @change="handleUpload"
          />
          <Icon name="upload" size="sm" class="mr-1.5" :stroke-width="2" />
          {{ resolvedUploadLabel }}
        </label>
        <button
          v-if="modelValue"
          type="button"
          class="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400"
          @click="$emit('update:modelValue', '')"
        >
          <Icon name="trash" size="sm" class="mr-1.5" :stroke-width="2" />
          {{ resolvedRemoveLabel }}
        </button>
      </div>
      <p v-if="hint" class="text-xs text-gray-500 dark:text-gray-400">{{ hint }}</p>
      <p v-if="error" class="text-xs text-red-500">{{ error }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeSvg } from '@/utils/sanitize'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  modelValue: string
  mode?: 'image' | 'svg'
  size?: 'sm' | 'md'
  uploadLabel?: string
  removeLabel?: string
  hint?: string
  maxSize?: number // bytes
}>(), {
  mode: 'image',
  size: 'md',
  uploadLabel: '',
  removeLabel: '',
  hint: '',
  maxSize: 300 * 1024,
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const error = ref('')

const resolvedUploadLabel = computed(() => props.uploadLabel || t('common.upload'))
const resolvedRemoveLabel = computed(() => props.removeLabel || t('common.remove'))

const acceptTypes = computed(() => props.mode === 'svg' ? '.svg' : 'image/*')

const sanitizedValue = computed(() =>
  props.mode === 'svg' ? sanitizeSvg(props.modelValue ?? '') : ''
)

const previewSizeClass = computed(() => props.size === 'sm' ? 'h-14 w-14' : 'h-20 w-20')
const innerSizeClass = computed(() => props.size === 'sm' ? 'h-7 w-7' : 'h-12 w-12')
const placeholderSizeClass = computed(() => props.size === 'sm' ? 'h-5 w-5' : 'h-8 w-8')

// Drag-over visual state. dragenter/dragleave bubble from child elements and
// would flicker, so track depth with a counter instead of a boolean toggle.
const isDragOver = ref(false)
let dragDepth = 0

function handleDragEnter(event: DragEvent) {
  event.preventDefault()
  dragDepth++
  isDragOver.value = true
}

function handleDragLeave(event: DragEvent) {
  event.preventDefault()
  dragDepth = Math.max(0, dragDepth - 1)
  if (dragDepth === 0) isDragOver.value = false
}

function handleDrop(event: DragEvent) {
  event.preventDefault()
  dragDepth = 0
  isDragOver.value = false
  const file = event.dataTransfer?.files?.[0]
  if (file) processFile(file)
}

function handleUpload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]

  if (!file) return

  processFile(file)
  input.value = ''
}

// Existing upload logic, unchanged — extracted so drag-and-drop and the file
// input share the same validation/emit path.
function processFile(file: File) {
  error.value = ''

	if (props.maxSize && file.size > props.maxSize) {
		error.value = t('common.fileTooLargeKb', {
			size: (file.size / 1024).toFixed(1),
			max: (props.maxSize / 1024).toFixed(0)
		})
		return
  }

  const reader = new FileReader()
  if (props.mode === 'svg') {
    // The file input enforces accept=".svg", but drag-and-drop bypasses it.
    if (file.type !== 'image/svg+xml' && !file.name.toLowerCase().endsWith('.svg')) {
      error.value = 'Please select an SVG file'
      return
    }
    reader.onload = (e) => {
      const text = e.target?.result as string
      if (text) emit('update:modelValue', text.trim())
    }
    reader.readAsText(file)
	} else {
		if (!file.type.startsWith('image/')) {
			error.value = t('common.selectImageFile')
			return
    }
    reader.onload = (e) => {
      emit('update:modelValue', e.target?.result as string)
    }
    reader.readAsDataURL(file)
  }

  reader.onerror = () => {
    error.value = t('common.fileReadFailed')
  }
}
</script>

<style scoped>
/* Inspira-style drag-over feedback: primary dashed border, soft glow, faint
   primary wash. Scoped selectors out-rank Tailwind utilities on the same node. */
.iu-dropzone {
  transition:
    border-color 200ms ease,
    background-color 200ms ease,
    box-shadow 200ms ease;
}

.iu-dropzone.is-dragover {
  border-color: rgb(var(--color-brand-500));
  border-style: dashed;
  background-color: rgb(var(--color-brand-500) / 0.07);
  box-shadow:
    0 0 0 1px rgb(var(--color-brand-500) / 0.45),
    0 0 16px rgb(var(--color-brand-400) / 0.28);
}

.dark .iu-dropzone.is-dragover {
  border-color: rgb(var(--color-brand-400));
  background-color: rgb(var(--color-brand-400) / 0.12);
  box-shadow:
    0 0 0 1px rgb(var(--color-brand-400) / 0.4),
    0 0 18px rgb(var(--color-brand-400) / 0.25);
}

/* Inner icon / preview lifts and scales slightly while dragging over. */
.iu-media {
  transition: transform 200ms ease;
}

.iu-dropzone.is-dragover .iu-media {
  transform: translateY(-2px) scale(1.03);
}

/* Newly selected / dropped preview pops in with a quick scale-fade. */
.iu-preview-in {
  animation: iu-pop-in 200ms ease-out;
}

@keyframes iu-pop-in {
  from {
    opacity: 0;
    transform: scale(0.92);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .iu-dropzone,
  .iu-media {
    transition: none;
  }

  .iu-preview-in {
    animation: none;
  }

  .iu-dropzone.is-dragover .iu-media {
    transform: none;
  }
}
</style>
