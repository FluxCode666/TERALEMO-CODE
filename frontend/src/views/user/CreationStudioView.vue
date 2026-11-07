<template>
  <AppLayout>
    <div class="cs-page">
      <!-- A 页头 -->
      <header class="cs-header">
        <div class="min-w-0">
          <h1 class="cs-title">{{ t('creationStudio.title') }}</h1>
          <p class="cs-description">{{ t('creationStudio.description') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
            <Toggle v-model="autoRefresh" />
            <span>{{ t('creationStudio.autoRefresh') }}</span>
          </label>
          <button type="button" class="btn btn-secondary" @click="refreshAll">
            {{ t('common.refresh') }}
          </button>
          <a class="btn btn-ghost btn-sm" href="/async-tasks">
            {{ t('creationStudio.action.openTaskList') }}
          </a>
        </div>
      </header>

      <!-- B 模式切换 -->
      <ModeSwitch
        :model-value="mode"
        :image-reason="options?.image.reason ?? null"
        :video-reason="options?.video.reason ?? null"
        @update:model-value="onModeChange"
      />

      <!-- 移动端「参数 / 结果」切换 -->
      <div class="cs-view-switch-bar">
        <div class="cs-segmented w-full" role="tablist" :aria-label="t('creationStudio.view.form')">
          <button
            type="button"
            class="cs-segmented-item flex-1 justify-center"
            role="tab"
            :aria-selected="mobileView === 'form'"
            @click="mobileView = 'form'"
          >
            {{ t('creationStudio.view.form') }}
          </button>
          <button
            type="button"
            class="cs-segmented-item flex-1 justify-center"
            role="tab"
            :aria-selected="mobileView === 'results'"
            @click="mobileView = 'results'"
          >
            {{ t('creationStudio.view.results') }}
            <span v-if="processingCount" class="cs-segmented-badge">{{ processingCount }}</span>
          </button>
        </div>
      </div>

      <div class="cs-layout">
        <!-- C 表单面板 -->
        <section
          class="card cs-form-panel"
          :class="[mobileView === 'form' ? '' : 'hidden md:flex', { 'cs-flash': flash }]"
        >
          <div class="cs-form-body">
            <!-- 加载中 -->
            <div v-if="keysLoading && !hasKeys" class="space-y-3" aria-busy="true">
              <div class="skeleton h-9 w-full" />
              <div class="skeleton h-9 w-full" />
              <div class="skeleton h-24 w-full" />
            </div>

            <!-- 无可用 Key -->
            <div v-else-if="!hasKeys" class="cs-empty cs-empty--compact">
              <img class="cs-illu" :src="setupRequired" alt="" aria-hidden="true" />
              <h2 class="cs-empty-title">{{ t('creationStudio.unavailable.noKey.title') }}</h2>
              <p class="cs-empty-desc">{{ t('creationStudio.unavailable.noKey.description') }}</p>
              <div class="cs-empty-actions">
                <a class="btn btn-primary" href="/keys">{{ t('creationStudio.action.createKey') }}</a>
              </div>
            </div>

            <template v-else>
              <KeyPicker
                :model-value="selectedId"
                :keys="keys"
                :loading="keysLoading"
                :failed="keysFailed"
                @update:model-value="onSelectKey"
                @retry="loadKeys()"
              />

              <!-- 当前模式不可用（对象存储关闭 / 分组不支持 / Key 不可用） -->
              <Callout
                v-if="modeUnavailable"
                tone="warning"
                :title="unavailableCopy.title"
                :body="unavailableCopy.body"
              >
                <template #actions>
                  <a v-if="isAdmin" class="cs-callout-link" href="/admin/settings">
                    {{ t('creationStudio.unavailable.imageStorage.contactAdmin') }}
                  </a>
                  <a v-else class="cs-callout-link" href="/keys">
                    {{ t('creationStudio.action.switchKey') }}
                  </a>
                </template>
              </Callout>

              <!-- 无可用模型 -->
              <div v-else-if="!modelsForMode.length" class="cs-empty cs-empty--compact">
                <img class="cs-illu" :src="setupRequired" alt="" aria-hidden="true" />
                <h2 class="cs-empty-title">{{ t('creationStudio.unavailable.noModel.title') }}</h2>
                <p class="cs-empty-desc">{{ t('creationStudio.unavailable.noModel.description') }}</p>
                <div class="cs-empty-actions">
                  <a class="btn btn-secondary" href="/keys">{{ t('creationStudio.action.switchKey') }}</a>
                </div>
              </div>

              <template v-else>
                <ModelPicker :model-value="modelId" :models="modelsForMode" @update:model-value="selectModel" />

                <PromptField
                  v-model="prompt"
                  :max="PROMPT_MAX_LENGTH"
                  :error="promptError"
                  input-id="creation-prompt"
                  @submit="onSubmit"
                />

                <!-- 非图片参数 -->
                <ParamField
                  v-for="descriptor in paramDescriptors"
                  :key="descriptor.key"
                  :descriptor="descriptor"
                  :model-value="params[descriptor.key]"
                  @update:model-value="setParam(descriptor.key, $event)"
                />

                <!-- 图片参数：上传不可用时多个字段合并成一个不可用框 -->
                <template v-if="!uploadsAvailable && imageDescriptors.length">
                  <div class="cs-field">
                    <div class="cs-field-head">
                      <span class="cs-field-label">{{ imageFieldLabels }}</span>
                    </div>
                    <ParamField :descriptor="imageDescriptors[0]" :images="imageBinding(imageDescriptors[0].key)" />
                  </div>
                </template>
                <template v-else>
                  <ParamField
                    v-for="descriptor in imageDescriptors"
                    :key="descriptor.key"
                    :descriptor="descriptor"
                    :images="imageBinding(descriptor.key)"
                    @pick="uploadsApi.pick(descriptor.key, $event, descriptor.maxCount)"
                    @retry="uploadsApi.retry"
                    @replace="(id: string, files: File[]) => uploadsApi.replace(id, files)"
                    @remove="uploadsApi.remove"
                    @expire="uploadsApi.markExpired"
                  />
                </template>
              </template>
            </template>
          </div>

          <!-- C5 + C6：费用与提交（移动端吸底） -->
          <div v-if="hasKeys && !modeUnavailable && modelsForMode.length" class="cs-form-footer">
            <CostSummary
              :estimate="estimate"
              :loading="estimateLoading"
              :balance-hidden="balanceHidden"
              :state-override="costStateOverride"
            />
            <Callout
              v-if="pricingMissing"
              tone="warning"
              :title="t('creationStudio.errors.pricingMissing')"
            />
            <Callout
              v-if="submitError && submitErrorKey"
              :tone="submitErrorTone"
              :title="submitErrorTitle"
              :body="submitErrorMessage"
            >
              <template #actions>
                <a
                  v-if="showRechargeLink"
                  class="cs-callout-link"
                  href="/purchase"
                >
                  {{ t('creationStudio.action.goRecharge') }}
                </a>
                <a v-if="showSwitchKeyLink" class="cs-callout-link" href="/keys">
                  {{ t('creationStudio.action.switchKey') }}
                </a>
                <span v-if="retryRemaining > 0" class="cs-callout-body">
                  {{ t('creationStudio.errors.rateLimitedCountdown', { retryAfter: retryRemaining }) }}
                </span>
                <button v-else type="button" class="cs-callout-link" @click="onSubmit">
                  {{ t('creationStudio.action.retry') }}
                </button>
              </template>
            </Callout>
            <SubmitAction
              :mode="mode"
              :busy="submitting"
              :disabled="submitDisabled"
              :reason="blockReason"
              @submit="onSubmit"
            />
          </div>
        </section>

        <!-- D 结果列 -->
        <div class="cs-results" :class="mobileView === 'results' ? '' : 'hidden md:block'">
          <div class="cs-sync" :data-state="syncState">
            <span class="cs-sync-dot" aria-hidden="true" />
            <span>{{ t(syncLabel) }}</span>
          </div>
          <HistoryFeed
            :items="items"
            :loading="feedLoading"
            :loading-more="loadingMore"
            :error="feedError"
            :filter="filter"
            :total="total"
            :has-more="items.length < total"
            @update:filter="setFilter"
            @load-more="loadMore()"
            @retry="loadFeed()"
            @reuse="onReuse"
            @refresh="onRefreshTask"
            @preview="onPreview"
            @apply-prompt="onApplyPrompt"
          />
        </div>
      </div>

      <Lightbox
        v-model:open="lightboxOpen"
        v-model:index="lightboxIndex"
        :items="lightboxItems"
        :title="t('creationStudio.action.preview')"
      />

      <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Toggle from '@/components/common/Toggle.vue'
import ModeSwitch from '@/features/creation-studio/components/ModeSwitch.vue'
import KeyPicker from '@/features/creation-studio/components/KeyPicker.vue'
import ModelPicker from '@/features/creation-studio/components/ModelPicker.vue'
import PromptField from '@/features/creation-studio/components/PromptField.vue'
import ParamField from '@/features/creation-studio/components/ParamField.vue'
import CostSummary from '@/features/creation-studio/components/CostSummary.vue'
import SubmitAction from '@/features/creation-studio/components/SubmitAction.vue'
import Callout from '@/features/creation-studio/components/Callout.vue'
import HistoryFeed from '@/features/creation-studio/components/HistoryFeed.vue'
import Lightbox from '@/features/creation-studio/components/Lightbox.vue'
import setupRequired from '@/assets/creation-studio/setup-required.svg'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useCreationKeys } from '@/features/creation-studio/useCreationKeys'
import { useCreationOptions } from '@/features/creation-studio/useCreationOptions'
import { PROMPT_MAX_LENGTH, useCreationForm } from '@/features/creation-studio/useCreationForm'
import { acceptLabel, useCreationUploads, type ImageFieldBinding } from '@/features/creation-studio/useCreationUploads'
import { useCreationEstimate } from '@/features/creation-studio/useCreationEstimate'
import { useCreationSubmit } from '@/features/creation-studio/useCreationSubmit'
import { useCreationFeed } from '@/features/creation-studio/useCreationFeed'
import { useCreationSnapshots } from '@/features/creation-studio/useCreationSnapshots'
import { errorKeyForReason, isUnavailableStateReason } from '@/features/creation-studio/errorKeys'
import type {
  CreationErrorKey,
  CreationEstimateRequest,
  CreationImageRole,
  CreationLightboxItem,
  CreationMode,
  CreationSnapshot,
  CreationSubmitRequest,
  CreationSubmitResponse,
  CreationTaskView,
  CreationUnavailableReason,
} from '@/features/creation-studio/types'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const autoRefresh = ref(true)
const mobileView = ref<'form' | 'results'>('form')
const flash = ref(false)
const liveMessage = ref('')

// ==================== Key / options ====================
const keysApi = useCreationKeys()
const { keys, loading: keysLoading, failed: keysFailed, selectedId, hasKeys, load: loadKeys, select: selectKey } = keysApi

const optionsApi = useCreationOptions()
const { options } = optionsApi

// ==================== 表单 ====================
const form = useCreationForm(options)
const {
  mode,
  modelId,
  prompt,
  params,
  modelsForMode,
  selectedModel,
  protocol,
  paramDescriptors,
  imageDescriptors,
  promptEmpty,
  promptTooLong,
  promptValid,
  paramsValid,
  selectModel,
  setMode,
  setParam,
  setPrompt,
  applyParams,
} = form

// ==================== 上传 ====================
const uploadsApi = useCreationUploads({
  limits: computed(() => options.value?.upload_limits),
  available: computed(() => options.value?.uploads.available ?? false),
  // 上传返回 CREATION_STORAGE_DISABLED 时重新拉 options，进入不可用态（R1-09）。
  onUnavailable: () => {
    const keyId = selectedId.value
    if (keyId) void optionsApi.refresh(keyId)
  },
})
const {
  uploading: uploadsUploading,
  failed: uploadsFailed,
  expired: uploadsExpired,
  imageRefs,
} = uploadsApi

// ==================== 快照 / 结果流 ====================
const snapshots = useCreationSnapshots(computed(() => authStore.user?.id ?? 0))

const feedApi = useCreationFeed({
  snapshotFor: (catalogId, taskId) => snapshots.find(catalogId, taskId),
  autoRefresh,
})
const {
  items,
  loading: feedLoading,
  loadingMore,
  error: feedError,
  filter,
  total,
  load: loadFeed,
  loadMore,
  insert: insertTask,
  setFilter,
} = feedApi

// ==================== 估算 / 提交 ====================
const estimateApi = useCreationEstimate(buildEstimateRequest)
const { estimate, loading: estimateLoading, balanceHidden, canSubmit: estimateCanSubmit, error: estimateError } = estimateApi

const submitApi = useCreationSubmit({
  buildRequest: buildSubmitRequest,
  onSuccess: onSubmitted,
  onUploadExpired: () => uploadsApi.markExpiredFromServer(),
})
const {
  submitting,
  error: submitError,
  errorKey: submitErrorKey,
  errorRetryAfter,
  submit: submitTask,
  clearError: clearSubmitError,
} = submitApi

// ==================== 派生状态 ====================
const isAdmin = computed(() => authStore.user?.role === 'admin')
const uploadsAvailable = computed(() => options.value?.uploads.available ?? false)
const modeAvailability = computed(() => (mode.value === 'image' ? options.value?.image : options.value?.video))
const modeUnavailable = computed(() => Boolean(modeAvailability.value && !modeAvailability.value.available))

const unavailableCopy = computed<{ title: string; body: string }>(() => {
  const reason: CreationUnavailableReason | undefined = modeAvailability.value?.reason
  switch (reason) {
    case 'storage_disabled':
      return {
        title: t('creationStudio.unavailable.imageStorage.title'),
        body: t('creationStudio.unavailable.imageStorage.description'),
      }
    case 'key_unavailable':
      return { title: t('creationStudio.errors.keyUnavailable'), body: t('creationStudio.errors.keyUnavailable') }
    case 'no_accounts':
      return {
        title: t('creationStudio.unavailable.noModel.title'),
        body: t('creationStudio.unavailable.noModel.description'),
      }
    case 'permission_denied':
      return { title: t('creationStudio.errors.imageForbidden'), body: t('creationStudio.errors.imageForbidden') }
    default:
      return { title: t('creationStudio.mode.unsupported'), body: t('creationStudio.mode.unsupported') }
  }
})

const imageFieldLabels = computed(() => imageDescriptors.value.map(item => t(item.labelKey)).join(' / '))

const promptError = computed(() => {
  if (promptEmpty.value) return t('creationStudio.submit.blocked.promptEmpty')
  if (promptTooLong.value) return t('creationStudio.submit.blocked.promptTooLong')
  return ''
})

const processingCount = computed(
  () => items.value.filter(item => item.status === 'processing' || item.status === 'queued').length,
)

const blockReason = computed(() => {
  if (!hasKeys.value) return t('creationStudio.submit.blocked.noKey')
  if (!selectedModel.value) return t('creationStudio.submit.blocked.noModel')
  if (promptEmpty.value) return t('creationStudio.submit.blocked.promptEmpty')
  if (promptTooLong.value) return t('creationStudio.submit.blocked.promptTooLong')
  if (!paramsValid.value) return t('creationStudio.submit.blocked.paramsInvalid')
  if (uploadsUploading.value) return t('creationStudio.submit.blocked.refImageUploading')
  if (uploadsFailed.value) return t('creationStudio.submit.blocked.refImageFailed')
  if (uploadsExpired.value) return t('creationStudio.submit.blocked.refImageExpired')
  if (!estimateCanSubmit.value) {
    return estimate.value?.priced === false
      ? t('creationStudio.errors.pricingMissing')
      : t('creationStudio.errors.balanceInsufficient')
  }
  return ''
})

const submitDisabled = computed(() => Boolean(blockReason.value))

const submitErrorParams = computed(() => {
  const metadata = submitError.value?.metadata ?? {}
  const limits = options.value?.upload_limits
  const maxBytes = Number(metadata.max_bytes ?? limits?.max_bytes ?? 0)
  return {
    max: maxBytes > 0 ? (maxBytes / (1024 * 1024)).toFixed(1) : '',
    maxSide: metadata.max_side_px ?? String(limits?.max_side_px ?? ''),
    maxPixels: limits?.max_pixels ?? '',
    types: acceptLabel(limits?.accept ?? []),
    retryAfter: submitRetryAfter.value,
  }
})

const submitRetryAfter = computed(() => errorRetryAfter.value)

/** retry_after 倒计时（R1-07）：按接收时刻算 deadline，归零后恢复重试按钮。 */
const retryRemaining = ref(0)
let retryTimer: ReturnType<typeof setInterval> | null = null

function stopRetryTimer() {
  if (retryTimer !== null) {
    clearInterval(retryTimer)
    retryTimer = null
  }
}

watch(errorRetryAfter, seconds => {
  stopRetryTimer()
  retryRemaining.value = seconds
  if (!seconds) return
  const deadline = Date.now() + seconds * 1000
  retryTimer = setInterval(() => {
    const left = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
    retryRemaining.value = left
    if (left === 0) stopRetryTimer()
  }, 1000)
})

const submitErrorTitle = computed(() => {
  if (!submitError.value) return ''
  const key: CreationErrorKey = submitErrorKey.value ?? errorKeyForReason(submitError.value.reason, submitError.value.status)
  return t(`creationStudio.errors.${key}`, submitErrorParams.value)
})

const submitErrorMessage = computed(() => submitError.value?.message ?? '')

/** 未定价：无论是否刚提交过，都要给 pricingMissing 的 warning（R1-05）。 */
const pricingMissing = computed(() => Boolean(estimate.value && !estimate.value.priced))

/** 提交余额类错误时费用区切 insufficient（R1-06）。 */
const costStateOverride = computed<'insufficient' | null>(() =>
  ['balanceInsufficient', 'balanceLow', 'enterpriseBalance'].includes(submitErrorKey.value ?? '')
    ? 'insufficient'
    : null,
)

const WARNING_KEYS: CreationErrorKey[] = [
  'modelUnavailable',
  'pricingMissing',
  'imageForbidden',
  'groupUnavailable',
  'keyUnavailable',
  'keyNotFound',
  'subscriptionMissing',
  'ipRestricted',
  'rateLimited',
  'quotaExceeded',
  'keyQuota',
  'enterpriseQuota',
  'uploadLimit',
  'uploadExpired',
  'uploadTooLarge',
  'uploadUnsupportedType',
  'uploadInvalidImage',
  'noCapacity',
]

/** 这两个 reason 即使没有对应 errors key，也按规范 §7.1 用 warning 色调。 */
const WARNING_REASONS = new Set(['CREATION_STORAGE_DISABLED', 'ASYNC_IMAGE_DISABLED'])

const submitErrorTone = computed<'warning' | 'danger'>(() => {
  if (submitError.value && WARNING_REASONS.has(submitError.value.reason)) return 'warning'
  const key = submitErrorKey.value
  return key && WARNING_KEYS.includes(key) ? 'warning' : 'danger'
})

const showRechargeLink = computed(
  () => submitErrorKey.value === 'balanceInsufficient' || submitErrorKey.value === 'balanceLow',
)
const showSwitchKeyLink = computed(() =>
  ['keyUnavailable', 'keyQuota', 'keyNotFound', 'groupUnavailable'].includes(submitErrorKey.value ?? ''),
)

const syncState = computed(() => {
  if (!autoRefresh.value) return 'paused'
  if (feedError.value) return 'error'
  return 'live'
})
const syncLabel = computed(() => {
  if (syncState.value === 'paused') return 'creationStudio.feed.syncPaused'
  if (syncState.value === 'error') return 'creationStudio.feed.syncError'
  return 'creationStudio.feed.syncLive'
})

// ==================== 灯箱 ====================
const lightboxOpen = ref(false)
const lightboxIndex = ref(0)
const lightboxItems = ref<CreationLightboxItem[]>([])

// ==================== 请求构造 ====================
function buildEstimateRequest(): CreationEstimateRequest | null {
  const keyId = selectedId.value
  const model = selectedModel.value
  if (!keyId || !model || !protocol.value) return null
  const body: CreationEstimateRequest = {
    api_key_id: keyId,
    mode: model.mode,
    model: model.model,
    protocol: model.protocol,
    params: { ...params.value },
  }
  if (imageRefs.value.length) body.images = [...imageRefs.value]
  return body
}

function buildSubmitRequest(): CreationSubmitRequest | null {
  const base = buildEstimateRequest()
  if (!base || !promptValid.value) return null
  const body: CreationSubmitRequest = {
    api_key_id: base.api_key_id,
    mode: base.mode,
    model: base.model,
    protocol: base.protocol,
    prompt: prompt.value.trim(),
    params: { ...base.params },
  }
  if (base.images?.length) body.images = base.images
  return body
}

function onSubmitted(response: CreationSubmitResponse, request: CreationSubmitRequest) {
  const snapshot: CreationSnapshot = {
    catalog_id: response.catalog_id,
    task_id: response.task_id,
    mode: response.mode,
    api_key_id: request.api_key_id,
    protocol: response.protocol,
    model: response.model,
    prompt: request.prompt,
    params: { ...request.params },
    images: uploadsApi.entries.value
      .filter(entry => entry.status === 'ready' && entry.uploadId && entry.url)
      .map(entry => ({
        role: entry.role,
        upload_id: entry.uploadId as string,
        url: entry.url as string,
        expires_at: entry.expiresAt ?? 0,
        width: entry.width ?? 0,
        height: entry.height ?? 0,
      })),
    created_at: response.created_at,
  }
  snapshots.add(snapshot)
  insertTask({
    catalogId: response.catalog_id,
    taskId: response.task_id,
    kind: response.mode,
    protocol: response.protocol,
    model: response.model,
    status: 'processing',
    prompt: snapshot.prompt,
    params: snapshot.params,
    imageCount: Number(request.params.n ?? 1),
    urls: [],
    error: '',
    createdAt: response.created_at,
    completedAt: 0,
    expiresAt: response.expires_at,
    snapshot,
    fresh: true,
  })
  appStore.showToast('success', t('creationStudio.submit.success'))
  if (mobileView.value === 'form' && window.matchMedia('(max-width: 767.98px)').matches) {
    mobileView.value = 'results'
  }
  estimateApi.refresh()
}

// ==================== 交互 ====================
function onModeChange(next: CreationMode) {
  setMode(next)
}

function onSelectKey(value: number | null) {
  selectKey(value)
}

async function onSubmit() {
  if (submitDisabled.value || submitting.value) return
  clearSubmitError()
  await submitTask()
}

function onApplyPrompt(value: string) {
  setPrompt(value)
  if (window.matchMedia('(max-width: 767.98px)').matches) mobileView.value = 'form'
}

function onPreview(payload: CreationLightboxItem[], index: number) {
  lightboxItems.value = payload
  lightboxIndex.value = index
  lightboxOpen.value = true
}

async function onRefreshTask(task: CreationTaskView) {
  if (!task.catalogId) return
  await feedApi.refreshLoaded()
}

function onReuse(task: CreationTaskView) {
  const snapshot = task.snapshot
  flash.value = true
  setTimeout(() => {
    flash.value = false
  }, 1200)
  if (window.matchMedia('(max-width: 767.98px)').matches) mobileView.value = 'form'

  if (!snapshot) {
    // 没有本地记录：只回填模型与默认参数（契约 §8.2 第 5 条）。
    setMode(task.kind)
    selectModel(`${task.kind}:${task.protocol}:${task.model}`)
    appStore.showToast('info', t('creationStudio.reuse.noSnapshot'))
    return
  }

  setMode(snapshot.mode)
  selectModel(`${snapshot.mode}:${snapshot.protocol}:${snapshot.model}`)
  setPrompt(snapshot.prompt)
  applyParams(snapshot.params)

  const keyExists = keys.value.some(item => item.id === snapshot.api_key_id)
  if (keyExists) selectKey(snapshot.api_key_id)

  uploadsApi.reset()
  let expiredCount = 0
  if (uploadsAvailable.value && snapshot.images.length) {
    uploadsApi.restore(snapshot.images)
    const now = Math.floor(Date.now() / 1000)
    expiredCount = snapshot.images.filter(image => image.expires_at - now < 900).length
  }

  if (!keyExists) appStore.showToast('warning', t('creationStudio.reuse.keyMissing'))
  else if (expiredCount > 0) appStore.showToast('warning', t('creationStudio.reuse.refImagesExpired', { count: expiredCount }))
  else if (!uploadsAvailable.value && snapshot.images.length) appStore.showToast('warning', t('creationStudio.reuse.partial'))
  else appStore.showToast('success', t('creationStudio.reuse.applied'))
}

function imageBinding(role: CreationImageRole): ImageFieldBinding {
  return {
    entries: uploadsApi.entriesFor(role),
    errors: uploadsApi.errorsFor(role),
    available: uploadsAvailable.value,
    uploading: uploadsUploading.value,
    limitReached: uploadsApi.limitReached.value,
    removedUnavailable: uploadsApi.removedUnavailable.value,
  }
}

function refreshAll() {
  void loadKeys()
  const keyId = selectedId.value
  if (keyId) void optionsApi.refresh(keyId)
  void loadFeed()
  estimateApi.refresh()
}

// ==================== 副作用 ====================
watch(
  selectedId,
  keyId => {
    uploadsApi.reset()
    if (keyId) void optionsApi.load(keyId)
  },
  { immediate: true },
)

watch(uploadsAvailable, available => {
  if (!available) uploadsApi.pruneUnavailable()
})

watch(estimateError, error => {
  if (!error) return
  // 估算错误只做静默刷新，不出提交 callout（规范 §7.2 末段、R1-20）。
  if (error.reason === 'CREATION_MODEL_NOT_AVAILABLE') {
    const keyId = selectedId.value
    if (keyId) void optionsApi.refresh(keyId)
  } else if (error.reason === 'CREATION_KEY_NOT_FOUND') {
    void loadKeys()
  }
})

watch(submitError, error => {
  if (!error) return
  if (isUnavailableStateReason(error.reason) || error.reason === 'CREATION_MODEL_NOT_AVAILABLE') {
    const keyId = selectedId.value
    if (keyId) void optionsApi.refresh(keyId)
  }
  if (['CREATION_KEY_NOT_FOUND', 'API_KEY_EXPIRED', 'API_KEY_DISABLED'].includes(error.reason)) {
    void loadKeys()
  }
})

const previousStatus = new Map<string, string>()
watch(items, list => {
  for (const task of list) {
    const id = task.catalogId || task.taskId
    const previous = previousStatus.get(id)
    if (previous && previous !== task.status) {
      if (task.status === 'completed') liveMessage.value = t('creationStudio.live.taskCompleted')
      if (task.status === 'failed') liveMessage.value = t('creationStudio.live.taskFailed', { message: task.error })
    }
    previousStatus.set(id, task.status)
  }
})

watch(uploadsExpired, expiredNow => {
  if (expiredNow) liveMessage.value = t('creationStudio.live.refImageExpired')
})

onMounted(() => {
  void loadKeys()
  void loadFeed()
})

onUnmounted(() => {
  stopRetryTimer()
  optionsApi.dispose()
})
</script>
