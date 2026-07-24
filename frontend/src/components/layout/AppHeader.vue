<template>
  <header class="glass sticky top-0 z-30 border-b border-gray-200/50 dark:border-dark-700/50">
    <div class="flex h-16 items-center justify-between gap-2 px-2 sm:px-4 md:px-6">
      <!-- Left: Mobile Menu Toggle + Page Title -->
      <div class="flex min-w-0 flex-1 items-center gap-2 sm:gap-4">
        <button
          @click="toggleMobileSidebar"
          class="btn-ghost btn-icon lg:hidden"
          :aria-label="t('common.toggleMenu')"
        >
          <Icon name="menu" size="md" />
        </button>

        <div class="hidden min-w-0 lg:block">
          <h1 class="truncate text-lg font-semibold text-gray-900 dark:text-white">
            {{ pageTitle }}
          </h1>
          <p v-if="pageDescription" class="truncate text-xs text-gray-500 dark:text-dark-400">
            {{ pageDescription }}
          </p>
        </div>
      </div>

      <!-- Right: Support + Announcements + Docs + Language + Subscriptions + Balance + User Dropdown -->
      <div class="ml-4 flex flex-shrink-0 items-center gap-2 xl:gap-3">
        <!-- Prominent support entry for new users -->
        <button
          v-if="hasSupportGroup"
          type="button"
          data-testid="header-contact-support"
          class="hidden h-9 min-w-9 items-center justify-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white dark:focus:ring-offset-dark-900 md:flex"
          :aria-label="t('common.contactSupport')"
          @click="openSupportDialog"
        >
          <span class="relative flex flex-shrink-0" aria-hidden="true">
            <Icon name="chat" size="sm" :stroke-width="2" />
            <span class="absolute -right-1 -top-1 h-1.5 w-1.5 rounded-full bg-amber-500 ring-2 ring-white dark:ring-dark-900"></span>
          </span>
          <span class="whitespace-nowrap">
            {{ t('common.contactSupport') }}
          </span>
        </button>

        <!-- Announcement Bell -->
        <AnnouncementBell v-if="user" />

        <!-- Docs Link -->
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
	          class="hidden items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white sm:flex"
        >
          <Icon name="book" size="sm" />
          <span class="hidden sm:inline">{{ t('nav.docs') }}</span>
        </a>

        <!-- Language Switcher -->
        <LocaleSwitcher />

        <!-- Subscription Progress (for users with active subscriptions) -->
        <SubscriptionProgressMini v-if="user" />

        <!-- Wallet -->
        <div
          v-if="user"
          ref="walletRef"
          data-testid="wallet-control"
          class="relative hidden flex-shrink-0 md:block"
          @mouseenter="openWallet"
          @mouseleave="handleWalletMouseLeave"
        >
          <div class="flex h-10 items-center gap-0.5 rounded-full border border-gray-200/80 bg-white p-0.5 shadow-sm dark:border-dark-700 dark:bg-dark-800 xl:h-12 xl:gap-1 xl:rounded-[22px] xl:p-1">
            <button
              type="button"
              data-testid="wallet-trigger"
              class="flex h-9 items-center gap-1.5 rounded-full px-2 text-left transition-colors hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-primary-500 dark:hover:bg-dark-700 xl:h-10 xl:gap-2 xl:px-2.5"
              :aria-expanded="walletOpen"
              aria-haspopup="dialog"
              @click="pinWallet"
            >
              <span class="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-primary-600 text-white xl:h-7 xl:w-7" aria-hidden="true">
                <Icon name="creditCard" size="sm" :stroke-width="2" />
              </span>
              <span class="min-w-0 whitespace-nowrap leading-none">
                <span class="hidden text-[10px] font-medium text-gray-500 dark:text-dark-300 xl:block">{{ walletText }}</span>
                <span class="text-sm font-semibold text-gray-950 dark:text-white xl:mt-1 xl:block">{{ formatHeaderMoney(availableBalance) }}</span>
              </span>
              <Icon
                name="chevronDown"
                size="xs"
                class="ml-0.5 hidden text-gray-400 transition-transform xl:block"
                :class="{ 'rotate-180': walletOpen }"
              />
            </button>

            <button
              type="button"
              data-testid="wallet-recharge-top"
              class="flex h-9 items-center gap-1 rounded-full bg-[#17191c] px-2.5 text-sm font-semibold text-white shadow-sm transition-colors hover:bg-black focus:outline-none focus:ring-2 focus:ring-gray-900 focus:ring-offset-2 dark:bg-white dark:text-gray-950 dark:hover:bg-gray-100 dark:focus:ring-white dark:focus:ring-offset-dark-900 xl:h-10 xl:gap-1.5 xl:px-3.5"
              @click="goToRecharge"
            >
              <Icon name="plus" size="sm" :stroke-width="2.25" />
              {{ rechargeText }}
            </button>
          </div>

          <transition name="dropdown">
            <div
              v-if="walletOpen"
              data-testid="wallet-panel"
              role="dialog"
              :aria-label="balanceAvailableText"
              class="absolute right-0 top-full z-50 mt-2 w-[268px] overflow-hidden rounded-[20px] border border-black/5 bg-white shadow-[0_18px_44px_rgba(15,23,42,0.18)] dark:border-white/10 dark:bg-dark-900"
            >
              <div class="relative h-[82px] overflow-hidden bg-[#3154d8]">
                <img
                  :src="walletArtwork"
                  alt=""
                  data-testid="wallet-artwork"
                  class="h-full w-full object-cover"
                >
                <button
                  type="button"
                  class="absolute right-2.5 top-2.5 flex h-7 w-7 items-center justify-center rounded-full border border-white/20 bg-black/15 text-white backdrop-blur-md transition-colors hover:bg-black/25 focus:outline-none focus:ring-2 focus:ring-white"
                  :aria-label="t('common.close')"
                  @click="closeWallet"
                >
                  <Icon name="x" size="sm" :stroke-width="2" />
                </button>
              </div>

              <div class="px-5 pb-5 pt-4 text-center">
                <p class="text-xs font-medium text-gray-500 dark:text-dark-300">{{ balanceAvailableText }}</p>
                <p class="mt-1 text-2xl font-bold leading-none text-gray-950 dark:text-white">
                  {{ formatHeaderMoney(availableBalance) }}
                </p>
                <button
                  type="button"
                  data-testid="wallet-recharge"
                  class="mx-auto mt-4 flex h-9 items-center justify-center gap-1.5 rounded-full bg-[#17191c] px-4 text-sm font-semibold text-white transition-colors hover:bg-black focus:outline-none focus:ring-2 focus:ring-gray-900 focus:ring-offset-2 dark:bg-white dark:text-gray-950 dark:hover:bg-gray-100 dark:focus:ring-white dark:focus:ring-offset-dark-900"
                  @click="goToRecharge"
                >
                  <Icon name="plus" size="sm" :stroke-width="2.25" />
                  {{ rechargeNowText }}
                </button>
              </div>
            </div>
          </transition>
        </div>

        <!-- User Dropdown -->
        <div v-if="user" class="relative flex-shrink-0" ref="dropdownRef">
          <button
            @click="toggleDropdown"
            class="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
            :aria-label="t('common.userMenu')"
          >
            <div class="flex h-8 w-8 items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 text-sm font-medium text-white shadow-sm">
              <img
                v-if="avatarUrl"
                :src="avatarUrl"
                :alt="displayName"
                class="h-full w-full object-cover"
              >
              <span v-else>{{ userInitials }}</span>
            </div>
            <div class="hidden text-left xl:block">
              <div class="text-sm font-medium text-gray-900 dark:text-white">
                {{ displayName }}
              </div>
              <div class="text-xs capitalize text-gray-500 dark:text-dark-400">
                {{ user.role }}
              </div>
            </div>
            <Icon name="chevronDown" size="sm" class="hidden text-gray-400 xl:block" />
          </button>

          <!-- Dropdown Menu -->
          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown right-0 mt-2 w-56">
              <!-- User Info -->
              <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="text-sm font-medium text-gray-900 dark:text-white">
                  {{ displayName }}
                </div>
                <div class="text-xs text-gray-500 dark:text-dark-400">{{ user.email }}</div>
              </div>

              <!-- Balance (mobile only) -->
              <div class="border-b border-gray-100 px-4 py-2 dark:border-dark-700 sm:hidden">
                <div class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t('common.balance') }}
                </div>
                <div class="text-sm font-semibold text-primary-600 dark:text-primary-400">
                  {{ formatHeaderMoney(availableBalance) }}
                </div>
                <div v-if="frozenBalance > 0" class="mt-1 text-xs text-amber-600 dark:text-amber-300">
                  {{ balanceFrozenText }} {{ formatHeaderMoney(frozenBalance) }}
                </div>
                <button
                  type="button"
                  class="mt-2 flex w-full items-center justify-center gap-1.5 rounded-lg bg-primary-600 px-3 py-2 text-sm font-semibold text-white hover:bg-primary-700"
                  @click="goToRecharge"
                >
                  <Icon name="plus" size="sm" :stroke-width="2" />
                  {{ rechargeNowText }}
                </button>
              </div>

              <div class="py-1">
                <router-link to="/profile" @click="closeDropdown" class="dropdown-item">
                  <Icon name="user" size="sm" />
                  {{ t('nav.profile') }}
                </router-link>

                <router-link to="/keys" @click="closeDropdown" class="dropdown-item">
                  <Icon name="key" size="sm" />
                  {{ t('nav.apiKeys') }}
                </router-link>

                <a
                  v-if="authStore.isAdmin"
                  href="https://github.com/Wei-Shaw/sub2api"
                  target="_blank"
                  rel="noopener noreferrer"
                  @click="closeDropdown"
                  class="dropdown-item"
                >
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      fill-rule="evenodd"
                      clip-rule="evenodd"
                      d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                    />
                  </svg>
                  {{ t('nav.github') }}
                </a>

              </div>

              <!-- Contact Support (only show if configured) -->
              <button
                v-if="hasSupportGroup"
                type="button"
                data-testid="menu-contact-support"
                class="flex w-full items-center gap-2 border-t border-gray-100 px-4 py-2.5 text-left text-xs text-gray-600 transition-colors hover:bg-primary-50 hover:text-primary-700 dark:border-dark-700 dark:text-gray-300 dark:hover:bg-primary-900/20 dark:hover:text-primary-200 md:hidden"
                @click="openSupportDialog"
              >
                <Icon name="chat" size="sm" />
                <span class="font-semibold">{{ t('common.getSupport') }}</span>
                <Icon name="chevronRight" size="sm" class="ml-auto" />
              </button>

              <div v-if="showOnboardingButton" class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button @click="handleReplayGuide" class="dropdown-item w-full">
                  <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
                    <path
                      d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 14a1 1 0 110 2 1 1 0 010-2zm1.07-7.75c0-.6-.49-1.25-1.32-1.25-.7 0-1.22.4-1.43 1.02a1 1 0 11-1.9-.62A3.41 3.41 0 0111.8 5c2.02 0 3.25 1.4 3.25 2.9 0 2-1.83 2.55-2.43 3.12-.43.4-.47.75-.47 1.23a1 1 0 01-2 0c0-1 .16-1.82 1.1-2.7.69-.64 1.82-1.05 1.82-2.06z"
                    />
                  </svg>
                  {{ $t('onboarding.restartTour') }}
                </button>
              </div>

              <div class="border-t border-gray-100 py-1 dark:border-dark-700">
                <button
                  @click="handleLogout"
                  class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                >
                  <svg
                    class="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="1.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15M12 9l-3 3m0 0l3 3m-3-3h12.75"
                    />
                  </svg>
                  {{ t('nav.logout') }}
                </button>
              </div>
            </div>
          </transition>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <Transition name="support-modal">
        <div
          v-if="supportDialogOpen"
          class="fixed inset-0 z-[100] flex items-center justify-center bg-black/30 p-4 backdrop-blur-sm"
          data-testid="support-overlay"
          @click.self="closeSupportDialog"
        >
          <section
            role="dialog"
            aria-modal="true"
            :aria-label="t('common.supportCommunityTitle')"
            data-testid="support-dialog"
            class="w-[344px] max-w-[calc(100vw-32px)] overflow-hidden rounded-[26px] border border-black/5 bg-white shadow-[0_22px_60px_rgba(15,23,42,0.22)] dark:border-white/10 dark:bg-dark-900"
          >
            <div class="flex items-start justify-between gap-3 px-6 pb-3 pt-5">
              <div class="min-w-0">
                <h2 class="text-base font-semibold text-gray-950 dark:text-white">
                  {{ t('common.supportCommunityTitle') }}
                </h2>
                <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-300">
                  {{ t('common.supportCommunityIntro') }}
                </p>
              </div>
              <button
                type="button"
                data-testid="support-close"
                class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-primary-500 dark:hover:bg-dark-700 dark:hover:text-white"
                :aria-label="t('common.close')"
                @click="closeSupportDialog"
              >
                <Icon name="x" size="sm" :stroke-width="2" />
              </button>
            </div>

            <div class="px-5">
              <div
                role="tablist"
                :aria-label="t('common.supportCommunityTitle')"
                class="grid grid-cols-2 rounded-full bg-gray-100 p-1 dark:bg-dark-800"
              >
                <button
                  type="button"
                  role="tab"
                  data-testid="support-tab-qq"
                  :aria-selected="activeSupportTab === 'qq'"
                  class="h-8 rounded-full text-sm font-semibold transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
                  :class="activeSupportTab === 'qq' ? 'bg-[#17191c] text-white shadow-sm dark:bg-white dark:text-gray-950' : 'text-gray-500 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'"
                  @click="activeSupportTab = 'qq'"
                >
                  {{ t('common.supportQQTab') }}
                </button>
                <button
                  type="button"
                  role="tab"
                  data-testid="support-tab-wechat"
                  :aria-selected="activeSupportTab === 'wechat'"
                  class="h-8 rounded-full text-sm font-semibold transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
                  :class="activeSupportTab === 'wechat' ? 'bg-[#17191c] text-white shadow-sm dark:bg-white dark:text-gray-950' : 'text-gray-500 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'"
                  @click="activeSupportTab = 'wechat'"
                >
                  {{ t('common.supportWeChatTab') }}
                </button>
              </div>
            </div>

            <div class="px-6 pb-6 pt-5 text-center">
              <div
                class="flex h-[240px] w-full items-center justify-center overflow-hidden rounded-[18px] border p-3"
                :class="activeSupportQRCode ? 'border-gray-200 bg-white' : 'border-dashed border-gray-300 bg-gray-50 dark:border-dark-600 dark:bg-dark-800'"
              >
                <img
                  v-if="activeSupportQRCode"
                  :src="activeSupportQRCode"
                  :alt="t('common.supportQRCodeAlt', { platform: activeSupportPlatform })"
                  data-testid="support-qr-image"
                  class="h-full w-full object-contain"
                >
                <div v-else data-testid="support-qr-empty" class="px-3 text-center">
                  <span class="mx-auto flex h-11 w-11 items-center justify-center rounded-full bg-gray-100 text-gray-400 dark:bg-dark-700 dark:text-dark-300">
                    <Icon name="chat" size="lg" :stroke-width="1.75" />
                  </span>
                  <p class="mt-3 text-sm font-semibold text-gray-800 dark:text-white">
                    {{ t('common.supportQRCodeEmptyTitle', { platform: activeSupportPlatform }) }}
                  </p>
                  <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-300">
                    {{ t('common.supportQRCodeEmptyDescription') }}
                  </p>
                </div>
              </div>
              <p
                class="mt-3 h-4 text-xs text-gray-500 dark:text-dark-300"
                :class="{ invisible: !activeSupportQRCode }"
              >
                {{ t('common.supportQRCodeHint', { platform: activeSupportPlatform }) }}
              </p>
            </div>
          </section>
        </div>
      </Transition>
    </Teleport>
  </header>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import SubscriptionProgressMini from '@/components/common/SubscriptionProgressMini.vue'
