<template>
  <main class="min-h-screen bg-slate-950 text-white">
    <div class="mx-auto flex min-h-screen w-full max-w-3xl items-center px-6 py-12">
      <section class="w-full rounded-2xl border border-white/10 bg-white/[0.06] p-8 shadow-2xl shadow-black/30 backdrop-blur">
        <div class="mb-8">
          <p class="mb-3 text-sm font-medium text-cyan-300">星链 AI Hub 客户端配置</p>
          <h1 class="text-3xl font-semibold tracking-tight">正在完成本机 {{ clientLabel }} 配置</h1>
          <p class="mt-4 text-sm leading-6 text-slate-300">
            请保持当前账号已登录，配置将自动继续。
          </p>
        </div>

        <div class="mb-8 grid gap-4 rounded-xl border border-white/10 bg-slate-900/70 p-5 text-sm">
          <div class="flex items-center justify-between gap-4">
            <span class="text-slate-400">客户端</span>
            <span class="font-medium">{{ clientLabel }}</span>
          </div>
          <div class="flex items-center justify-between gap-4">
            <span class="text-slate-400">验证码</span>
            <code class="rounded bg-cyan-400/10 px-2 py-1 font-mono text-cyan-200">{{ deviceCode || '-' }}</code>
          </div>
          <div class="flex items-center justify-between gap-4">
            <span class="text-slate-400">状态</span>
            <span :class="statusClass">{{ statusText }}</span>
          </div>
        </div>

        <div v-if="errorMessage" class="mb-6 rounded-xl border border-rose-400/30 bg-rose-500/10 p-4 text-sm text-rose-100">
          {{ errorMessage }}
        </div>

        <div v-if="successMessage" class="mb-6 rounded-xl border border-emerald-400/30 bg-emerald-500/10 p-4 text-sm text-emerald-100">
          {{ successMessage }}
        </div>

        <div class="rounded-xl border border-cyan-300/20 bg-cyan-300/10 px-5 py-4 text-sm text-cyan-100">
          {{ actionText }}
        </div>
      </section>
    </div>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { clientSetupAPI } from '@/api'

const CLIENT_SETUP_REDIRECT_URL = 'https://aixlau.me/dashboard'
const CLIENT_SETUP_REDIRECT_DELAY_MS = 10_000

const route = useRoute()
const setupId = computed(() => String(route.query.setup_id || '').trim())
const deviceCode = computed(() => String(route.query.device_code || '').trim())
const client = computed(() => String(route.query.client || 'codex').trim())

const loading = ref(false)
const approved = ref(false)
const status = ref('pending')
const errorMessage = ref('')
const successMessage = ref('')
let redirectTimer: ReturnType<typeof setTimeout> | null = null

const clientLabel = computed(() => (client.value === 'claude' ? 'Claude Code' : 'Codex'))
const statusText = computed(() => {
  if (approved.value || status.value === 'approved') return '已确认'
  if (status.value === 'exchanged') return '已完成'
  return '处理中'
})
const statusClass = computed(() => {
  if (approved.value || status.value === 'approved' || status.value === 'exchanged') {
    return 'font-medium text-emerald-300'
  }
  if (loading.value) {
    return 'font-medium text-cyan-300'
  }
  return 'font-medium text-amber-300'
})
const actionText = computed(() => {
  if (loading.value) return '正在处理本次配置，请稍候...'
  if (approved.value) return '配置确认完成，10 秒后将进入仪表盘。'
  if (errorMessage.value) return '处理失败，请按页面提示重试或回到终端继续处理。'
  return '正在检查配置请求，请稍候。'
})

onMounted(async () => {
  if (!setupId.value || !deviceCode.value) {
    errorMessage.value = '缺少配置参数，请回到终端重新运行脚本。'
    return
  }
  try {
    const session = await clientSetupAPI.getSession(setupId.value)
    status.value = session.status
    await approve()
  } catch (error) {
    errorMessage.value = readErrorMessage(error, '配置请求不存在或已过期，请回到终端重新运行脚本。')
  }
})

async function approve() {
  errorMessage.value = ''
  successMessage.value = ''
  loading.value = true
  try {
    const result = await clientSetupAPI.approveSession(setupId.value, {
      device_code: deviceCode.value,
      client: client.value
    })
    approved.value = true
    status.value = result.status
    successMessage.value = '配置确认完成，10 秒后将进入仪表盘。'
    redirectTimer = setTimeout(() => {
      window.location.href = CLIENT_SETUP_REDIRECT_URL
    }, CLIENT_SETUP_REDIRECT_DELAY_MS)
  } catch (error) {
    errorMessage.value = readErrorMessage(error, '处理失败，请稍后重试或回到终端继续处理。')
  } finally {
    loading.value = false
  }
}

onBeforeUnmount(() => {
  if (redirectTimer) {
    clearTimeout(redirectTimer)
    redirectTimer = null
  }
})

function readErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object' && 'message' in error) {
    const message = String((error as { message?: unknown }).message || '').trim()
    if (message) return message
  }
  return fallback
}
</script>
