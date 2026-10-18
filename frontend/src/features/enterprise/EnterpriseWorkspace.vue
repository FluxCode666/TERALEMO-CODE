<template>
  <div ref="root" class="shadcn-scope mx-auto w-full min-w-0 space-y-6 text-foreground motion-safe:transition-[max-width] motion-safe:duration-300 motion-reduce:transition-none" :class="wideLayout ? 'max-w-full' : 'max-w-6xl'" :data-layout="wideLayout ? 'wide' : 'narrow'">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="min-w-0 flex-1"><h1 class="max-w-full break-words text-2xl font-bold text-foreground">{{ enterprise?.name || t('enterprise.title') }}</h1><p v-if="overview" class="mt-1 break-words text-sm text-muted-foreground">{{ t(`enterprise.roles.${overview.me.role}`) }} · {{ overview.me.department_name || t('enterprise.noDepartment') }}</p></div>
      <div class="flex flex-wrap items-center gap-3" data-test="enterprise-header-actions">
        <slot name="header-actions" />
        <Button variant="outline"  :disabled="busy" data-test="enterprise-refresh" @click="refresh"><RefreshCw class="mr-2 size-4" :class="busy ? 'animate-spin' : ''" />{{ t('enterprise.refresh') }}</Button>
      </div>
    </div>
    <EnterpriseOwnerWarning v-if="adminEnterprise" :active-owner-count="adminEnterprise.active_owner_count" />
    <p v-if="readonly" class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300" data-test="enterprise-readonly">{{ t('enterprise.readonly') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <Button variant="ghost" class="underline" data-test="enterprise-refresh" @click="refresh">{{ t('enterprise.retry') }}</Button></p>
    <div v-if="busy && !enterprise" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4" aria-busy="true"><Skeleton v-for="n in 4" :key="n" class="h-36" /></div>
    <template v-if="enterprise">
      <component :is="admin ? Tabs : 'div'" :model-value="admin ? activeTab : undefined" :unmount-on-hide="admin ? true : undefined" class="min-w-0" @update:model-value="selectTab">
        <div v-if="admin" class="overflow-x-auto pb-1"><TabsList class="h-auto min-w-max justify-start" :aria-label="t('enterprise.title')"><TabsTrigger v-for="tab in tabs" :key="tab" :value="tab" :data-test="`tab-${tab}`" :aria-current="activeTab === tab ? 'page' : undefined" @click="activeTab = tab">{{ t(`enterprise.${tab}`) }}</TabsTrigger></TabsList></div>
        <component :is="admin ? TabsContent : 'div'" :value="admin ? activeTab : undefined" :class="admin ? 'mt-5' : undefined" data-tab-content>
      <section v-if="activeTab === 'overview'" class="space-y-5">
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <EnterpriseMetric v-if="balanceVisible && balance !== null" data-test="enterprise-balance" :title="t('enterprise.balance')" :value="balance" currency />
          <template v-if="adminEnterprise">
            <EnterpriseMetric :title="t('enterprise.totalRecharged')" :value="adminEnterprise.total_recharged" currency />
            <EnterpriseMetric :title="t('enterprise.memberCount')" :value="adminEnterprise.member_count" />
            <EnterpriseMetric :title="t('enterprise.departmentCount')" :value="adminEnterprise.department_count" />
          </template>
          <template v-if="overview"><EnterpriseMetric :title="t('enterprise.todayCost')" :value="overview.stats.today_cost" currency /><EnterpriseMetric :title="t('enterprise.monthCost')" :value="overview.stats.month_cost" currency /></template>
        </div>
        <Card class="rounded-ui-lg border bg-card text-card-foreground shadow-sm flex flex-wrap items-center justify-between gap-4 p-5"><div class="flex flex-wrap items-center gap-6"><div><span class="text-sm text-muted-foreground">{{ t('enterprise.status') }}</span><p class="mt-1 font-medium text-foreground"><Badge variant="secondary">{{ t(enterprise.status === 'active' ? 'enterprise.active' : 'enterprise.disabled') }}</Badge></p></div><div v-if="overview"><span class="text-sm text-muted-foreground">{{ t('enterprise.organizationUi.myIdentity') }}</span><p class="mt-1 font-medium text-foreground">{{ t(`enterprise.roles.${overview.me.role}`) }} · {{ overview.me.department_name || t('enterprise.noDepartment') }}</p></div></div><Button variant="default" v-if="admin"  @click="balanceOpen = true">{{ t('enterprise.adjustBalance') }}</Button></Card>
        <EnterpriseAdminQuotaSummary v-if="admin" :api="api" :quota="enterpriseQuota" :refresh-key="refreshKey" @manage="activeTab = 'quotas'" />
        <div v-else class="grid gap-4 xl:grid-cols-3"><EnterpriseQuotaDisplay v-if="enterpriseQuota" data-test="enterprise-quota" :quota="enterpriseQuota" :title="t('enterprise.quotaSections.enterprise.title')" compact />
        <EnterpriseQuotaDisplay v-if="overview?.department_quota_visible && overview.department_quota" data-test="department-quota" :quota="overview.department_quota" :title="t('enterprise.quotaSections.department.title')" compact />
        <EnterpriseQuotaDisplay v-if="overview?.member_quota_visible && overview.member_quota" data-test="member-quota" :quota="overview.member_quota" :title="t('enterprise.myQuota')" compact />
        </div><p v-if="enterpriseQuota || overview?.member_quota || overview?.department_quota" class="text-sm text-muted-foreground">{{ t('enterprise.quotaHint') }}</p>
      </section>
      <EnterpriseOrganizationPanel v-else-if="activeTab === 'organization'" :enterprise-name="enterprise.name" :member-count="adminEnterprise?.member_count" :api="api" :admin="admin" :readonly="readonly" :overview="overview" :refresh-key="refreshKey" @updated="updated" />
      <EnterpriseQuotaPanel v-else-if="activeTab === 'quotas'" :enterprise-name="enterprise.name" :api="api" :admin="admin" :readonly="readonly" :overview="overview" :refresh-key="refreshKey" @updated="updated" />
      <EnterpriseGroupsPanel v-else-if="activeTab === 'groups'" :api="api" :enterprise-id="enterprise.id" :admin="admin" :readonly="readonly" :overview="overview" :refresh-key="refreshKey" @updated="updated" />
      <EnterpriseUsagePanel v-else-if="activeTab === 'usage'" :api="api" :admin="admin" :overview="overview" :refresh-key="refreshKey" />
      <EnterpriseLogsPanel v-else-if="activeTab === 'audit' || activeTab === 'balanceLogs'" :key="activeTab" :api="api" :kind="activeTab" :enterprise="enterprise" :admin="admin" :overview="overview" :refresh-key="refreshKey" />
      <div v-else-if="activeTab === 'settings'" class="space-y-5"><EnterpriseBasicsForm v-if="adminEnterprise" :key="adminEnterprise.updated_at" :enterprise="adminEnterprise" @updated="updated" /><EnterpriseSettingsPanel :api="api" :admin="admin" :enterprise-id="enterprise.id" :settings="adminEnterprise?.settings ?? overview?.settings" :readonly="readonly" :refresh-key="refreshKey" @updated="updated" /></div>
        </component>
      </component>
    </template>
    <EnterpriseDialog :show="balanceOpen" :title="t('enterprise.adjustBalance')" @close="balanceOpen = false">
      <EnterpriseBalanceForm v-if="balanceOpen && enterpriseId" :enterprise-id="enterpriseId" @updated="balanceOpen = false; updated()" />
    </EnterpriseDialog>
  </div>
