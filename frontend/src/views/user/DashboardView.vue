<template>
  <AppLayout>
    <div class="user-dashboard">
      <div class="user-dashboard__canvas" data-testid="user-dashboard">
        <UserDashboardDecorations />
        <UserDashboardHero :display-name="displayName" />

        <UserDashboardSkeleton v-if="loading" />
        <div
          v-else-if="stats"
          class="user-dashboard__content"
          :aria-busy="loadingCharts || loadingUsage || loadingActivity"
        >
          <UserDashboardStats
            :stats="stats"
            :balance="user?.balance || 0"
            :is-simple="authStore.isSimpleMode"
            :activity="activity"
          />
          <UserDashboardActivity :activity="activity" :loading="loadingActivity" />
          <UserDashboardCharts
            v-model:startDate="startDate"
            v-model:endDate="endDate"
            v-model:granularity="granularity"
            :loading="loadingCharts"
            :trend="trendData"
            :models="modelStats"
            @dateRangeChange="loadCharts"
            @granularityChange="loadCharts"
            @refresh="refreshAll"
          />
          <div class="user-dashboard__bottom-grid">
            <UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" />
            <UserDashboardQuickActions />
          </div>
        </div>
        <section
          v-else-if="statsError"
          class="user-dashboard__error user-dashboard__surface"
          role="alert"
          data-testid="dashboard-load-error"
        >
          <p>{{ t('dashboard.loadFailed') }}</p>
          <button type="button" class="user-dashboard__retry" @click="refreshAll">
            {{ t('errors.tryAgain') }}
          </button>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import {
  usageAPI,
  type UserDashboardActivity as UserActivityType,
  type UserDashboardStats as UserStatsType,
} from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserDashboardHero from '@/components/user/dashboard/UserDashboardHero.vue'
import UserDashboardDecorations from '@/components/user/dashboard/UserDashboardDecorations.vue'
import UserDashboardSkeleton from '@/components/user/dashboard/UserDashboardSkeleton.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardActivity from '@/components/user/dashboard/UserDashboardActivity.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat } from '@/types'
import { formatDateLocalInput } from '@/utils/format'
import '@/styles/user-dashboard.css'

type DashboardGranularity = 'day' | 'hour'

const { t } = useI18n()
const authStore = useAuthStore()
const user = computed(() => authStore.user)
const displayName = computed(() => {
  const username = user.value?.username?.trim()
  if (username) return username
  return user.value?.email?.split('@')[0]?.trim() || ''
})
const stats = ref<UserStatsType | null>(null)
const activity = ref<UserActivityType | null>(null)
const loading = ref(true)
const loadingUsage = ref(false)
const loadingCharts = ref(false)
const loadingActivity = ref(false)
const statsError = ref(false)
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const recentUsage = ref<UsageLog[]>([])
const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatDateLocalInput(new Date()))
const granularity = ref<DashboardGranularity>('day')

const loadStats = async () => {
  const isInitialLoad = stats.value === null
  statsError.value = false
  if (isInitialLoad) loading.value = true
  try {
    const [, dashboardStats] = await Promise.all([
      authStore.refreshUser(),
      usageAPI.getDashboardStats()
    ])
    stats.value = dashboardStats
  } catch (error) {
    statsError.value = true
    console.error('Failed to load dashboard stats:', error)
  } finally {
    if (isInitialLoad) loading.value = false
  }
}
const loadActivity = async () => {
  loadingActivity.value = true
  try {
    activity.value = await usageAPI.getDashboardActivity()
  } catch (error) {
    console.error('Failed to load dashboard activity:', error)
  } finally {
    loadingActivity.value = false
  }
}

const loadCharts = async () => {
  loadingCharts.value = true
  try {
    const [trendResponse, modelsResponse] = await Promise.all([
      usageAPI.getDashboardTrend({
        start_date: startDate.value,
        end_date: endDate.value,
        granularity: granularity.value,
      }),
      usageAPI.getDashboardModels({
        start_date: startDate.value,
        end_date: endDate.value,
      }),
    ])
    trendData.value = trendResponse.trend || []
    modelStats.value = modelsResponse.models || []
  } catch (error) {
    console.error('Failed to load dashboard charts:', error)
  } finally {
    loadingCharts.value = false
  }
}

const loadRecent = async () => {
  loadingUsage.value = true
  try {
    const response = await usageAPI.getByDateRange(startDate.value, endDate.value)
    recentUsage.value = response.items.slice(0, 5)
  } catch (error) {
    console.error('Failed to load recent usage:', error)
  } finally {
    loadingUsage.value = false
  }
}

const refreshAll = () => {
  void loadStats()
  void loadActivity()
  void loadCharts()
  void loadRecent()
}

onMounted(refreshAll)
</script>
