<template>
  <BaseDialog
    :show="show"
    :title="t(`${copyPrefix}.title`)"
    width="normal"
    :close-on-escape="amount !== null"
    :close-on-click-outside="amount !== null"
    :show-close-button="amount !== null"
    :auto-focus="false"
    :z-index="100000000"
    @close="finish"
  >
    <div class="pb-1 text-center">
      <div
        class="mx-auto flex h-12 w-12 items-center justify-center rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-950/50 dark:text-blue-300"
      >
        <Icon name="gift" size="lg" />
      </div>
      <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-dark-300">
        {{ t(`${copyPrefix}.${amount !== null ? 'revealedHint' : 'scratchHint'}`) }}
      </p>
    </div>

    <div
      v-if="!skinReady"
      class="mt-5 flex min-h-[11rem] items-center justify-center overflow-hidden rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-900"
    >
      <LoadingSpinner size="lg" variant="orbit" />
    </div>

    <ScratchToReveal
      v-else
      class="mt-5 min-h-[11rem] overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900"
      :cover-color="skin.coverColor"
      :cover-image="skin.coverImage"
      :cover-text-color="skin.coverTextColor"
      :cover-text="t(`${copyPrefix}.coverText`)"
      :threshold="0.42"
      :radius="28"
      @complete="revealReward"
    >
      <div class="flex min-h-[11rem] flex-col items-center justify-center px-6 py-5">
        <LoadingSpinner v-if="claiming" size="lg" variant="orbit" />
        <template v-else-if="amount !== null">
          <div
            class="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300"
          >
            <Icon name="sparkles" size="xs" />
            {{ t(`${copyPrefix}.won`) }}
          </div>
          <p class="mt-3 text-5xl font-bold text-gray-950 dark:text-white">
            <span class="mr-1 text-2xl font-semibold text-blue-600 dark:text-blue-400">$</span
            >{{ amount.toFixed(2) }}
          </p>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
            {{ t(`${copyPrefix}.credited`) }}
          </p>
        </template>
        <template v-else-if="claimFailed">
          <p class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t(`${copyPrefix}.claimFailed`) }}
          </p>
          <button type="button" class="btn btn-secondary mt-4" @click="revealReward">
            {{ t('common.retry') }}
          </button>
        </template>
        <div
          v-else
          class="flex h-14 w-14 items-center justify-center rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-950/50 dark:text-blue-300"
          aria-hidden="true"
        >
          <Icon name="gift" size="lg" />
        </div>
      </div>
    </ScratchToReveal>

    <p v-if="amount === null" class="mt-3 text-center text-xs text-gray-500 dark:text-dark-400">
      {{ t(`${copyPrefix}.gestureHint`) }}
    </p>

    <template v-if="amount !== null" #footer>
      <button type="button" class="btn btn-primary w-full" @click="finish">
        {{ t(`${copyPrefix}.continue`) }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import ScratchToReveal from '@/components/inspira/ScratchToReveal.vue'
import { fireCelebration } from '@/components/inspira/confetti'
import { useAuthStore } from '@/stores/auth'
import {
  welcomeRewardSkins,
  type WelcomeRewardSkinId
} from '@/components/auth/welcomeRewardSkins'

const props = withDefaults(defineProps<{
  show: boolean
  skinId: WelcomeRewardSkinId
  variant?: 'welcome' | 'surprise'
}>(), {
  variant: 'welcome'
})

const emit = defineEmits<{
  (event: 'finish'): void
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const copyPrefix = computed(() =>
  props.variant === 'surprise' ? 'surpriseReward' : 'welcomeReward'
)
const amount = ref<number | null>(null)
const claiming = ref(false)
const claimFailed = ref(false)
const skinReady = ref(false)
let skinLoadRequest = 0
const skin = computed(
  () => welcomeRewardSkins.find((candidate) => candidate.id === props.skinId) ?? welcomeRewardSkins[0]
)

watch(
  () => skin.value.coverImage,
  (source) => {
    skinReady.value = false
    const request = ++skinLoadRequest
    if (typeof Image !== 'function') {
      skinReady.value = true
      return
    }
    const image = new Image()
    image.decoding = 'async'
    image.onload = async () => {
      try {
        await image.decode?.()
      } catch {
        // onload 已确认图片可用，decode 失败不阻塞展示。
      }
      if (request === skinLoadRequest) skinReady.value = true
    }
    image.onerror = () => {
      if (request === skinLoadRequest) skinReady.value = true
    }
    image.src = source
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  skinLoadRequest++
})

async function revealReward() {
  if (claiming.value || amount.value !== null) return
  claiming.value = true
  claimFailed.value = false
  try {
    const result = props.variant === 'surprise'
      ? await authStore.claimSurpriseReward()
      : await authStore.claimWelcomeReward()
    amount.value = result.amount
    fireCelebration()
  } catch {
    amount.value = null
    claimFailed.value = true
  } finally {
    claiming.value = false
  }
}

function finish() {
  if (amount.value === null) return
  emit('finish')
}
</script>
