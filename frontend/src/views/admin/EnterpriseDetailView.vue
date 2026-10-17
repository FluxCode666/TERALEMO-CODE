<template><AppLayout><EnterpriseWorkspace :key="enterpriseId" admin :enterprise-id="enterpriseId" /></AppLayout></template>
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import EnterpriseWorkspace from '@/features/enterprise/EnterpriseWorkspace.vue'
const route = useRoute()
const enterpriseId = computed(() => Number(route.params.id))
</script>
