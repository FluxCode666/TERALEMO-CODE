<template>
  <div class="cs-field">
    <!-- 上传不可用（对象存储未开启 / 描述符 disabled）：不渲染 file input，也不响应拖拽（规范 §4.5） -->
    <div
      v-if="!available"
      class="cs-dropzone"
      data-state="unavailable"
      aria-disabled="true"
      :aria-describedby="hintId"
    >
      <CloudOff class="cs-dropzone-icon" aria-hidden="true" />
      <span class="cs-dropzone-title">{{ t('creationStudio.unavailable.uploads.title') }}</span>
      <span :id="hintId" class="cs-dropzone-hint">
        {{
          t(
            descriptor.required
              ? 'creationStudio.unavailable.uploads.required'
              : 'creationStudio.unavailable.uploads.description',
          )
        }}
      </span>
    </div>

    <template v-else>
      <div v-if="entries.length" class="cs-thumb-list">
        <div
          v-for="(entry, index) in entries"
          :key="entry.id"
          class="cs-thumb"
          :data-state="thumbState(entry)"
        >
          <img
            v-if="entry.status === 'ready' || entry.status === 'uploading' || entry.status === 'queued'"
            :src="thumbSrc(entry)"
            :alt="t(descriptor.labelKey)"
            @error="emit('expire', entry.id)"
          />
          <button
            v-else-if="entry.status === 'failed'"
            type="button"
            class="cs-thumb-error"
            :aria-label="t('creationStudio.upload.retryAria', { index: index + 1 })"
            @click="emit('retry', entry.id)"
          >
            <RotateCcw aria-hidden="true" />
            <span class="cs-thumb-error-text">{{ t('creationStudio.upload.retry') }}</span>
          </button>
          <button
            v-else
            type="button"
            class="cs-thumb-expired"
            :aria-label="t('creationStudio.upload.expiredAria', { index: index + 1 })"
            @click="openPicker('replace', entry.id)"
          >
            <Link2Off aria-hidden="true" />
            <span class="cs-thumb-expired-text">{{ t('creationStudio.upload.expired') }}</span>
          </button>
          <span
            v-if="entry.status === 'uploading' || entry.status === 'queued'"
            class="cs-thumb-progress"
            :style="{ '--cs-progress': `${entry.progress}%` }"
          />
          <span v-if="descriptor.maxCount === 1" class="cs-thumb-label">
            {{ t(descriptor.labelKey) }}
          </span>
          <button
            type="button"
            class="cs-thumb-remove"
            :aria-label="t('common.remove')"
            @click="emit('remove', entry.id)"
          >
            <X aria-hidden="true" />
          </button>
        </div>
      </div>

      <!-- 次数用完（429）时保留禁用入口而不是直接隐藏（规范 §4.5、§7.1） -->
      <div
        v-if="showDropzone"
        class="cs-dropzone"
        :class="{ 'cs-dropzone--tile': entries.length > 0 }"
        :data-dragover="dragover"
        :data-invalid="invalid"
        :aria-disabled="dropzoneDisabled ? 'true' : undefined"
        role="button"
        :tabindex="dropzoneDisabled ? -1 : 0"
        :aria-label="t('creationStudio.upload.dropzone')"
        :aria-describedby="describedBy"
        @click="openPicker('add')"
        @keydown.enter.prevent="openPicker('add')"
        @keydown.space.prevent="openPicker('add')"
        @dragover.prevent="onDragOver"
        @dragleave="dragover = false"
        @drop.prevent="onDrop"
      >
        <Upload class="cs-dropzone-icon" aria-hidden="true" />
        <span class="cs-dropzone-title">{{ t('creationStudio.upload.dropzone') }}</span>
        <span v-if="entries.length === 0" :id="hintId" class="cs-dropzone-hint">{{ hintText }}</span>
      </div>

      <p v-if="removedUnavailable" class="cs-field-warning">
        <TriangleAlert aria-hidden="true" />
        <span>{{ t('creationStudio.upload.removedUnavailable') }}</span>
      </p>

      <p v-if="hasExpired" :id="warningId" class="cs-field-warning">
        <TriangleAlert aria-hidden="true" />
        <span>
          {{ t('creationStudio.upload.expiredHint', { count: expiredCount }) }}
          <button type="button" class="cs-field-action" @click="openPicker('replace', firstExpiredId)">
            {{ t('creationStudio.upload.reupload') }}
          </button>
        </span>
      </p>

      <p v-for="error in displayErrors" :id="error.id" :key="error.id" class="cs-field-error">
        <CircleX aria-hidden="true" />
        <span>
          {{
            t('creationStudio.upload.fileError', {
              name: error.fileName ?? '',
              reason: t(`creationStudio.errors.${error.errorKey}`, error.params ?? {}),
            })
          }}
        </span>
      </p>
      <p v-if="moreErrors > 0" class="cs-field-error">
        <CircleX aria-hidden="true" />
        <span>{{ t('creationStudio.upload.moreFileErrors', { count: moreErrors }) }}</span>
      </p>

      <input
        ref="fileInput"
        type="file"
        class="hidden"
        :accept="descriptor.accept.join(',')"
        :multiple="descriptor.maxCount > 1"
        @change="onFileChange"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleX, CloudOff, Link2Off, RotateCcw, TriangleAlert, Upload, X } from '@lucide/vue'
