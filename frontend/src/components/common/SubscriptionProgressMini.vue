<template>
  <div
    v-if="hasActiveSubscriptions"
    ref="containerRef"
    data-testid="subscription-control"
    class="relative flex-shrink-0"
    @mouseenter="openTooltip"
    @mouseleave="handleTooltipMouseLeave"
  >
    <button
      @click="pinTooltip"
      class="flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border border-violet-100 bg-violet-50 px-2.5 text-left transition-colors hover:bg-violet-100 focus:outline-none focus:ring-2 focus:ring-violet-500 focus:ring-offset-2 dark:border-violet-900/60 dark:bg-violet-900/20 dark:hover:bg-violet-900/30 dark:focus:ring-offset-dark-900"
      :title="t('subscriptionProgress.viewDetails')"
      :aria-expanded="tooltipOpen"
      aria-haspopup="dialog"
    >
      <span class="relative flex h-5 w-5 items-center justify-center rounded-full bg-violet-600 text-white" aria-hidden="true">
        <Icon name="creditCard" size="xs" :stroke-width="2.25" />
        <span class="absolute -right-1 -top-1 h-2 w-2 rounded-full border-2 border-violet-50 bg-emerald-500 dark:border-dark-800"></span>
      </span>
      <span class="hidden text-sm font-semibold text-violet-900 dark:text-violet-100 md:inline">
        {{ t('subscriptionProgress.title') }}
      </span>
    </button>

    <transition name="dropdown">
      <div
        v-if="tooltipOpen"
        role="dialog"
        :aria-label="t('subscriptionProgress.title')"
        class="absolute right-0 z-50 mt-2 w-[316px] overflow-hidden rounded-[18px] border border-line-default bg-surface-raised shadow-glass"
      >
        <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
          <div class="flex items-start justify-between gap-3">
            <div>
              <h3 class="text-sm font-semibold text-gray-950 dark:text-white">
                {{ t('subscriptionProgress.title') }}
              </h3>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                {{ t('subscriptionProgress.activeCount', { count: activeSubscriptions.length }) }}
              </p>
            </div>
            <button
              type="button"
              data-testid="subscription-close"
              class="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-primary-500 dark:hover:bg-dark-700 dark:hover:text-dark-100"
              :aria-label="t('common.close')"
              @click="closeTooltip"
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
        </div>

        <div class="max-h-60 overflow-y-auto">
          <div
            v-for="subscription in displaySubscriptions"
            :key="subscription.id"
            class="border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700"
          >
            <div class="mb-2 flex items-center justify-between gap-3">
              <span class="min-w-0 truncate text-sm font-semibold text-gray-900 dark:text-white">
                {{ subscription.group?.name || `Group #${subscription.group_id}` }}
              </span>
              <span
                v-if="subscription.expires_at"
                class="flex-shrink-0 text-xs"
                :class="getDaysRemainingClass(subscription.expires_at)"
              >
                {{ formatDaysRemaining(subscription.expires_at) }}
              </span>
            </div>

            <div class="space-y-1.5">
              <div
                v-if="isUnlimited(subscription)"
                class="flex items-center gap-2 rounded-lg bg-emerald-50 px-2.5 py-1.5 dark:bg-emerald-900/20"
              >
                <span class="text-base text-emerald-600 dark:text-emerald-400">∞</span>
                <span class="text-sm font-medium text-emerald-700 dark:text-emerald-300">
                  {{ t('subscriptionProgress.unlimited') }}
                </span>
              </div>

              <template v-else>
                <div v-if="subscription.group?.daily_limit_usd" class="flex items-center gap-2">
                  <span class="w-8 flex-shrink-0 text-xs text-gray-500">{{
                    t('subscriptionProgress.daily')
                  }}</span>
                  <div class="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="
                        getProgressBarClass(
                          subscription.daily_usage_usd,
                          subscription.group?.daily_limit_usd
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          subscription.daily_usage_usd,
                          subscription.group?.daily_limit_usd
                        )
                      }"
                    ></div>
                  </div>
                  <span class="w-24 flex-shrink-0 text-right text-xs text-gray-500">
                    {{
                      formatUsage(subscription.daily_usage_usd, subscription.group?.daily_limit_usd)
                    }}
                  </span>
                </div>

                <div v-if="subscription.group?.weekly_limit_usd" class="flex items-center gap-2">
                  <span class="w-8 flex-shrink-0 text-xs text-gray-500">{{
                    t('subscriptionProgress.weekly')
                  }}</span>
                  <div class="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="
                        getProgressBarClass(
                          subscription.weekly_usage_usd,
                          subscription.group?.weekly_limit_usd
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          subscription.weekly_usage_usd,
                          subscription.group?.weekly_limit_usd
                        )
                      }"
                    ></div>
                  </div>
                  <span class="w-24 flex-shrink-0 text-right text-xs text-gray-500">
                    {{
                      formatUsage(subscription.weekly_usage_usd, subscription.group?.weekly_limit_usd)
                    }}
                  </span>
                </div>

                <div v-if="subscription.group?.monthly_limit_usd" class="flex items-center gap-2">
                  <span class="w-8 flex-shrink-0 text-xs text-gray-500">{{
                    t('subscriptionProgress.monthly')
                  }}</span>
                  <div class="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="
                        getProgressBarClass(
                          subscription.monthly_usage_usd,
                          getEffectiveMonthlyLimit(subscription)
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          subscription.monthly_usage_usd,
                          getEffectiveMonthlyLimit(subscription)
                        )
                      }"
                    ></div>
                  </div>
                  <span class="w-24 flex-shrink-0 text-right text-xs text-gray-500">
                    {{
                      formatUsage(
                        subscription.monthly_usage_usd,
                        getEffectiveMonthlyLimit(subscription)
                      )
                    }}
                  </span>
                </div>
              </template>
            </div>
          </div>
        </div>

        <div class="border-t border-gray-100 px-4 py-2.5 dark:border-dark-700">
          <router-link
            to="/subscriptions"
            @click="closeTooltip"
            class="block w-full text-center text-sm font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
          >
            {{ t('subscriptionProgress.viewAll') }}
          </router-link>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useSubscriptionStore } from '@/stores'
