<template>
  <section
    class="w-full overflow-hidden rounded-[inherit] text-gray-950"
    :role="dialog ? 'dialog' : undefined"
    :aria-modal="dialog ? 'true' : undefined"
    :aria-label="t('common.supportCommunityTitle')"
    :data-testid="`${testIdPrefix}-dialog`"
  >
    <header class="flex items-start justify-between gap-3 px-6 pb-4 pt-6">
      <div class="min-w-0">
        <h1 class="text-lg font-semibold tracking-tight text-gray-950">
          {{ t('common.supportCommunityTitle') }}
        </h1>
        <p class="mt-1.5 text-xs leading-5 text-gray-600">
          {{ t('common.supportCommunityIntro') }}
        </p>
      </div>

      <button
        v-if="dismissible"
        type="button"
        :data-testid="`${testIdPrefix}-close`"
        class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full border border-white/70 bg-white/[0.65] text-gray-500 shadow-sm transition-colors hover:bg-white/80 hover:text-gray-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
        :aria-label="t('common.close')"
        @click="emit('close')"
      >
        <Icon name="x" size="sm" :stroke-width="1.8" />
      </button>
      <span
        v-else
        class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full border border-white/70 bg-white/[0.65] text-gray-500 shadow-sm"
        aria-hidden="true"
      >
        <Icon name="chat" size="sm" :stroke-width="1.8" />
      </span>
    </header>

    <div class="px-5">
      <div
        role="tablist"
        :aria-label="t('common.supportCommunityTitle')"
        class="grid grid-cols-2 rounded-full border border-white/75 bg-white/[0.55] p-1 shadow-[inset_0_1px_0_rgba(255,255,255,0.85)]"
      >
        <button
          type="button"
          role="tab"
          :data-testid="`${testIdPrefix}-tab-qq`"
          :aria-selected="modelValue === 'qq'"
          class="h-8 rounded-full text-sm font-semibold transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="modelValue === 'qq' ? 'bg-white text-gray-950 shadow-sm' : 'text-gray-600 hover:text-gray-950'"
          @click="selectPlatform('qq')"
        >
          {{ t('common.supportQQTab') }}
        </button>
        <button
          type="button"
          role="tab"
          :data-testid="`${testIdPrefix}-tab-wechat`"
          :aria-selected="modelValue === 'wechat'"
          class="h-8 rounded-full text-sm font-semibold transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="modelValue === 'wechat' ? 'bg-white text-gray-950 shadow-sm' : 'text-gray-600 hover:text-gray-950'"
          @click="selectPlatform('wechat')"
        >
          {{ t('common.supportWeChatTab') }}
        </button>
      </div>
    </div>

    <div class="px-5 pb-6 pt-5 text-center">
      <div
        :data-testid="`${testIdPrefix}-contact-content`"
        class="flex h-[256px] w-full items-center justify-center overflow-hidden px-4"
      >
        <img
          v-if="activeQRCode"
          :src="activeQRCode"
          :alt="t('common.supportQRCodeAlt', { platform: activePlatform })"
          :data-testid="`${testIdPrefix}-qr-image`"
          class="h-[216px] w-[216px] rounded-[20px] bg-white object-contain shadow-[0_14px_36px_rgba(15,23,42,0.14)]"
        >
        <div v-else :data-testid="`${testIdPrefix}-qr-empty`" class="px-6 py-7 text-center">
          <span
            class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-white/70 text-gray-500 shadow-sm"
          >
            <Icon name="chat" size="lg" :stroke-width="1.75" />
          </span>
          <p class="mt-3 text-sm font-semibold text-gray-900">
            {{ t('common.supportQRCodeEmptyTitle', { platform: activePlatform }) }}
          </p>
          <p class="mt-1 text-xs leading-5 text-gray-600">
            {{ t('common.supportQRCodeEmptyDescription') }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

export type SupportPlatform = 'qq' | 'wechat'

interface Props {
  modelValue: SupportPlatform
  qqQrCode?: string
  wechatQrCode?: string
  dialog?: boolean
  dismissible?: boolean
  testIdPrefix?: string
}

const props = withDefaults(defineProps<Props>(), {
  qqQrCode: '',
  wechatQrCode: '',
  dialog: false,
  dismissible: false,
  testIdPrefix: 'support'
})

const emit = defineEmits<{
  'update:modelValue': [value: SupportPlatform]
  close: []
}>()

const { t } = useI18n()

const activeQRCode = computed(() =>
  props.modelValue === 'qq' ? props.qqQrCode.trim() : props.wechatQrCode.trim()
)
const activePlatform = computed(() =>
  props.modelValue === 'qq' ? t('common.supportQQTab') : t('common.supportWeChatTab')
)

function selectPlatform(platform: SupportPlatform) {
  emit('update:modelValue', platform)
}
</script>