import AnnouncementBell from '@/components/common/AnnouncementBell.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'
import walletArtwork from '@/assets/wallet-fluid-blue-violet.png'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const onboardingStore = useOnboardingStore()

const user = computed(() => authStore.user)
const dropdownOpen = ref(false)
const walletOpen = ref(false)
const walletPinned = ref(false)
const supportDialogOpen = ref(false)
const activeSupportTab = ref<'qq' | 'wechat'>('qq')
const dropdownRef = ref<HTMLElement | null>(null)
const walletRef = ref<HTMLElement | null>(null)
const supportQQGroupQRCode = computed(() => appStore.supportQQGroupQRCode?.trim() || '')
const supportWeChatGroupQRCode = computed(() => appStore.supportWeChatGroupQRCode?.trim() || '')
const hasSupportGroup = computed(() => Boolean(supportQQGroupQRCode.value || supportWeChatGroupQRCode.value))
const activeSupportQRCode = computed(() => activeSupportTab.value === 'qq' ? supportQQGroupQRCode.value : supportWeChatGroupQRCode.value)
const activeSupportPlatform = computed(() => activeSupportTab.value === 'qq' ? t('common.supportQQTab') : t('common.supportWeChatTab'))
const docUrl = computed(() => sanitizeUrl(appStore.docUrl))
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const balanceAvailableText = computed(() => t('common.availableBalance') === 'common.availableBalance' ? '可用余额' : t('common.availableBalance'))
const balanceFrozenText = computed(() => t('common.frozenBalance') === 'common.frozenBalance' ? '冻结金额' : t('common.frozenBalance'))
const walletText = computed(() => t('common.wallet') === 'common.wallet' ? '钱包' : t('common.wallet'))
const rechargeText = computed(() => t('common.recharge') === 'common.recharge' ? '充值' : t('common.recharge'))
const rechargeNowText = computed(() => t('common.rechargeNow') === 'common.rechargeNow' ? '立即充值' : t('common.rechargeNow'))

