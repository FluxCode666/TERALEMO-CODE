/**
 * Type definitions for Vue Router meta fields
 * Extends the RouteMeta interface with custom properties
 */

import 'vue-router'
import type { EnterpriseSection } from '@/features/enterprise/navigation'

declare module 'vue-router' {
  interface RouteMeta {
    /**
     * Whether this route requires authentication
     * @default true
     */
    requiresAuth?: boolean

    /** Requires an enterprise owner/manager, or a system admin with enterprise membership. */
    requiresEnterprise?: boolean
    enterpriseSection?: EnterpriseSection

    /** Personal payments, balance redemption and unsupported wallet flows. */
    requiresPersonalWallet?: boolean

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
     * 是否要求订阅功能开关（subscription_enabled，opt-out）未被显式关闭
     * @default false
     */
    requiresSubscription?: boolean

    /**
     * i18n key for the page title
     */
    titleKey?: string

    /**
     * i18n key for the page description
     */
    descriptionKey?: string
  }
}
