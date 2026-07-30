<template>
  <main
    class="liquid-glass-readability-preview relative flex min-h-[100svh] items-center justify-center overflow-hidden p-4"
    data-testid="liquid-glass-readability-preview"
  >
    <LiquidGlassBackdrop />

    <LiquidGlass
      data-testid="readability-preview-outer-liquid-glass"
      :radius="28"
      :border="0.07"
      :lightness="50"
      blend="difference"
      :alpha="0.93"
      :blur="14"
      :scale="-150"
      :frost="0.16"
      container-class="relative z-10 w-[360px] max-w-[calc(100vw-32px)] overflow-hidden"
    >
      <section
        class="relative isolate w-full overflow-hidden rounded-[inherit] text-gray-950"
        :aria-label="t('common.supportCommunityTitle')"
      >
        <span class="preview-reading-frost" aria-hidden="true"></span>

        <header class="relative z-10 flex items-start justify-between gap-3 px-6 pb-4 pt-6">
          <div class="min-w-0">
            <h1 class="text-lg font-semibold tracking-tight text-gray-950">
              {{ t('common.supportCommunityTitle') }}
            </h1>
            <p
              data-testid="readability-preview-subtitle"
              class="mt-1.5 text-[13px] leading-5 text-gray-800"
            >
              {{ t('common.supportCommunityIntro') }}
            </p>
          </div>
          <span
            class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full border border-white/80 bg-white/[0.76] text-gray-600 shadow-sm"
            aria-hidden="true"
          >
            <Icon name="chat" size="sm" :stroke-width="1.8" />
          </span>
        </header>

        <div class="relative z-10 px-5">
          <div
            role="tablist"
            :aria-label="t('common.supportCommunityTitle')"
            class="grid grid-cols-2 rounded-full border border-white/75 bg-white/[0.55] p-1 shadow-[inset_0_1px_0_rgba(255,255,255,0.85)]"
          >
            <button
              type="button"
              role="tab"
              data-testid="preview-tab-qq"
              :aria-selected="activeTab === 'qq'"
              class="h-8 rounded-full text-sm font-semibold transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
              :class="activeTab === 'qq' ? 'bg-white text-gray-950 shadow-sm' : 'text-gray-700 hover:text-gray-950'"
              @click="activeTab = 'qq'"
            >
              {{ t('common.supportQQTab') }}
            </button>
            <button
              type="button"
              role="tab"
              data-testid="preview-tab-wechat"
              :aria-selected="activeTab === 'wechat'"
              class="h-8 rounded-full text-sm font-semibold transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
              :class="activeTab === 'wechat' ? 'bg-white text-gray-950 shadow-sm' : 'text-gray-700 hover:text-gray-950'"
              @click="activeTab = 'wechat'"
            >
              {{ t('common.supportWeChatTab') }}
            </button>
          </div>
        </div>

        <div class="relative z-10 px-5 pb-6 pt-5 text-center">
          <div
            data-testid="preview-contact-content"
            class="flex h-[256px] w-full items-center justify-center overflow-hidden px-4"
          >
            <img
              v-if="activeTab === 'wechat'"
              data-testid="preview-wechat-qr"
              :src="wechatPreviewQRCode"
              :alt="t('common.supportQRCodeAlt', { platform: activePlatform })"
              class="h-[216px] w-[216px] rounded-[20px] bg-white object-contain shadow-[0_14px_36px_rgba(15,23,42,0.14)]"
            >
            <div v-else data-testid="preview-qq-empty" class="px-6 py-7">
              <span
                class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-white/70 text-gray-500 shadow-sm"
              >
                <Icon name="chat" size="lg" :stroke-width="1.75" />
              </span>
              <p class="mt-3 text-sm font-semibold text-gray-900">
                {{ t('common.supportQRCodeEmptyTitle', { platform: activePlatform }) }}
              </p>
              <p class="mt-1 text-[13px] leading-5 text-gray-700">
                {{ t('common.supportQRCodeEmptyDescription') }}
              </p>
            </div>
          </div>
        </div>
      </section>
    </LiquidGlass>
  </main>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import wechatPreviewQRCode from '@/assets/support-wechat-preview.webp'
import Icon from '@/components/icons/Icon.vue'
import LiquidGlass from '@/components/inspira/LiquidGlass.vue'
import LiquidGlassBackdrop from '@/components/inspira/LiquidGlassBackdrop.vue'

const { t } = useI18n()
const activeTab = ref<'qq' | 'wechat'>('qq')
const activePlatform = computed(() =>
  activeTab.value === 'qq' ? t('common.supportQQTab') : t('common.supportWeChatTab')
)
</script>

<style scoped>
.preview-reading-frost {
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  background:
    radial-gradient(
      ellipse 88% 72% at 50% 58%,
      rgb(255 255 255 / 0.54) 0%,
      rgb(255 255 255 / 0.42) 48%,
      rgb(255 255 255 / 0.16) 76%,
      transparent 100%
    ),
    linear-gradient(180deg, rgb(255 255 255 / 0.22), rgb(255 255 255 / 0.08));
}
</style>
