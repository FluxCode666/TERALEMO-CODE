<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download, Upload, Check, FileSpreadsheet } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseImportRow, EnterpriseImportPreview, EnterpriseImportResult } from '@/types/enterprise'
import { enterpriseErrorMessage } from '@/api/enterprise'
import { downloadCsv, downloadMemberTemplate, MemberImportError, parseMemberWorkbook, validateImportFile } from '../memberImport'
import EnterpriseDialog from './EnterpriseDialog.vue'
const props = defineProps<{ api: EnterpriseResourceAPI; limitedManager?: boolean }>()
const emit = defineEmits<{ close: []; imported: [] }>()
const { t } = useI18n()
const step = ref(1), busy = ref(false), error = ref(''), fileName = ref(''), dragging = ref(false)
const rows = ref<EnterpriseImportRow[]>([]), preview = ref<EnterpriseImportPreview>(), result = ref<EnterpriseImportResult>()
let alive = true
const notes = computed(() => [t('enterprise.importUi.rules'), t('enterprise.importUi.rolesHint'), t('enterprise.importUi.passwordHint'), t('enterprise.importUi.occupiedHint')])
const passwords = computed(() => result.value?.rows.filter(row => !!row.generated_password) ?? [])
const summary = computed(() => result.value ? Object.entries(result.value.summary) : preview.value ? Object.entries(preview.value.summary) : [])
function template(format: 'xlsx' | 'csv') { downloadMemberTemplate(format, notes.value, t('enterprise.importUi.membersSheet'), t('enterprise.importUi.notesSheet')) }
async function upload(file?: File) {
  if (!file || busy.value) return
  error.value = ''; preview.value = undefined; result.value = undefined; rows.value = []; fileName.value = ''
  busy.value = true
  try {
    validateImportFile(file)
    const parsed = parseMemberWorkbook(await file.arrayBuffer())
    if (!alive) return
    rows.value = parsed; fileName.value = file.name
  } catch (cause) { if (alive) error.value = cause instanceof MemberImportError ? t(`enterprise.importUi.${cause.key}`, { row: cause.row }) : t('enterprise.importUi.parseError') }
  finally { if (alive) busy.value = false }
}
function fileChange(event: Event) { const target = event.target as HTMLInputElement; void upload(target.files?.[0]); target.value = '' }
function drop(event: DragEvent) { dragging.value = false; if ((event.dataTransfer?.files.length ?? 0) > 1) { error.value = t('enterprise.importUi.oneFile'); return } void upload(event.dataTransfer?.files[0]) }
async function requestPreview() {
  if (busy.value || !rows.value.length) return
  busy.value = true; error.value = ''
  try { const value = await props.api.previewImport({ rows: rows.value }); if (alive) { preview.value = value; step.value = 3 } }
  catch (cause) { if (alive) error.value = enterpriseErrorMessage(cause, t) }
  finally { if (alive) busy.value = false }
}
async function commit() {
  if (busy.value || !preview.value || preview.value.summary.error > 0 || result.value) return
  busy.value = true; error.value = ''
  try {
    const value = await props.api.commitImport({ rows: rows.value })
    if (alive) { result.value = value; step.value = 4; rows.value = rows.value.map(row => ({ ...row, password: undefined })); emit('imported') }
  } catch (cause) { if (alive) error.value = enterpriseErrorMessage(cause, t) }
  finally { if (alive) busy.value = false }
}
function downloadErrors() {
  if (!preview.value) return
  downloadCsv('enterprise-import-errors.csv', [[t('enterprise.importUi.row'), t('enterprise.email'), t('enterprise.importUi.departmentPath'), t('enterprise.importUi.messages')], ...preview.value.rows.filter(row => row.status === 'error').map(row => [row.row, row.email, row.department_path, row.messages.join('; ')])])
}
function downloadPasswords() {
  if (!passwords.value.length) return
  downloadCsv('enterprise-initial-passwords.csv', [[t('enterprise.email'), t('enterprise.importUi.initialPassword')], ...passwords.value.map(row => [preview.value?.rows.find(source => source.row === row.row)?.email ?? String(row.row), row.generated_password!])])
}
onBeforeUnmount(() => { alive = false; rows.value = []; preview.value = undefined; result.value = undefined })
</script>
<template>
  <EnterpriseDialog :show="true" :title="t('enterprise.importUi.title')" width="wide" @close="() => { if (!busy) emit('close') }">
    <div class="shadcn-scope min-w-0 space-y-5" :aria-busy="busy">
      <ol class="grid grid-cols-2 gap-2 sm:grid-cols-4" :aria-label="t('enterprise.importUi.steps')"><li v-for="n in 4" :key="n" class="flex items-center gap-2 border-b-2 pb-2 text-xs" :class="step === n ? 'border-primary font-medium text-primary' : 'border-muted text-muted-foreground'" :aria-current="step === n ? 'step' : undefined"><span class="flex size-5 shrink-0 items-center justify-center rounded-full bg-muted"><Check v-if="step > n" class="size-3" /><template v-else>{{ n }}</template></span>{{ t(`enterprise.importUi.step${n}`) }}</li></ol>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
      <template v-if="step === 1"><div class="rounded-lg border bg-muted/30 p-4"><FileSpreadsheet class="mb-3 size-7 text-primary" /><p class="text-sm">{{ t('enterprise.importUi.templateHint') }}</p><p class="mt-2 text-xs text-muted-foreground">{{ t('enterprise.importUi.headers') }}</p><div class="mt-4 flex flex-wrap gap-2"><Button variant="outline" data-test="import-template-xlsx" @click="template('xlsx')"><Download class="mr-2 size-4" />XLSX</Button><Button variant="outline" data-test="import-template-csv" @click="template('csv')"><Download class="mr-2 size-4" />CSV</Button></div></div><ul class="list-disc space-y-2 pl-5 text-sm text-muted-foreground"><li v-for="note in notes" :key="note">{{ note }}</li><li v-if="limitedManager">{{ t('enterprise.importUi.managerHint') }}</li></ul><Button data-test="import-next" @click="step = 2">{{ t('enterprise.importUi.next') }}</Button></template>
      <template v-if="step === 2"><div class="rounded-lg border-2 border-dashed p-6 text-center" :class="dragging ? 'border-primary bg-primary/5' : ''" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="drop"><Upload class="mx-auto mb-3 size-8 text-muted-foreground" /><label for="enterprise-import-file" class="mb-3 block text-sm font-medium">{{ t('enterprise.importUi.uploadHint') }}</label><Input id="enterprise-import-file" type="file" accept=".xlsx,.csv" :disabled="busy" class="mx-auto max-w-sm" data-test="import-file" @change="fileChange" /><p class="mt-3 text-xs text-muted-foreground">{{ t('enterprise.importUi.fileLimits') }}</p></div><p v-if="fileName" role="status" class="break-all text-sm">{{ fileName }} · {{ t('enterprise.importUi.parsed', { count: rows.length }) }}</p><div class="flex gap-2"><Button variant="outline" :disabled="busy" @click="step = 1">{{ t('enterprise.importUi.back') }}</Button><Button :disabled="busy || !rows.length" data-test="import-preview" @click="requestPreview">{{ t(busy ? 'enterprise.importUi.processing' : 'enterprise.importUi.preview') }}</Button></div></template>
      <template v-if="step >= 3">
        <div class="grid grid-cols-3 gap-2 sm:grid-cols-5"><div v-for="[key, value] in summary" :key="key" class="rounded-md border px-3 py-2"><p class="text-xs text-muted-foreground">{{ t(`enterprise.importUi.summary.${key}`) }}</p><p class="mt-1 text-xl font-semibold tabular-nums">{{ value }}</p></div></div>
        <div v-if="step === 3 && preview?.departments_to_create.length" class="rounded-md bg-muted/40 p-3"><p class="mb-2 text-sm font-medium">{{ t('enterprise.importUi.autoDepartments') }}</p><ul class="max-h-28 list-disc overflow-y-auto pl-5 text-xs"><li v-for="path in preview.departments_to_create" :key="path">{{ path }}</li></ul></div>
        <div v-if="step === 3" class="max-h-72 overflow-auto rounded-md border"><Table><TableHeader><TableRow><TableHead>{{ t('enterprise.importUi.row') }}</TableHead><TableHead>{{ t('enterprise.email') }}</TableHead><TableHead>{{ t('enterprise.importUi.departmentPath') }}</TableHead><TableHead>{{ t('enterprise.role') }}</TableHead><TableHead>{{ t('enterprise.status') }}</TableHead><TableHead>{{ t('enterprise.importUi.messages') }}</TableHead></TableRow></TableHeader><TableBody><TableRow v-for="row in preview?.rows" :key="row.row"><TableCell>{{ row.row }}</TableCell><TableCell>{{ row.email }}</TableCell><TableCell>{{ row.department_path || t('enterprise.orgTree.unassigned') }}</TableCell><TableCell>{{ t(`enterprise.roles.${row.role}`) }}</TableCell><TableCell><Badge :variant="row.status === 'error' ? 'destructive' : 'secondary'">{{ t(`enterprise.importUi.summary.${row.status}`) }}</Badge></TableCell><TableCell class="min-w-40">{{ row.messages.join('；') || '—' }}</TableCell></TableRow></TableBody></Table></div>
        <div v-if="step === 3" class="flex flex-wrap gap-2"><Button variant="outline" :disabled="busy" @click="step = 2; preview = undefined">{{ t('enterprise.importUi.reupload') }}</Button><Button v-if="preview?.summary.error" variant="outline" data-test="import-errors" @click="downloadErrors">{{ t('enterprise.importUi.downloadErrors') }}</Button><Button :disabled="busy || !!preview?.summary.error" data-test="import-commit" @click="commit">{{ t(busy ? 'enterprise.importUi.processing' : 'enterprise.importUi.confirm') }}</Button></div>
        <template v-if="step === 4"><p role="status" class="text-sm">{{ t('enterprise.importUi.completed') }}</p><div v-if="passwords.length" class="space-y-3 rounded-md border border-amber-300 bg-amber-50 p-4 text-amber-950 dark:bg-amber-950/30 dark:text-amber-200"><p class="text-sm">{{ t('enterprise.importUi.passwordWarning') }}</p><Button variant="outline" data-test="import-passwords" @click="downloadPasswords"><Download class="mr-2 size-4" />{{ t('enterprise.importUi.downloadPasswords') }}</Button></div><div class="max-h-60 overflow-auto rounded-md border"><Table><TableHeader><TableRow><TableHead>{{ t('enterprise.importUi.row') }}</TableHead><TableHead>{{ t('enterprise.status') }}</TableHead><TableHead>{{ t('enterprise.importUi.messages') }}</TableHead></TableRow></TableHeader><TableBody><TableRow v-for="row in result?.rows" :key="row.row"><TableCell>{{ row.row }}</TableCell><TableCell><Badge :variant="row.status === 'failed' ? 'destructive' : 'secondary'">{{ t(`enterprise.importUi.summary.${row.status}`) }}</Badge></TableCell><TableCell>{{ row.message || '—' }}</TableCell></TableRow></TableBody></Table></div><Button @click="emit('close')">{{ t('enterprise.importUi.done') }}</Button></template>
      </template>
    </div>
  </EnterpriseDialog>
</template>
