<template>
  <div class="rounded-lg border border-gray-200 dark:border-dark-600">
    <div
      class="flex flex-wrap items-center gap-1 border-b border-gray-200 p-2 dark:border-dark-600"
      data-test="editor-toolbar"
    >
      <select
        class="input !w-auto !py-1 text-sm"
        :value="headingValue"
        :title="t('admin.massEmails.editor.heading')"
        @change="onHeadingChange"
      >
        <option value="0">{{ t('admin.massEmails.editor.paragraph') }}</option>
        <option value="1">H1</option>
        <option value="2">H2</option>
        <option value="3">H3</option>
      </select>
      <button type="button" :class="btnClass(editor?.isActive('bold'))" :title="t('admin.massEmails.editor.bold')" @click="editor?.chain().focus().toggleBold().run()"><b>B</b></button>
      <button type="button" :class="btnClass(editor?.isActive('italic'))" :title="t('admin.massEmails.editor.italic')" @click="editor?.chain().focus().toggleItalic().run()"><i>I</i></button>
      <button type="button" :class="btnClass(editor?.isActive('bulletList'))" :title="t('admin.massEmails.editor.bulletList')" @click="editor?.chain().focus().toggleBulletList().run()">• {{ t('admin.massEmails.editor.bulletList') }}</button>
      <button type="button" :class="btnClass(editor?.isActive('orderedList'))" :title="t('admin.massEmails.editor.orderedList')" @click="editor?.chain().focus().toggleOrderedList().run()">1. {{ t('admin.massEmails.editor.orderedList') }}</button>
      <button type="button" :class="btnClass(editor?.isActive('link'))" :title="t('admin.massEmails.editor.link')" @click="setLink">{{ t('admin.massEmails.editor.link') }}</button>
      <button type="button" :class="btnClass(false)" :title="t('admin.massEmails.editor.clearFormat')" @click="editor?.chain().focus().unsetAllMarks().clearNodes().run()">{{ t('admin.massEmails.editor.clearFormat') }}</button>
      <span class="mx-1 h-5 w-px bg-gray-200 dark:bg-dark-600"></span>
      <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.massEmails.form.insertVariable') }}:</span>
      <button
        v-for="v in MASS_EMAIL_VARIABLES"
        :key="v"
        type="button"
        :class="btnClass(false)"
        :data-test="`insert-var-${v}`"
        @click="insertVariable(v)"
      >
        {{ variableToken(v) }}
      </button>
    </div>
    <EditorContent :editor="editor" class="mass-email-editor" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Editor, EditorContent } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import { MASS_EMAIL_VARIABLES } from '@/views/admin/massEmails/recipientForm'

const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const { t } = useI18n()

function variableToken(name: string): string {
  return `{{${name}}}`
}

const editor = new Editor({
  content: props.modelValue,
  extensions: [StarterKit.configure({ link: { openOnClick: false } })],
  editorProps: {
    attributes: { class: 'prose prose-sm max-w-none min-h-[240px] p-3 focus:outline-none dark:prose-invert' }
  },
  onUpdate: ({ editor: e }) => {
    emit('update:modelValue', e.isEmpty ? '' : e.getHTML())
  }
})

watch(
  () => props.modelValue,
  (value) => {
    if (value !== editor.getHTML() && !(editor.isEmpty && !value)) {
      editor.commands.setContent(value || '', { emitUpdate: false })
    }
  }
)

onBeforeUnmount(() => editor.destroy())

const headingValue = computed(() => {
  for (const level of [1, 2, 3]) {
    if (editor.isActive('heading', { level })) return String(level)
  }
  return '0'
})

function btnClass(active?: boolean) {
  return [
    'rounded px-2 py-1 text-sm transition-colors',
    active
      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
      : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-600'
  ]
}

function onHeadingChange(e: Event) {
  const level = Number((e.target as HTMLSelectElement).value)
  const chain = editor.chain().focus()
  if (level === 0) chain.setParagraph().run()
  else chain.toggleHeading({ level: level as 1 | 2 | 3 }).run()
}

function setLink() {
  const prev = (editor.getAttributes('link').href as string | undefined) || ''
  const url = window.prompt(t('admin.massEmails.editor.linkPrompt'), prev)
  if (url === null) return
  if (url.trim() === '') {
    editor.chain().focus().extendMarkRange('link').unsetLink().run()
    return
  }
  editor.chain().focus().extendMarkRange('link').setLink({ href: url.trim() }).run()
}

function insertVariable(name: string) {
  editor.chain().focus().insertContent(variableToken(name)).run()
}
</script>

<style scoped>
.mass-email-editor :deep(.ProseMirror) {
  min-height: 240px;
}
.mass-email-editor :deep(.ProseMirror ul) {
  list-style: disc;
  padding-left: 1.5rem;
}
.mass-email-editor :deep(.ProseMirror ol) {
  list-style: decimal;
  padding-left: 1.5rem;
}
.mass-email-editor :deep(.ProseMirror a) {
  color: #2563eb;
  text-decoration: underline;
}
.mass-email-editor :deep(.ProseMirror h1) {
  font-size: 1.5rem;
  font-weight: 700;
}
.mass-email-editor :deep(.ProseMirror h2) {
  font-size: 1.25rem;
  font-weight: 600;
}
.mass-email-editor :deep(.ProseMirror h3) {
  font-size: 1.1rem;
  font-weight: 600;
}
</style>
