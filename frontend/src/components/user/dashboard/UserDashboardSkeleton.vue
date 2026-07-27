<template>
  <!-- 布局与 DashboardView 实际内容对齐:两行统计卡 → 图表卡 → 最近用量 + 快捷操作 -->
  <div data-testid="user-dashboard-skeleton" class="space-y-6" aria-busy="true">
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

    <div class="card p-4">
      <div class="flex flex-wrap items-center gap-4">
        <Skeleton height="32px" width="260px" />
        <Skeleton height="40px" width="88px" />
        <Skeleton class="ml-auto" height="40px" width="112px" />
      </div>
    </div>

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <div class="card p-4">
        <Skeleton class="mb-4" height="16px" width="140px" />
        <div class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
          <Skeleton variant="circle" :width="192" :height="192" />
          <div class="w-full flex-1 space-y-3">
            <Skeleton v-for="i in 5" :key="i" height="24px" />
          </div>
        </div>
      </div>
      <div class="card p-4">
        <Skeleton class="mb-4" height="16px" width="140px" />
        <Skeleton height="224px" />
      </div>
    </div>

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
      <div class="card space-y-3 p-4 lg:col-span-2">
        <Skeleton height="16px" width="120px" />
        <Skeleton v-for="i in 5" :key="i" height="40px" />
      </div>
      <div class="card space-y-3 p-4 lg:col-span-1">
        <Skeleton height="16px" width="100px" />
        <Skeleton v-for="i in 4" :key="i" height="44px" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Skeleton from '@/components/common/Skeleton.vue'
</script>