// 只在标准模式的管理员下显示新手引导按钮
const showOnboardingButton = computed(() => {
  return !authStore.isSimpleMode && user.value?.role === 'admin'
})

const userInitials = computed(() => {
  if (!user.value) return ''
  // Prefer username, fallback to email
  if (user.value.username) {
    return user.value.username.substring(0, 2).toUpperCase()
  }
  if (user.value.email) {
    // Get the part before @ and take first 2 chars
    const localPart = user.value.email.split('@')[0]
    return localPart.substring(0, 2).toUpperCase()
  }
  return ''
})

const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

const pageTitle = computed(() => {
  // For custom pages, use the menu item's label instead of generic "自定义页面"
  if (route.name === 'CustomPage') {
    const id = route.params.id as string
    const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
    const menuItem = publicItems.find((item) => item.id === id)
      ?? (authStore.isAdmin ? adminSettingsStore.customMenuItems.find((item) => item.id === id) : undefined)
    if (menuItem?.label) return menuItem.label
  }
  const titleKey = route.meta.titleKey as string
  if (titleKey) {
    return t(titleKey)
  }
  return (route.meta.title as string) || ''
})

const pageDescription = computed(() => {
  const descKey = route.meta.descriptionKey as string
  if (descKey) {
    return t(descKey)
  }
  return (route.meta.description as string) || ''
})

