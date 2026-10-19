<template>
  <AppLayout compact-padding><div class="shadcn-scope min-w-0">
    <EnterpriseWorkspace :key="enterpriseId" admin :enterprise-id="enterpriseId">
      <template #header-actions><Button variant="destructive"  data-test="enterprise-delete-detail" @click="deleting = true">{{ t('admin.enterprises.delete') }}</Button></template>
    </EnterpriseWorkspace>
    <EnterpriseDeleteDialog v-if="deleting" :key="enterpriseId" :enterprise-id="enterpriseId" @cancel="deleting = false" @deleted="removed" />
  </div></AppLayout>
</template>
<script setup lang="ts">
import { Button } from '@/components/ui/button'

import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import EnterpriseWorkspace from '@/features/enterprise/EnterpriseWorkspace.vue'
import EnterpriseDeleteDialog from '@/features/enterprise/components/EnterpriseDeleteDialog.vue'
const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const deleting = ref(false)
const enterpriseId = computed(() => Number(route.params.id))
function removed() { deleting.value = false; void router.push('/admin/enterprises') }
</script>