import type { CreationParamImageDescriptor } from '../types'
import { acceptLabel, type CreationFieldError, type CreationImageEntry } from '../useCreationUploads'

const props = withDefaults(
  defineProps<{
    descriptor: CreationParamImageDescriptor
    entries: CreationImageEntry[]
    errors: CreationFieldError[]
    available: boolean
    uploading?: boolean
    limitReached?: boolean
    removedUnavailable?: boolean
  }>(),
  { uploading: false, limitReached: false, removedUnavailable: false },
)

const emit = defineEmits<{
  (e: 'pick', files: File[]): void
  (e: 'retry', id: string): void
  (e: 'replace', id: string, files: File[]): void
  (e: 'remove', id: string): void
  (e: 'expire', id: string): void
}>()

const { t } = useI18n()

const uid = `cs-upload-${Math.random().toString(36).slice(2, 9)}`
const hintId = `${uid}-hint`
const warningId = `${uid}-warning`

const fileInput = ref<HTMLInputElement | null>(null)
const dragover = ref(false)
const invalid = ref(false)
const pickerMode = ref<'add' | 'replace'>('add')
const replaceId = ref<string | null>(null)

const dropzoneDisabled = computed(() => props.limitReached)
const showDropzone = computed(
  () => props.available && props.entries.length < props.descriptor.maxCount,
)
const expiredEntries = computed(() => props.entries.filter(entry => entry.status === 'expired'))
const hasExpired = computed(() => expiredEntries.value.length > 0)
const expiredCount = computed(() => expiredEntries.value.length)
const firstExpiredId = computed(() => expiredEntries.value[0]?.id ?? '')
const displayErrors = computed(() => props.errors.slice(0, 3))
const moreErrors = computed(() => Math.max(0, props.errors.length - 3))
/** 上传区域要同时关联提示与字段内错误（规范 §7.1、§10）。 */
const describedBy = computed(() => [hintId, ...props.errors.map(error => error.id)].join(' '))
const hintText = computed(() => {
  const base = t('creationStudio.upload.limitHint', {
    types: acceptLabel(props.descriptor.accept),
    max: props.descriptor.maxSizeMB,
  })
  if (props.descriptor.maxCount <= 1) return base
  return `${base} · ${t('creationStudio.upload.maxFiles', { count: props.descriptor.maxCount })}`
})

function thumbState(entry: CreationImageEntry): 'error' | 'expired' | undefined {
  if (entry.status === 'failed') return 'error'
  if (entry.status === 'expired') return 'expired'
  return undefined
}

function thumbSrc(entry: CreationImageEntry): string | undefined {
  return entry.previewUrl ?? entry.url
}

function openPicker(mode: 'add' | 'replace', entryId?: string) {
  if (mode === 'add' && dropzoneDisabled.value) return
  pickerMode.value = mode
  replaceId.value = entryId ?? null
  const input = fileInput.value
  if (!input) return
  input.value = ''
  input.click()
}

function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const files = input.files ? Array.from(input.files) : []
  input.value = ''
  if (!files.length) return
  if (pickerMode.value === 'replace' && replaceId.value) {
    emit('replace', replaceId.value, files)
  } else {
    emit('pick', files)
  }
  pickerMode.value = 'add'
  replaceId.value = null
}

function onDragOver() {
  if (dropzoneDisabled.value) return
  dragover.value = true
}

function onDrop(event: DragEvent) {
  dragover.value = false
  if (dropzoneDisabled.value) return
  const files = event.dataTransfer?.files ? Array.from(event.dataTransfer.files) : []
  if (files.length) emit('pick', files)
}

// 校验类失败：上传框红 3 秒后自动复位（规范 §4.5）。
let invalidTimer: ReturnType<typeof setTimeout> | null = null
watch(
  () => props.errors.length,
  length => {
    if (length === 0) return
    invalid.value = true
    if (invalidTimer !== null) clearTimeout(invalidTimer)
    invalidTimer = setTimeout(() => {
      invalid.value = false
      invalidTimer = null
    }, 3000)
  },
)
</script>
