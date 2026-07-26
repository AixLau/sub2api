<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import AuroraBackground from '@/components/inspira/AuroraBackground.vue'
import BorderBeam from '@/components/inspira/BorderBeam.vue'
import CardSpotlight from '@/components/inspira/CardSpotlight.vue'
import Marquee from '@/components/inspira/Marquee.vue'
import NumberTicker from '@/components/inspira/NumberTicker.vue'
import ShimmerButton from '@/components/inspira/ShimmerButton.vue'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const isDark = ref(document.documentElement.classList.contains('dark'))
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const currentYear = computed(() => new Date().getFullYear())

const stats = [
  { value: 36, suffix: 'B', decimalPlaces: 0, label: 'Tokens 处理 / 日' },
  { value: 45, suffix: '', decimalPlaces: 0, label: 'P50 延迟 (ms)' },
  { value: 99.9, suffix: '%', decimalPlaces: 1, label: '可用性 (%)' },
]

const models = [
  { name: 'Claude', gradient: 'from-[#d97757] to-[#cc785c]' },
  { name: 'GPT', gradient: 'from-[#10a37f] to-[#1a7f64]' },
  { name: 'Gemini', gradient: 'from-[#4285f4] to-[#1a73e8]' },
  { name: 'DeepSeek', gradient: 'from-[#4d6bfe] to-[#2f4ad0]' },
  { name: 'Qwen', gradient: 'from-[#7c3aed] to-[#5b21b6]' },
  { name: 'Grok', gradient: 'from-[#374151] to-[#111827]' },
  { name: 'Kimi', gradient: 'from-[#0ea5e9] to-[#0369a1]' },
]

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<template>
  <div v-if="homeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <div
    v-else
    class="min-h-screen bg-slate-50 text-slate-900 antialiased dark:bg-slate-900 dark:text-white"
  >
    <nav
      class="fixed inset-x-0 top-0 z-50 border-b border-slate-200 bg-white/80 backdrop-blur-xl dark:border-slate-800 dark:bg-slate-900/80"
    >
      <div class="mx-auto flex h-16 max-w-7xl items-center justify-between px-6">
        <router-link to="/" class="flex items-center gap-3 no-underline">
          <div
            class="flex h-9 w-9 items-center justify-center rounded-lg bg-gradient-to-br from-blue-600 to-cyan-600"
          >
            <img :src="siteLogo || '/logo.svg'" :alt="siteName" class="h-5 w-5" />
          </div>
          <span class="text-lg font-bold text-slate-900 dark:text-white">{{ siteName }}</span>
        </router-link>

        <div class="hidden items-center gap-8 md:flex">
          <a
            href="#features"
            class="text-sm font-medium text-slate-600 no-underline transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
            >平台能力</a
          >
          <a
            href="#models"
            class="text-sm font-medium text-slate-600 no-underline transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
            >支持模型</a
          >
          <a
            href="#pricing"
            class="text-sm font-medium text-slate-600 no-underline transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
            >定价</a
          >
        </div>

        <div class="flex items-center gap-3">
          <LocaleSwitcher />

          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="rounded-lg p-2 text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-700 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>

          <button
            class="rounded-lg p-2 text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-700 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>

          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="rounded-lg bg-gradient-to-r from-blue-600 to-cyan-600 px-5 py-2 text-sm font-semibold text-white no-underline shadow-lg shadow-blue-500/25 transition-colors hover:from-blue-700 hover:to-cyan-700"
          >
            {{ isAuthenticated ? '控制台' : '登录' }}
          </router-link>
        </div>
      </div>
    </nav>

    <main class="pt-16">
      <section class="relative overflow-hidden bg-white px-6 py-16 dark:bg-slate-900 md:py-20">
        <AuroraBackground />
        <div class="relative mx-auto max-w-5xl text-center">
          <span
            class="inline-block rounded-full bg-gradient-to-r from-blue-100 to-cyan-100 px-4 py-1.5 text-sm font-medium text-blue-700 ring-1 ring-blue-200 dark:from-blue-900/30 dark:to-cyan-900/30 dark:text-blue-300 dark:ring-blue-800/50"
          >
            ⚡ AI API 统一网关
          </span>

          <h1
            class="mx-auto mt-6 max-w-4xl text-5xl font-black leading-[1.1] tracking-tight text-slate-900 dark:text-white md:text-7xl lg:text-8xl"
          >
            统一接入<br />
            <span class="bg-gradient-to-r from-blue-600 to-cyan-600 bg-clip-text text-transparent"
              >所有 AI 模型</span
            >
          </h1>

          <p
            class="mx-auto mt-8 max-w-2xl text-lg leading-relaxed text-slate-600 dark:text-slate-400 md:text-xl"
          >
            一个 API 密钥，调用 Claude、GPT、Gemini。<br class="hidden sm:block" />
            告别多平台订阅，<span class="font-semibold text-slate-900 dark:text-white"
              >专注构建你的应用</span
            >。
          </p>

          <div class="mt-8 flex flex-col items-center justify-center gap-4 sm:flex-row">
            <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="no-underline">
              <ShimmerButton
                as="span"
                class="h-12 rounded-xl px-8 text-base font-semibold text-white shadow-xl shadow-blue-500/30 transition-transform hover:scale-[1.02]"
              >
                {{ isAuthenticated ? '进入控制台' : '免费开始' }} →
              </ShimmerButton>
            </router-link>
            <a
              v-if="docUrl"
              :href="docUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="h-12 rounded-xl border border-slate-300 bg-white px-8 text-base font-semibold leading-[48px] text-slate-900 no-underline transition-colors hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-white dark:hover:bg-slate-700"
            >
              查看文档
            </a>
          </div>

          <div class="mx-auto mt-16 grid max-w-3xl grid-cols-1 gap-6 sm:grid-cols-3">
            <div
              v-for="(stat, index) in stats"
              :key="index"
              class="rounded-2xl bg-white p-8 shadow-lg ring-1 ring-slate-200 dark:bg-slate-800 dark:ring-slate-700"
            >
              <div
                class="bg-gradient-to-r from-blue-600 to-cyan-600 bg-clip-text font-mono text-4xl font-bold text-transparent"
              >
                <NumberTicker
                  :value="stat.value"
                  :suffix="stat.suffix"
                  :decimal-places="stat.decimalPlaces"
                  :duration="2000"
                />
              </div>
              <div class="mt-2 text-sm text-slate-600 dark:text-slate-400">{{ stat.label }}</div>
            </div>
          </div>
        </div>
      </section>

      <section id="features" class="px-6 py-24">
        <div class="mx-auto max-w-7xl">
          <div class="mx-auto max-w-2xl text-center">
            <h2 class="text-4xl font-black md:text-5xl">
              为<span class="bg-gradient-to-r from-cyan-400 to-blue-500 bg-clip-text text-transparent"
                >开发者</span
              >而生
            </h2>
            <p class="mt-4 text-lg text-slate-600 dark:text-slate-400">
              {{ siteSubtitle }}
            </p>
          </div>

          <div class="mt-16 grid grid-cols-1 gap-6 md:grid-cols-3">
            <div
              class="relative rounded-2xl bg-white p-8 ring-1 ring-slate-200 backdrop-blur-sm dark:bg-slate-800 dark:ring-slate-700 md:col-span-2"
            >
              <BorderBeam :size="180" :duration="10" />
              <div class="flex items-center gap-3">
                <div
                  class="flex h-11 w-11 items-center justify-center rounded-xl bg-gradient-to-br from-cyan-500 to-blue-600"
                >
                  <Icon name="key" size="md" class="text-white" />
                </div>
                <h3 class="text-2xl font-bold text-slate-900 dark:text-white">统一 API 密钥</h3>
              </div>
              <p class="mt-4 text-slate-600 dark:text-slate-300">
                一个密钥管理所有模型。无需维护多个平台账号，告别密钥混乱。
              </p>

              <div class="relative mt-6 overflow-hidden rounded-xl bg-slate-900 p-4 ring-1 ring-slate-700">
                <div class="absolute inset-x-0 top-0 flex h-8 items-center gap-2 bg-slate-800 px-3">
                  <div class="h-3 w-3 rounded-full bg-red-500"></div>
                  <div class="h-3 w-3 rounded-full bg-yellow-500"></div>
                  <div class="h-3 w-3 rounded-full bg-green-500"></div>
                  <span class="ml-2 text-xs text-slate-400">terminal</span>
                </div>
                <pre class="overflow-x-auto pb-2 pt-8 text-sm leading-relaxed text-slate-300"><code>$ curl https://api.example.com/v1/chat \
  -H "Authorization: Bearer sk-xxx..." \
  -d '{"model":"claude-opus-4","messages":[{"role":"user","content":"Hello"}]}'

