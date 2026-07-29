<template>
  <header class="site-header">
    <nav class="navbar" aria-label="主导航">
      <router-link class="logo" to="/" :aria-label="`${brandName} home`">
        <img class="logo-image" src="/logo.png" alt="" />
        <span>{{ brandName }}</span>
      </router-link>
      <div class="nav-actions">
        <router-link class="nav-account-link" to="/login">登录</router-link>
        <router-link class="nav-account-link nav-account-link--strong" to="/register">
          注册
        </router-link>
        <button
          class="menu-button"
          type="button"
          :aria-expanded="isOpen"
          aria-controls="site-drawer"
          @click="isOpen = !isOpen"
        >
          菜单
          <svg
            aria-hidden="true"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.25"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="m18 15-6-6-6 6" />
          </svg>
        </button>
      </div>
    </nav>

    <div
      id="site-drawer"
      class="drawer-overlay"
      :class="{ 'drawer-overlay--open': isOpen }"
      :aria-hidden="!isOpen"
    >
      <button class="drawer-close" type="button" aria-label="关闭菜单" @click="isOpen = false">
        关闭
      </button>
      <div class="drawer-inner">
        <ul class="drawer-links">
          <li v-for="link in navLinks" :key="link.href">
            <router-link
              :to="link.href"
              :aria-current="route.path === link.href ? 'page' : undefined"
              @click="isOpen = false"
            >
              {{ link.label }}
            </router-link>
          </li>
        </ul>
        <p class="drawer-footer">© {{ currentYear }} {{ brandName }} · 模型 API 服务</p>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { brandName, navLinks } from '@/data/landing'

const route = useRoute()
const isOpen = ref(false)
const currentYear = new Date().getFullYear()
</script>
