<template>
  <!-- 布局与管理员 DashboardView 对齐:两行统计卡 → 图表区 → 双栏面板 -->
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

    <div class="card p-4">
      <div class="mb-4 flex items-center justify-between">
        <Skeleton height="16px" width="140px" />
        <Skeleton height="32px" width="260px" />
      </div>
      <Skeleton height="288px" />
    </div>

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <div v-for="i in 2" :key="i" class="card space-y-3 p-4">
        <Skeleton height="16px" width="130px" />
        <Skeleton v-for="j in 4" :key="j" height="44px" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Skeleton from '@/components/common/Skeleton.vue'
</script>
