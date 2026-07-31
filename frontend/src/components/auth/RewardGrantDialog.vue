<template>
  <BaseDialog
    :show="show"
    :title="grant.title || t('rewardQueue.titleFallback')"
    width="normal"
    :close-on-escape="!claiming"
    :close-on-click-outside="!claiming"
    :show-close-button="!claiming"
    :auto-focus="false"
    :z-index="100000000"
    @close="handleClose"
  >
    <div class="pb-1 text-center">
      <div
        class="mx-auto flex h-12 w-12 items-center justify-center rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-950/50 dark:text-blue-300"
      >
        <Icon name="gift" size="lg" />
      </div>
      <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-dark-300">
        {{ amount !== null ? t('rewardQueue.revealedHint') : (grant.hint || t('rewardQueue.hintFallback')) }}
      </p>
    </div>

    <ScratchToReveal
      class="mt-5 min-h-[11rem] overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900"
      :cover-color="grant.skin?.cover_color || neutralColors['300']"
      :cover-image="coverImage"
      :cover-text-color="grant.skin?.cover_text_color"
      :cover-text="grant.cover_text || t('rewardQueue.coverTextFallback')"
      :threshold="0.42"
      :radius="28"
      @complete="revealReward"
    >
      <div class="flex min-h-[11rem] flex-col items-center justify-center px-6 py-5">
        <span v-if="grant.skin?.alt" class="sr-only">{{ grant.skin.alt }}</span>
        <LoadingSpinner v-if="claiming" size="lg" variant="orbit" />
        <template v-else-if="amount !== null">
          <div
            class="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-3 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300"
          >
            <Icon name="sparkles" size="xs" />
            {{ t('rewardQueue.won') }}
          </div>
          <p class="mt-3 text-5xl font-bold text-gray-950 dark:text-white">
            <span class="mr-1 text-2xl font-semibold text-blue-600 dark:text-blue-400">$</span
            >{{ amount.toFixed(2) }}
          </p>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
            {{ grant.success_message || t('rewardQueue.credited') }}
          </p>
        </template>
        <template v-else-if="claimFailed">
          <p class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('rewardQueue.claimFailed') }}
          </p>
          <button
            type="button"
            data-testid="reward-claim-retry"
            class="btn btn-secondary mt-4 inline-flex items-center justify-center gap-2"
            @click="revealReward"
          >
            <Icon name="refresh" size="sm" />
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
      {{ t('rewardQueue.gestureHint') }}
    </p>
    <p
      v-if="formattedExpiry && amount === null"
      class="mt-1 text-center text-xs text-gray-400 dark:text-dark-500"
    >
      {{ t('rewardQueue.expiresAt', { time: formattedExpiry }) }}
    </p>

    <template #footer>
      <button
        v-if="amount === null"
        type="button"
        data-testid="reward-defer"
        class="btn btn-secondary inline-flex w-full items-center justify-center gap-2"
        :disabled="claiming"
        @click="defer"
      >
        <Icon name="clock" size="sm" />
        {{ t('rewardQueue.later') }}
      </button>
      <button
        v-else
        type="button"
        data-testid="reward-finish"
        class="btn btn-primary inline-flex w-full items-center justify-center gap-2"
        @click="finish"
      >
        <Icon name="check" size="sm" />
        {{ grant.claim_cta || t('rewardQueue.continue') }}
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
import { neutralColors } from '@/theme/designTokens'
import { userAPI } from '@/api'
import type { RewardClaimResponse, RewardGrant } from '@/types'

const props = defineProps<{
  show: boolean
  grant: RewardGrant
}>()

const emit = defineEmits<{
  (event: 'defer'): void
  (event: 'claimed', result: RewardClaimResponse): void
  (event: 'finish'): void
}>()

const { locale, t } = useI18n()
const amount = ref<number | null>(null)
const claiming = ref(false)
const claimFailed = ref(false)
let claimGeneration = 0

const coverImage = computed(() => {
  const source = props.grant.skin?.image_url?.trim()
  if (!source || typeof window === 'undefined') return undefined
  try {
    const resolved = new URL(source, window.location.origin)
    return resolved.origin === window.location.origin ? resolved.href : undefined
  } catch {
    return undefined
  }
})

const formattedExpiry = computed(() => {
  if (!props.grant.expires_at) return ''
  const value = new Date(props.grant.expires_at)
  if (!Number.isFinite(value.getTime())) return ''
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(value)
})

watch(
  () => props.grant.grant_id,
  () => {
    claimGeneration++
    amount.value = null
    claiming.value = false
    claimFailed.value = false
  }
)

onBeforeUnmount(() => {
  claimGeneration++
})

async function revealReward() {
  if (claiming.value || amount.value !== null) return
  const request = ++claimGeneration
  const grantID = props.grant.grant_id
  claiming.value = true
  claimFailed.value = false
  try {
    const result = await userAPI.claimReward(grantID)
    if (request !== claimGeneration || props.grant.grant_id !== grantID) return
    amount.value = Number(result.amount)
    emit('claimed', result)
    fireCelebration()
  } catch {
    if (request !== claimGeneration) return
    amount.value = null
    claimFailed.value = true
  } finally {
    if (request === claimGeneration) claiming.value = false
  }
}

function defer() {
  if (claiming.value || amount.value !== null) return
  emit('defer')
}

function finish() {
  if (amount.value === null) return
  emit('finish')
}

function handleClose() {
  if (claiming.value) return
  if (amount.value === null) {
    defer()
  } else {
    finish()
  }
}
</script>