import type { UserSubscription } from '@/types'

const { t } = useI18n()

const subscriptionStore = useSubscriptionStore()

const containerRef = ref<HTMLElement | null>(null)
const tooltipOpen = ref(false)
const tooltipPinned = ref(false)

// Use store data instead of local state
const activeSubscriptions = computed(() => subscriptionStore.activeSubscriptions)
const hasActiveSubscriptions = computed(() => subscriptionStore.hasActiveSubscriptions)

const displaySubscriptions = computed(() => {
  // Sort by most usage (highest percentage first)
  return [...activeSubscriptions.value].sort((a, b) => {
    const aMax = getMaxUsagePercentage(a)
    const bMax = getMaxUsagePercentage(b)
    return bMax - aMax
  })
})

function getMaxUsagePercentage(sub: UserSubscription): number {
  const percentages: number[] = []
  if (sub.group?.daily_limit_usd) {
    percentages.push(((sub.daily_usage_usd || 0) / sub.group.daily_limit_usd) * 100)
  }
  if (sub.group?.weekly_limit_usd) {
    percentages.push(((sub.weekly_usage_usd || 0) / sub.group.weekly_limit_usd) * 100)
  }
  if (sub.group?.monthly_limit_usd) {
    percentages.push(((sub.monthly_usage_usd || 0) / getEffectiveMonthlyLimit(sub)) * 100)
  }
  return percentages.length > 0 ? Math.max(...percentages) : 0
}

function getEffectiveMonthlyLimit(sub: UserSubscription): number {
  return (sub.group?.monthly_limit_usd || 0) + (sub.monthly_bonus_usd || 0)
}

function isUnlimited(sub: UserSubscription): boolean {
  return (
    !sub.group?.daily_limit_usd &&
    !sub.group?.weekly_limit_usd &&
    !sub.group?.monthly_limit_usd
  )
}

function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return 'bg-gray-400'
  const percentage = ((used || 0) / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return '0%'
  const percentage = Math.min(((used || 0) / limit) * 100, 100)
  return `${percentage}%`
}

function formatUsage(used: number | undefined, limit: number | null | undefined): string {
  const usedValue = (used || 0).toFixed(2)
  const limitValue = limit?.toFixed(2) || '∞'
  return `$${usedValue}/$${limitValue}`
}

function formatDaysRemaining(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  if (diff < 0) return t('subscriptionProgress.expired')
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
  if (days === 0) return t('subscriptionProgress.expiresToday')
  if (days === 1) return t('subscriptionProgress.expiresTomorrow')
  return t('subscriptionProgress.daysRemaining', { days })
}

function getDaysRemainingClass(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
  if (days <= 3) return 'text-red-600 dark:text-red-400'
  if (days <= 7) return 'text-orange-600 dark:text-orange-400'
  return 'text-gray-500 dark:text-dark-400'
}

function pinTooltip() {
  tooltipOpen.value = true
  tooltipPinned.value = true
}

function openTooltip() {
  tooltipOpen.value = true
}

function handleTooltipMouseLeave() {
  if (!tooltipPinned.value) {
    closeTooltip()
  }
}

function closeTooltip() {
  tooltipOpen.value = false
  tooltipPinned.value = false
}

function handleClickOutside(event: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    closeTooltip()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  // Trigger initial fetch if not already loaded
  // The actual data loading is handled by App.vue globally
  subscriptionStore.fetchActiveSubscriptions().catch((error) => {
    console.error('Failed to load subscriptions in SubscriptionProgressMini:', error)
  })
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
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
</style>
