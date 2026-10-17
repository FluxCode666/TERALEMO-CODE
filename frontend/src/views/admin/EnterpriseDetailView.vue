<template>
  <AppLayout>
    <div class="mb-4 flex justify-end"><button class="btn btn-danger" data-test="enterprise-delete-detail" @click="deleting = true">{{ t('admin.enterprises.delete') }}</button></div>
    <EnterpriseWorkspace :key="enterpriseId" admin :enterprise-id="enterpriseId" />
    <EnterpriseDeleteDialog v-if="deleting" :key="enterpriseId" :enterprise-id="enterpriseId" @cancel="deleting = false" @deleted="removed" />
  </AppLayout>
</template>
<script setup lang="ts">
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
