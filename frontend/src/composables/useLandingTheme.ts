import { onBeforeUnmount, onMounted } from 'vue'

/**
 * 落地页 / 认证页固定浅色：挂载时移除 html.dark（中和页面内容中残留的 dark: 工具类，
 * 包括共享的 OAuth 按钮等子组件），卸载时按用户偏好恢复主题
 * （规则与 main.ts 的 initThemeClass 一致）。
 */
export function useLandingLightTheme(): void {
  onMounted(() => {
    document.documentElement.classList.remove('dark')
  })

  onBeforeUnmount(() => {
    const savedTheme = localStorage.getItem('theme')
    const shouldUseDark =
      savedTheme === 'dark' ||
      (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
    document.documentElement.classList.toggle('dark', shouldUseDark)
  })
}
