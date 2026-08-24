/**
 * Type definitions for Vue Router meta fields
 * Extends the RouteMeta interface with custom properties
 */

import 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    /**
     * Whether this route requires authentication
     * @default true
     */
    requiresAuth?: boolean

    /**
     * Whether this route requires admin role
     * @default false
     */
    requiresAdmin?: boolean

    /**
     * Page title for this route
     */
    title?: string

    /**
     * Optional breadcrumb items for navigation
     */
    breadcrumbs?: Array<{
      label: string
      to?: string
    }>

    /**
     * Icon name for this route (for sidebar navigation)
     */
    icon?: string

    /**
     * Whether to hide this route from navigation menu
     * @default false
     */
    hideInMenu?: boolean

    /**
     * Whether this route requires internal payment system to be enabled
     * @default false
     */
    requiresPayment?: boolean

    /**
     * 是否要求风控中心功能开关已启用
     * @default false
     */
    requiresRiskControl?: boolean

    /**
     * Whether this route requires the reward campaign center rollout switch.
     * @default false
     */
    requiresRewardCampaigns?: boolean

    /**
     * i18n key for the page title
     */
    titleKey?: string

    /**
     * 落地页完整文档标题（星链AI SEO 标题，原样使用、不追加站点名后缀）。
     * 设置后优先级高于 title/titleKey。
     */
    landingTitle?: string

    /**
     * i18n key for the page description
     */
    descriptionKey?: string

    /**
     * Hide the desktop page title block while preserving header actions.
     * @default false
     */
    hideHeaderTitle?: boolean

    /**
     * Let a route own the complete main-content canvas while preserving
     * the shared header and sidebar.
     * @default false
     */
    fullBleedContent?: boolean

    /**
     * Hide the shared page mesh when a route supplies its own visual canvas.
     * @default false
     */
    hideBackgroundMesh?: boolean
  }
}
