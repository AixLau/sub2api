<template>
  <!-- Keep the first frame structurally aligned with DashboardView to avoid layout shifts. -->
  <div data-testid="admin-dashboard-skeleton" class="space-y-6" aria-busy="true">
    <div v-for="row in 2" :key="row" class="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <div v-for="i in 4" :key="i" class="card flex items-center gap-3 p-4">
        <Skeleton variant="circle" :width="36" :height="36" />
        <div class="flex-1 space-y-2">
          <Skeleton height="10px" width="60%" />
          <Skeleton height="20px" width="45%" />
          <Skeleton height="10px" width="70%" />
        </div>
      </div>
    </div>

    <div data-testid="admin-dashboard-skeleton-actions" class="card p-4">
      <Skeleton class="mb-3" height="16px" width="120px" />
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <div v-for="i in 2" :key="i" class="flex items-center gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800/50">
          <Skeleton variant="rect" :width="40" :height="40" />
          <div class="flex-1 space-y-2">
            <Skeleton height="14px" width="45%" />
            <Skeleton height="10px" width="75%" />
          </div>
        </div>
      </div>
    </div>

    <div data-testid="admin-dashboard-skeleton-filter" class="card p-4">
      <div class="flex flex-wrap items-center gap-4">
        <Skeleton height="16px" width="72px" />
        <Skeleton height="40px" width="260px" />
        <Skeleton height="40px" width="88px" />
        <Skeleton class="ml-auto" height="40px" width="112px" />
      </div>
    </div>

    <div data-testid="admin-dashboard-skeleton-charts" class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <div v-for="i in 2" :key="i" class="card p-4">
        <Skeleton class="mb-4" height="16px" width="140px" />
        <Skeleton height="288px" />
      </div>
    </div>

    <div class="card p-4">
      <Skeleton class="mb-4" height="16px" width="150px" />
      <Skeleton height="320px" />
    </div>

    <div class="card p-4">
      <div class="mb-4 flex items-center justify-between">
        <Skeleton height="16px" width="180px" />
        <Skeleton height="32px" width="112px" />
      </div>
      <Skeleton height="260px" />
    </div>
  </div>
</template>

<script setup lang="ts">
import Skeleton from '@/components/common/Skeleton.vue'
</script>
