<template>
  <BaseDialog
    :show="show"
    :title="t('welcomeReward.title')"
    width="normal"
    :close-on-escape="revealed"
    :close-on-click-outside="revealed"
    :show-close-button="revealed"
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
        {{ t(revealed ? 'welcomeReward.revealedHint' : 'welcomeReward.scratchHint') }}
      </p>
    </div>

    <ScratchToReveal
      class="mt-5 min-h-[11rem] overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900"
      :cover-color="skin.coverColor"
      :cover-image="skin.coverImage"
      :cover-text-color="skin.coverTextColor"
      :cover-text="t('welcomeReward.coverText')"
      :threshold="0.42"
      :radius="28"
      @complete="revealReward"
    >
      <div class="flex min-h-[11rem] flex-col items-center justify-center px-6 py-5">
        <div
          class="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300"
        >
          <Icon name="sparkles" size="xs" />
          {{ t('welcomeReward.won') }}
        </div>
        <p class="mt-3 text-5xl font-bold text-gray-950 dark:text-white">
          <span class="mr-1 text-2xl font-semibold text-blue-600 dark:text-blue-400">¥</span
          >{{ amount.toFixed(2) }}
        </p>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{ t('welcomeReward.credited') }}
        </p>
      </div>
    </ScratchToReveal>

    <p v-if="!revealed" class="mt-3 text-center text-xs text-gray-500 dark:text-dark-400">
      {{ t('welcomeReward.gestureHint') }}
    </p>

    <template v-if="revealed" #footer>
      <button type="button" class="btn btn-primary w-full" @click="finish">
        {{ t('welcomeReward.continue') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ScratchToReveal from '@/components/inspira/ScratchToReveal.vue'
import { fireCelebration } from '@/components/inspira/confetti'
import {
  welcomeRewardSkins,
  type WelcomeRewardSkinId
} from '@/components/auth/welcomeRewardSkins'

const props = defineProps<{
  show: boolean
  amount: number
  skinId: WelcomeRewardSkinId
}>()

const emit = defineEmits<{
  (event: 'finish'): void
}>()

const { t } = useI18n()
const revealed = ref(false)
const skin = computed(
  () => welcomeRewardSkins.find((candidate) => candidate.id === props.skinId) ?? welcomeRewardSkins[0]
)

function revealReward() {
  if (revealed.value) return
  revealed.value = true
  fireCelebration()
}

function finish() {
  if (!revealed.value) return
  emit('finish')
}
</script>
