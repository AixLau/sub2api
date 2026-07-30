<template>
  <main
    class="liquid-glass-preview relative flex min-h-[100svh] items-center justify-center overflow-hidden p-4"
    data-testid="liquid-glass-preview"
  >
    <LiquidGlassBackdrop />

    <LiquidGlass
      data-testid="preview-outer-liquid-glass"
      v-bind="supportLiquidGlassPreset"
    >
      <SupportContactCardContent
        v-model="activeTab"
        :wechat-qr-code="wechatPreviewQRCode"
        test-id-prefix="preview"
      />
    </LiquidGlass>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import wechatPreviewQRCode from '@/assets/support-wechat-preview.webp'
import LiquidGlass from '@/components/inspira/LiquidGlass.vue'
import LiquidGlassBackdrop from '@/components/inspira/LiquidGlassBackdrop.vue'
import SupportContactCardContent, { type SupportPlatform } from '@/components/inspira/SupportContactCardContent.vue'
import { supportLiquidGlassPreset } from '@/components/inspira/liquidGlassPresets'

const activeTab = ref<SupportPlatform>('qq')
</script>
