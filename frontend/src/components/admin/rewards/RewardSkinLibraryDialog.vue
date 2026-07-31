<template>
  <BaseDialog
    :show="show"
    :title="t('admin.rewards.skins.title')"
    width="extra-wide"
    @close="emit('close')"
  >
    <div class="space-y-6">
      <RewardSkinPicker
        :model-value="[]"
        :skins="skins"
        :show-selection="false"
        :uploading="uploading"
        :reset-token="resetToken"
        @upload="(file, metadata, fallback) => emit('upload', file, metadata, fallback)"
      />

      <div class="border-t border-gray-200 pt-5 dark:border-dark-700">
        <div class="mb-3 flex items-center justify-between gap-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.rewards.skins.libraryTitle') }}
            </h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.rewards.skins.libraryHint') }}
            </p>
          </div>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.rewards.skins.count', { count: skins.length }) }}
          </span>
        </div>

        <div v-if="skins.length" class="space-y-2">
          <div
            v-for="skin in skins"
            :key="skin.id"
            class="grid grid-cols-1 gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700 md:grid-cols-[180px_minmax(0,1fr)_auto]"
          >
            <div class="aspect-[1320/500] overflow-hidden rounded-md bg-gray-100 dark:bg-dark-900">
              <img :src="skin.image_url" :alt="skin.alt_text" class="h-full w-full object-cover" />
            </div>

            <div v-if="editingId === skin.id" class="grid min-w-0 grid-cols-1 gap-2 lg:grid-cols-2">
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.skins.name') }}</label>
                <input v-model.trim="editForm.name" type="text" class="input h-9" maxlength="80" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.rewards.editor.skins.altText') }}</label>
                <input v-model.trim="editForm.alt_text" type="text" class="input h-9" maxlength="160" />
              </div>
              <div class="lg:col-span-2">
                <label class="input-label">{{ t('admin.rewards.editor.skins.description') }}</label>
                <input v-model.trim="editForm.description" type="text" class="input h-9" />
              </div>
            </div>

            <div v-else class="min-w-0 self-center">
              <div class="flex flex-wrap items-center gap-2">
                <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ skin.name }}</p>
                <span :class="skin.status === 'enabled' ? 'badge badge-success' : skin.status === 'archived' ? 'badge badge-gray' : 'badge badge-warning'">
                  {{ t(`admin.rewards.skinStatuses.${skin.status}`) }}
                </span>
              </div>
              <p class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400">{{ skin.alt_text }}</p>
              <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                {{ skin.width }} x {{ skin.height }} · {{ formatSize(skin.size_bytes) }} · {{ skin.mime_type }}
              </p>
            </div>

            <div class="flex items-center justify-end gap-1 self-center">
              <template v-if="editingId === skin.id">
                <button type="button" class="btn btn-secondary btn-sm" @click="cancelEdit">
                  {{ t('common.cancel') }}
                </button>
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  :disabled="!editForm.name || !editForm.alt_text"
                  @click="saveEdit(skin)"
                >
                  {{ t('common.save') }}
                </button>
              </template>
              <template v-else>
                <label
                  v-if="skin.status !== 'archived'"
                  class="mr-1 flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-300"
                >
                  <input
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    :checked="skin.status === 'enabled'"
                    @change="toggleEnabled(skin, ($event.target as HTMLInputElement).checked)"
                  />
                  {{ t('admin.rewards.skins.enabled') }}
                </label>
                <button
                  type="button"
                  class="icon-action"
                  :title="t('common.edit')"
                  :aria-label="t('common.edit')"
                  @click="startEdit(skin)"
                >
                  <Icon name="edit" size="sm" />
                </button>
                <button
                  v-if="skin.status !== 'archived'"
                  type="button"
                  class="icon-action"
                  :title="t('admin.rewards.actions.archive')"
                  :aria-label="t('admin.rewards.actions.archive')"
                  @click="archiveCandidate = skin"
                >
                  <Icon name="inbox" size="sm" />
                </button>
              </template>
            </div>
          </div>
        </div>
        <div
          v-else
          class="rounded-lg border border-dashed border-gray-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
        >
          {{ t('admin.rewards.skins.empty') }}
        </div>
      </div>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">
        {{ t('common.close') }}
      </button>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="!!archiveCandidate"
    :title="t('admin.rewards.skins.archiveTitle')"
    :message="t('admin.rewards.skins.archiveMessage', { name: archiveCandidate?.name ?? '' })"
    :confirm-text="t('admin.rewards.actions.archive')"
    @confirm="confirmArchive"
    @cancel="archiveCandidate = null"
  />
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import RewardSkinPicker from './RewardSkinPicker.vue'
import type { RewardSkin, RewardSkinUploadMetadata } from '@/api/admin/rewards'

withDefaults(defineProps<{
  show: boolean
  skins: RewardSkin[]
  uploading?: boolean
  resetToken?: number
}>(), {
  uploading: false,
  resetToken: 0
})

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'upload', file: File, metadata: RewardSkinUploadMetadata, canvasFallback: boolean): void
  (
    event: 'update',
    skin: RewardSkin,
    payload: Pick<RewardSkin, 'name' | 'description' | 'alt_text' | 'status'>
  ): void
  (event: 'archive', skin: RewardSkin): void
}>()

const { t } = useI18n()
const editingId = ref<number | null>(null)
const archiveCandidate = ref<RewardSkin | null>(null)
const editForm = reactive({
  name: '',
  description: '',
  alt_text: ''
})

function startEdit(skin: RewardSkin) {
  editingId.value = skin.id
  editForm.name = skin.name
  editForm.description = skin.description
  editForm.alt_text = skin.alt_text
}

function cancelEdit() {
  editingId.value = null
}

function saveEdit(skin: RewardSkin) {
  emit('update', skin, {
    name: editForm.name.trim(),
    description: editForm.description.trim(),
    alt_text: editForm.alt_text.trim(),
    status: skin.status
  })
  editingId.value = null
}

function toggleEnabled(skin: RewardSkin, enabled: boolean) {
  emit('update', skin, {
    name: skin.name,
    description: skin.description,
    alt_text: skin.alt_text,
    status: enabled ? 'enabled' : 'disabled'
  })
}

function confirmArchive() {
  if (archiveCandidate.value) emit('archive', archiveCandidate.value)
  archiveCandidate.value = null
}

function formatSize(bytes: number) {
  return `${Math.max(0, bytes / 1024).toFixed(0)} KB`
}
</script>

<style scoped>
.icon-action {
  @apply inline-flex h-8 w-8 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-gray-200;
}
</style>
