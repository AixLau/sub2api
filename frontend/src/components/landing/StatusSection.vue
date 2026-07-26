<template>
  <section id="status" class="content-section status-section" aria-labelledby="status-title">
    <div class="content-inner status-panel">
      <div class="status-copy">
        <p class="status-kicker">
          <span aria-hidden="true" />
          Service status
        </p>
        <h2 id="status-title">状态与用量，都有迹可循。</h2>
        <p>登录后可查看服务状态、请求明细和费用信息。</p>
        <router-link class="inline-action inline-action--light" to="/monitor">
          登录后查看服务状态
          <svg
            aria-hidden="true"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M7 7h10v10" />
            <path d="M7 17 17 7" />
          </svg>
        </router-link>
      </div>

      <div class="status-list">
        <div v-for="item in statusItems" :key="item.label" class="status-item">
          <svg
            aria-hidden="true"
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
            v-html="item.iconPaths"
          ></svg>
          <div>
            <strong>{{ item.label }}</strong>
            <span>{{ item.detail }}</span>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// 图标为 lucide 的 route / gauge / key-round 路径（与原 React 版一致）
const statusItems = [
  {
    label: '服务状态',
    detail: '查看当前服务可用情况',
    iconPaths:
      '<circle cx="6" cy="19" r="3" /><path d="M9 19h8.5a3.5 3.5 0 0 0 0-7h-11a3.5 3.5 0 0 1 0-7H15" /><circle cx="18" cy="5" r="3" />'
  },
  {
    label: '调用记录',
    detail: '查询延迟、Token、费用与错误',
    iconPaths: '<path d="m12 14 4-4" /><path d="M3.34 19a10 10 0 1 1 17.32 0" />'
  },
  {
    label: '额度管理',
    detail: '查看余额、订阅与 API Key 用量',
    iconPaths:
      '<path d="M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z" /><circle cx="16.5" cy="7.5" r=".5" fill="currentColor" />'
  }
]
</script>