# Unified endpoint ready
200 OK</code></pre>
              </div>
            </div>

            <CardSpotlight class="rounded-2xl bg-white p-8 ring-1 ring-slate-200 dark:bg-slate-800 dark:ring-slate-700">
              <div
                class="flex h-11 w-11 items-center justify-center rounded-xl bg-gradient-to-br from-blue-600 to-cyan-600"
              >
                <Icon name="bolt" size="md" class="text-white" />
              </div>
              <h3 class="mt-4 text-2xl font-bold text-slate-900 dark:text-white">极速响应</h3>
              <p class="mt-3 text-slate-600 dark:text-slate-300">
                P50 延迟低于 50ms，边缘优化加速全球访问。
              </p>
            </CardSpotlight>

            <CardSpotlight
              class="rounded-2xl bg-white p-8 ring-1 ring-slate-200 backdrop-blur-sm dark:bg-slate-800 dark:ring-slate-700"
            >
              <div class="flex h-11 w-11 items-center justify-center rounded-xl bg-emerald-500/20">
                <Icon name="checkCircle" size="md" class="text-emerald-400" />
              </div>
              <h3 class="mt-4 text-2xl font-bold text-slate-900 dark:text-white">智能容错</h3>
              <p class="mt-3 text-slate-600 dark:text-slate-300">
                多上游自动切换、负载均衡，99.9% 可用性保障。
              </p>
            </CardSpotlight>

            <CardSpotlight
              class="rounded-2xl bg-white p-8 ring-1 ring-slate-200 backdrop-blur-sm dark:bg-slate-800 dark:ring-slate-700 md:col-span-2"
            >
              <div class="flex items-center gap-3">
                <div
                  class="flex h-11 w-11 items-center justify-center rounded-xl bg-gradient-to-br from-violet-500 to-purple-600"
                >
                  <Icon name="chart" size="md" class="text-white" />
                </div>
                <h3 class="text-2xl font-bold text-slate-900 dark:text-white">实时监控</h3>
              </div>
              <p class="mt-4 text-slate-600 dark:text-slate-300">
                详细的使用统计、成本分析、错误追踪，掌握每一笔调用。
              </p>
              <div class="mt-6 flex h-28 items-end gap-2">
                <div
                  v-for="(height, index) in [55, 80, 45, 90, 70, 60, 85]"
                  :key="index"
                  class="flex-1 rounded-t bg-gradient-to-t from-blue-600 to-cyan-400"
                  :style="{ height: `${height}%` }"
                ></div>
              </div>
            </CardSpotlight>
          </div>
        </div>
      </section>

      <section id="models" class="px-6 py-24">
        <div
          class="mx-auto max-w-5xl rounded-2xl bg-white p-12 text-center ring-1 ring-slate-200 backdrop-blur-sm dark:bg-slate-800 dark:ring-slate-700"
        >
          <h2 class="text-3xl font-bold text-slate-900 dark:text-white md:text-4xl">
            已接入主流模型
          </h2>
          <div class="mt-10">
            <Marquee :duration="22" class="py-2">
              <span
                v-for="model in models"
                :key="model.name"
                :class="`shrink-0 whitespace-nowrap rounded-full bg-gradient-to-r ${model.gradient} px-8 py-3 text-lg font-bold text-white shadow-lg`"
              >
                {{ model.name }}
              </span>
              <span
                class="shrink-0 whitespace-nowrap rounded-full bg-gradient-to-r from-blue-600 to-cyan-600 px-8 py-3 text-lg font-bold text-white shadow-lg"
              >
                更多接入中…
              </span>
            </Marquee>
          </div>
        </div>
      </section>

      <section id="pricing" class="px-6 py-28">
        <div
          class="mx-auto max-w-4xl rounded-2xl bg-gradient-to-br from-blue-600 via-cyan-600 to-blue-700 p-16 text-center shadow-2xl shadow-blue-600/20"
        >
          <h2 class="text-4xl font-black text-white md:text-6xl">准备好开始了吗？</h2>
          <p class="mx-auto mt-6 max-w-xl text-lg text-blue-50">
            注册即可获得免费额度，立即体验统一 AI API 服务。
          </p>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="mt-10 inline-block h-14 rounded-xl bg-white px-10 text-lg font-bold leading-[56px] text-blue-600 no-underline shadow-lg transition-colors hover:bg-blue-50"
          >
            免费开始 →
          </router-link>
        </div>
      </section>

      <footer class="border-t border-slate-200 px-6 py-12 dark:border-slate-800">
        <div
          class="mx-auto flex max-w-7xl flex-col items-center justify-between gap-4 text-sm text-slate-600 dark:text-slate-400 md:flex-row"
        >
          <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
          <a
            :href="githubUrl"
            target="_blank"
            rel="noopener"
            class="text-slate-600 no-underline transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
            >GitHub</a
          >
        </div>
      </footer>
    </main>
  </div>
</template>
