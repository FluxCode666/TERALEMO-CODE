<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check } from '@lucide/vue'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Checkbox } from '@/components/ui/checkbox'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { EnterpriseResourceAPI } from '@/api/enterprise'
import type { EnterpriseMember } from '@/types/enterprise'
import { useEnterpriseAction } from '../useEnterpriseAction'
const props = defineProps<{ api: EnterpriseResourceAPI; member: EnterpriseMember }>()
const emit = defineEmits<{ close: []; updated: [] }>()
const { t } = useI18n()
const { busy, error, run } = useEnterpriseAction()
const mode = ref('auto'), password = ref(''), confirmation = ref(''), validation = ref('')
const generated = ref(''), finished = ref(false), saved = ref(false), copied = ref(false)
const locked = computed(() => busy.value || !!generated.value && !saved.value)
function close() {
  if (locked.value) return
  generated.value = ''; password.value = ''; confirmation.value = ''
  emit('close')
}
async function submit() {
  if (finished.value) return
  validation.value = ''
  if (mode.value === 'manual' && (password.value.length < 8 || password.value !== confirmation.value)) {
    validation.value = t(password.value.length < 8 ? 'enterprise.passwordReset.minLength' : 'enterprise.passwordReset.mismatch'); return
  }
  const body = mode.value === 'manual' ? { password: password.value } : {}
  if (await run(async () => {
    const result = await props.api.resetMemberPassword(props.member.id, body)
    generated.value = result.generated_password || ''
    password.value = ''; confirmation.value = ''; finished.value = true
  })) emit('updated')
}
async function copy() {
  try { await navigator.clipboard.writeText(generated.value); copied.value = true }
  catch { validation.value = t('enterprise.passwordReset.copyFailed') }
}
</script>
<template>
  <Dialog :open="true" @update:open="open => { if (!open) close() }">
    <DialogContent class="shadcn-scope max-h-[90dvh] w-[calc(100%-2rem)] overflow-y-auto rounded-ui-lg motion-reduce:animate-none" :class="locked ? '[&>button:last-child]:hidden' : ''" @escape-key-down="event => { if (locked) event.preventDefault() }" @interact-outside="event => { if (locked) event.preventDefault() }">
      <DialogHeader><DialogTitle>{{ t('enterprise.passwordReset.title') }}</DialogTitle><DialogDescription>{{ member.username || member.email }} · {{ t('enterprise.passwordReset.sessions') }}</DialogDescription></DialogHeader>
      <form v-if="!finished" class="space-y-4" data-test="password-reset-form" @submit.prevent="submit">
        <RadioGroup v-model="mode" :disabled="busy" :aria-label="t('enterprise.passwordReset.method')">
          <Label v-for="method in ['auto', 'manual']" :key="method" class="flex items-center gap-3 rounded-ui-md border p-3"><RadioGroupItem :value="method" :data-test="`password-mode-${method}`" />{{ t(`enterprise.passwordReset.${method}`) }}</Label>
        </RadioGroup>
        <template v-if="mode === 'manual'">
          <Label class="block space-y-2"><span>{{ t('enterprise.passwordReset.newPassword') }}</span><Input v-model="password" type="password" autocomplete="new-password" :disabled="busy" data-test="reset-new-password" /></Label>
          <Label class="block space-y-2"><span>{{ t('enterprise.passwordReset.confirmPassword') }}</span><Input v-model="confirmation" type="password" autocomplete="new-password" :disabled="busy" data-test="reset-confirm-password" /></Label>
        </template>
        <p v-if="validation || error" role="alert" class="text-sm text-destructive">{{ validation || error }}</p>
        <Button :disabled="busy" data-test="password-reset-submit">{{ t('enterprise.passwordReset.submit') }}</Button>
      </form>
      <div v-else class="space-y-4" data-test="password-reset-result">
        <p role="status" class="text-sm">{{ t('enterprise.passwordReset.success') }}</p>
        <template v-if="generated">
          <p class="text-sm text-amber-700 dark:text-amber-400">{{ t('enterprise.passwordReset.once') }}</p>
          <div class="flex min-w-0 items-center gap-2 rounded-ui-md border bg-muted/40 p-3"><code class="min-w-0 flex-1 select-all break-all" data-test="generated-password">{{ generated }}</code><Button variant="outline" size="icon" :aria-label="t('enterprise.passwordReset.copy')" data-test="password-copy" @click="copy"><Check v-if="copied" class="size-4" /><Copy v-else class="size-4" /></Button></div>
          <p v-if="copied" role="status" class="text-sm text-muted-foreground">{{ t('enterprise.passwordReset.copied') }}</p>
          <p v-if="validation" role="alert" class="text-sm text-destructive">{{ validation }}</p>
          <Label class="flex items-center gap-2"><Checkbox v-model="saved" data-test="password-saved" />{{ t('enterprise.passwordReset.saved') }}</Label>
        </template>
        <Button :disabled="locked" data-test="password-result-close" @click="close">{{ t('enterprise.close') }}</Button>
      </div>
    </DialogContent>
  </Dialog>
</template>