function toggleMobileSidebar() {
  appStore.toggleMobileSidebar()
}

function toggleDropdown() {
  walletOpen.value = false
  dropdownOpen.value = !dropdownOpen.value
}

function closeDropdown() {
  dropdownOpen.value = false
}

function openWallet() {
  dropdownOpen.value = false
  walletOpen.value = true
}

function pinWallet() {
  dropdownOpen.value = false
  walletOpen.value = true
  walletPinned.value = true
}

function handleWalletMouseLeave() {
  if (!walletPinned.value) {
    closeWallet()
  }
}

function closeWallet() {
  walletOpen.value = false
  walletPinned.value = false
}

async function navigateFromWallet(path: string) {
  closeWallet()
  closeDropdown()
  await router.push(path)
}

function goToRecharge() {
  return navigateFromWallet('/purchase')
}

function openSupportDialog() {
  closeDropdown()
  closeWallet()
  activeSupportTab.value = supportQQGroupQRCode.value ? 'qq' : 'wechat'
  supportDialogOpen.value = true
}

function closeSupportDialog() {
  supportDialogOpen.value = false
}

async function handleLogout() {
  closeDropdown()
  try {
    await authStore.logout()
  } catch (error) {
    // Ignore logout errors - still redirect to login
    console.error('Logout error:', error)
  }
  await router.push('/login')
}

function handleReplayGuide() {
  closeDropdown()
  onboardingStore.replay()
}

function formatHeaderMoney(value: number) {
  if (!Number.isFinite(value)) return '$0.00'
  return `$${value.toFixed(2)}`
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    closeDropdown()
  }
  if (walletRef.value && !walletRef.value.contains(event.target as Node)) {
    closeWallet()
  }
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && supportDialogOpen.value) {
    closeSupportDialog()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}

.support-modal-enter-active,
.support-modal-leave-active {
  transition: opacity 0.18s ease;
}

.support-modal-enter-active section,
.support-modal-leave-active section {
  transition: transform 0.18s ease, opacity 0.18s ease;
}

.support-modal-enter-from,
.support-modal-leave-to {
  opacity: 0;
}

.support-modal-enter-from section,
.support-modal-leave-to section {
  opacity: 0;
  transform: scale(0.96) translateY(6px);
}
</style>