</template>

<script setup lang="ts">
import { Card } from '@/components/ui/card'

import { Button } from '@/components/ui/button'

import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { enterpriseAPI } from '@/api/enterprise'
import { enterprisesAPI } from '@/api/admin/enterprises'
import type { Enterprise, EnterpriseOverview } from '@/types/enterprise'
import EnterpriseMetric from './components/EnterpriseMetric.vue'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { RefreshCw } from '@lucide/vue'
import { useMotion } from '@/composables/useMotion'
import EnterpriseDialog from '@/features/enterprise/components/EnterpriseDialog.vue'
import EnterpriseOrganizationPanel from './components/EnterpriseOrganizationPanel.vue'
import EnterpriseAdminQuotaSummary from './components/EnterpriseAdminQuotaSummary.vue'
import EnterpriseQuotaDisplay from './components/EnterpriseQuotaDisplay.vue'
import EnterpriseQuotaPanel from './components/EnterpriseQuotaPanel.vue'
import EnterpriseGroupsPanel from './components/EnterpriseGroupsPanel.vue'
import EnterpriseUsagePanel from './components/EnterpriseUsagePanel.vue'
import EnterpriseLogsPanel from './components/EnterpriseLogsPanel.vue'
import EnterpriseSettingsPanel from './components/EnterpriseSettingsPanel.vue'
import EnterpriseBasicsForm from './components/EnterpriseBasicsForm.vue'
import EnterpriseBalanceForm from './components/EnterpriseBalanceForm.vue'
import EnterpriseOwnerWarning from './components/EnterpriseOwnerWarning.vue'
import { useEnterpriseAction } from './useEnterpriseAction'
import { getEnterpriseTabs, type EnterpriseSection, type EnterpriseTab } from './navigation'
const props = defineProps<{ admin?: boolean; enterpriseId?: number; overview?: EnterpriseOverview; section?: EnterpriseSection }>()
const emit = defineEmits<{ updated: []; navigate: [section: EnterpriseSection] }>()
const { t } = useI18n()
const { busy, error, read } = useEnterpriseAction()
const adminEnterprise = ref<Enterprise | null>(null)
const enterprise = computed(() => props.admin ? adminEnterprise.value : props.overview?.enterprise)
const api = computed(() => props.admin ? enterprisesAPI.forEnterprise(props.enterpriseId!) : enterpriseAPI)
const readonly = computed(() => !props.admin && (enterprise.value?.status !== 'active' || props.overview?.me.status !== 'active'))
const balanceVisible = computed(() => props.admin || props.overview?.balance_visible)
const balance = computed(() => props.admin ? adminEnterprise.value?.balance ?? null : props.overview?.balance ?? null)
const enterpriseQuota = computed(() => props.admin ? adminEnterprise.value?.quota : props.overview?.enterprise_quota)
const tabs = computed(() => getEnterpriseTabs(props.overview, props.admin))
const selectedTab = ref<EnterpriseTab>('overview')
const activeTab = computed<EnterpriseTab>({
  get: () => props.admin ? selectedTab.value : (props.section && tabs.value.includes(props.section) ? props.section : 'overview'),
  set: value => {
    if (props.admin) selectedTab.value = value
    else if (value !== 'balanceLogs') emit('navigate', value)
  }
})
function selectTab(value: string | number) {
  if (tabs.value.includes(value as EnterpriseTab)) activeTab.value = value as EnterpriseTab
}
const wideLayout = computed(() => ['organization', 'usage'].includes(activeTab.value))
const root = ref<HTMLElement | null>(null)
const motion = useMotion(root)
async function animateTab() { await nextTick(); motion.stop(); motion.fadeIn('[data-tab-content]'); motion.stagger('[data-metric]') }
watch(activeTab, animateTab)
watch(enterprise, animateTab)
onMounted(animateTab)
const refreshKey = ref(0)
watch(tabs, value => { if (!value.includes(activeTab.value)) activeTab.value = 'overview' })
watch(() => props.overview, () => { if (!props.admin) refreshKey.value++ })
async function refresh() {
  if (props.admin && props.enterpriseId) {
    await read(signal => enterprisesAPI.get(props.enterpriseId!, { signal }), value => {
      adminEnterprise.value = value
      refreshKey.value++
    })
  } else emit('updated')
}
async function updated() { emit('updated'); if (props.admin) await refresh() }
const balanceOpen = ref(false)
onMounted(() => { if (props.admin) void refresh() })
</script>
