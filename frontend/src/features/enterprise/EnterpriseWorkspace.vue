<template>
  <div class="min-w-0 space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="min-w-0 flex-1"><h1 class="max-w-full break-words text-2xl font-bold text-gray-900 dark:text-white">{{ enterprise?.name || t('enterprise.title') }}</h1><p v-if="overview" class="mt-1 break-words text-sm text-gray-500 dark:text-gray-400">{{ t(`enterprise.roles.${overview.me.role}`) }} · {{ overview.me.department_name || t('enterprise.noDepartment') }}</p></div>
      <button class="btn btn-secondary" :disabled="busy" @click="refresh">{{ t('enterprise.refresh') }}</button>
    </div>
    <p v-if="readonly" class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300" data-test="enterprise-readonly">{{ t('enterprise.readonly') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }} <button class="underline" @click="refresh">{{ t('enterprise.retry') }}</button></p>
    <div v-if="busy && !enterprise" class="h-40 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-800"></div>
    <template v-if="enterprise">
      <nav class="flex gap-1 overflow-x-auto border-b border-gray-200 dark:border-dark-700" :aria-label="t('enterprise.title')">
        <button v-for="tab in tabs" :key="tab" :data-test="`tab-${tab}`" :aria-current="activeTab === tab ? 'page' : undefined" class="shrink-0 border-b-2 px-4 py-3 text-sm font-medium transition-colors" :class="activeTab === tab ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-white'" @click="activeTab = tab">{{ t(`enterprise.${tab}`) }}</button>
      </nav>
      <section v-if="activeTab === 'overview'" class="space-y-5">
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard v-if="balanceVisible && balance !== null" data-test="enterprise-balance" :title="t('enterprise.balance')" :value="money(balance)" />
          <template v-if="adminEnterprise"><StatCard :title="t('enterprise.totalRecharged')" :value="money(adminEnterprise.total_recharged)" /><StatCard :title="t('enterprise.memberCount')" :value="adminEnterprise.member_count" /><StatCard :title="t('enterprise.departmentCount')" :value="adminEnterprise.department_count" /></template>
          <template v-if="overview"><StatCard :title="t('enterprise.todayCost')" :value="money(overview.stats.today_cost)" /><StatCard :title="t('enterprise.monthCost')" :value="money(overview.stats.month_cost)" /></template>
        </div>
        <div class="card flex flex-wrap items-center justify-between gap-4 p-5"><div><span class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.status') }}</span><p class="mt-1 font-medium text-gray-900 dark:text-gray-100">{{ t(enterprise.status === 'active' ? 'enterprise.active' : 'enterprise.disabled') }}</p></div><button v-if="admin" class="btn btn-primary" @click="balanceOpen = true">{{ t('enterprise.adjustBalance') }}</button></div>
        <EnterpriseQuotaDisplay v-if="enterpriseQuota" data-test="enterprise-quota" :quota="enterpriseQuota" :title="t('enterprise.enterpriseScope')" />
        <EnterpriseQuotaDisplay v-if="overview?.department_quota_visible && overview.department_quota" data-test="department-quota" :quota="overview.department_quota" :title="t('enterprise.departmentScope')" />
        <EnterpriseQuotaDisplay v-if="overview?.member_quota_visible && overview.member_quota" data-test="member-quota" :quota="overview.member_quota" :title="t('enterprise.memberScope')" />
        <p v-if="enterpriseQuota || overview?.member_quota || overview?.department_quota" class="text-sm text-gray-500 dark:text-gray-400">{{ t('enterprise.quotaHint') }}</p>
      </section>
      <EnterpriseOrganizationPanel v-else-if="activeTab === 'departments' || activeTab === 'members'" :key="activeTab" :mode="activeTab" :api="api" :admin="admin" :readonly="readonly" :overview="overview" :refresh-key="refreshKey" @updated="updated" />
      <EnterpriseQuotaPanel v-else-if="activeTab === 'quotas'" :api="api" :admin="admin" :readonly="readonly" :overview="overview" :refresh-key="refreshKey" @updated="updated" />
      <EnterpriseGroupsPanel v-else-if="activeTab === 'groups'" :api="api" :enterprise-id="enterprise.id" :admin="admin" :readonly="readonly" :overview="overview" :refresh-key="refreshKey" @updated="updated" />
      <EnterpriseUsagePanel v-else-if="activeTab === 'usage'" :api="api" :admin="admin" :overview="overview" :refresh-key="refreshKey" />
      <EnterpriseLogsPanel v-else-if="activeTab === 'audit' || activeTab === 'balanceLogs'" :key="activeTab" :api="api" :kind="activeTab" :refresh-key="refreshKey" />
      <div v-else-if="activeTab === 'settings'" class="space-y-5"><EnterpriseBasicsForm v-if="adminEnterprise" :key="adminEnterprise.updated_at" :enterprise="adminEnterprise" @updated="updated" /><EnterpriseSettingsPanel :api="api" :admin="admin" :enterprise-id="enterprise.id" :settings="adminEnterprise?.settings ?? overview?.settings" :readonly="readonly" :refresh-key="refreshKey" @updated="updated" /></div>
    </template>
    <BaseDialog :show="balanceOpen" :title="t('enterprise.adjustBalance')" @close="balanceOpen = false">
      <EnterpriseBalanceForm v-if="balanceOpen && enterpriseId" :enterprise-id="enterpriseId" @updated="balanceOpen = false; updated()" />
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { enterpriseAPI } from '@/api/enterprise'
import { enterprisesAPI } from '@/api/admin/enterprises'
import type { Enterprise, EnterpriseOverview } from '@/types/enterprise'
import StatCard from '@/components/common/StatCard.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EnterpriseOrganizationPanel from './components/EnterpriseOrganizationPanel.vue'
import EnterpriseQuotaDisplay from './components/EnterpriseQuotaDisplay.vue'
import EnterpriseQuotaPanel from './components/EnterpriseQuotaPanel.vue'
import EnterpriseGroupsPanel from './components/EnterpriseGroupsPanel.vue'
import EnterpriseUsagePanel from './components/EnterpriseUsagePanel.vue'
import EnterpriseLogsPanel from './components/EnterpriseLogsPanel.vue'
import EnterpriseSettingsPanel from './components/EnterpriseSettingsPanel.vue'
import EnterpriseBasicsForm from './components/EnterpriseBasicsForm.vue'
import EnterpriseBalanceForm from './components/EnterpriseBalanceForm.vue'
import { useEnterpriseAction } from './useEnterpriseAction'
const props = defineProps<{ admin?: boolean; enterpriseId?: number; overview?: EnterpriseOverview }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const { busy, error, read } = useEnterpriseAction()
const adminEnterprise = ref<Enterprise | null>(null)
const enterprise = computed(() => props.admin ? adminEnterprise.value : props.overview?.enterprise)
const api = computed(() => props.admin ? enterprisesAPI.forEnterprise(props.enterpriseId!) : enterpriseAPI)
const readonly = computed(() => !props.admin && (enterprise.value?.status !== 'active' || props.overview?.me.status !== 'active'))
const balanceVisible = computed(() => props.admin || props.overview?.balance_visible)
const balance = computed(() => props.admin ? adminEnterprise.value?.balance ?? null : props.overview?.balance ?? null)
const enterpriseQuota = computed(() => props.admin ? adminEnterprise.value?.quota : props.overview?.enterprise_quota)
const tabs = computed(() => {
  const permissions = props.overview?.permissions
  const result = ['overview', 'departments']
  if (props.admin || permissions?.view_members) result.push('members')
  if (props.admin || props.overview?.enterprise_quota || (props.overview?.department_quota_visible && props.overview.department_quota) || (props.overview?.member_quota_visible && props.overview.member_quota) || permissions?.edit_quotas || permissions?.edit_member_quota) result.push('quotas')
  result.push('groups', 'usage')
  if (props.admin) result.push('balanceLogs')
  if (props.admin || permissions?.view_audit_logs) result.push('audit')
  if (props.admin || permissions?.edit_settings) result.push('settings')
  return result
})
const activeTab = ref('overview')
const refreshKey = ref(0)
const money = (value: number) => `$${value.toFixed(4)}`
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
