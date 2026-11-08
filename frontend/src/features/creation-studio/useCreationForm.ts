/**
 * F7：模式、模型、提示词、参数。
 *
 * - 参数一律由描述符驱动（`params` 用描述符 `default` 初始化），不按模型名写死。
 * - 切换模型保留提示词、按新模型重建参数；切换模式只换表单。
 * - 未知 `kind` 只告警并跳过，不抛错（后端将来新增 kind 时不至于把页面炸掉）。
 */
import { computed, ref, watch, type Ref } from 'vue'
import type {
  CreationMode,
  CreationModelOption,
  CreationOptionsResponse,
  CreationParamDescriptor,
  CreationParamImageDescriptor,
  CreationParamValues,
  CreationProtocol,
} from './types'

/** 契约 §5.2 V3：提示词按 rune 计不超过 8000。 */
export const PROMPT_MAX_LENGTH = 8000

/** 契约 §3.2：`kind` 只有四种；未知 kind 一律跳过（F7、R1-02）。 */
const KNOWN_KINDS = new Set<string>(['select', 'integer', 'ratio', 'image'])

export function isKnownParamKind(kind: string): boolean {
  return KNOWN_KINDS.has(kind)
}

/**
 * 严格整数解析（契约 §5.2 V5）：`2junk`、`1.5`、NaN 都不算整数，
 * 不能像 `parseInt` 那样截断后当成合法值。
 */
function strictInteger(value: unknown): number | null {
  if (typeof value === 'number') return Number.isInteger(value) ? value : null
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!/^-?\d+$/.test(trimmed)) return null
    const parsed = Number(trimmed)
    return Number.isSafeInteger(parsed) ? parsed : null
  }
  return null
}

/** 参数内容是否一致：避免 options 刷新时重建同值的 params 对象，
 * 否则会触发一次多余的估算（模型持续 400 时会变成刷新→估算的死循环）。 */
function sameParams(left: CreationParamValues, right: CreationParamValues): boolean {
  const leftKeys = Object.keys(left)
  if (leftKeys.length !== Object.keys(right).length) return false
  return leftKeys.every(key => left[key] === right[key])
}

function descriptorDefaults(model: CreationModelOption | null): CreationParamValues {
  const next: CreationParamValues = {}
  if (!model) return next
  for (const descriptor of model.params) {
    switch (descriptor.kind) {
      case 'select':
      case 'ratio':
      case 'integer':
        next[descriptor.key] = descriptor.default
        break
      case 'image':
        // 图片字段由 useCreationUploads 维护，不进 params。
        break
      default: {
        const unknown = descriptor as { kind?: string; key?: string }
        console.warn(`[creation-studio] unknown param kind "${unknown.kind}" for "${unknown.key}", skipped`)
      }
    }
  }
  return next
}

/** 描述符级别的取值校验（契约 §5.2 V5）。 */
export function isParamValueValid(descriptor: CreationParamDescriptor, value: unknown): boolean {
  // 未知 kind 不参与校验（渲染层同样跳过），否则会让表单永远不合法。
  if (!isKnownParamKind(descriptor.kind)) return true
  if (descriptor.kind === 'image') return true
  if (value === undefined || value === null || value === '') return false
  if (descriptor.kind === 'select') {
    const text = String(value)
    return descriptor.options.some(option => option.value === text && !option.disabled)
  }
  if (descriptor.kind === 'ratio') {
    const text = String(value)
    return descriptor.options.some(option => option.value === text)
  }
  const numeric = strictInteger(value)
  if (numeric === null) return false
  if (numeric < descriptor.min || numeric > descriptor.max) return false
  const step = descriptor.step > 0 ? descriptor.step : 1
  return (numeric - descriptor.min) % step === 0
}

export function useCreationForm(options: Ref<CreationOptionsResponse | null>) {
  const mode = ref<CreationMode>('image')
  const modelId = ref('')
  const prompt = ref('')
  const params = ref<CreationParamValues>({})

  const allModels = computed(() => options.value?.models ?? [])
  const modelsForMode = computed(() => allModels.value.filter(model => model.mode === mode.value))
  const selectedModel = computed(() => allModels.value.find(model => model.id === modelId.value) ?? null)
  const protocol = computed<CreationProtocol | ''>(() => selectedModel.value?.protocol ?? '')
  const paramDescriptors = computed(() =>
    (selectedModel.value?.params ?? []).filter(
      (descriptor): descriptor is Exclude<CreationParamDescriptor, CreationParamImageDescriptor> =>
        descriptor.kind !== 'image' && isKnownParamKind(descriptor.kind),
    ),
  )
  const imageDescriptors = computed(() =>
    (selectedModel.value?.params ?? []).filter(
      (descriptor): descriptor is CreationParamImageDescriptor => descriptor.kind === 'image',
    ),
  )

  const promptLength = computed(() => [...prompt.value].length)
  const promptEmpty = computed(() => prompt.value.trim() === '')
  const promptTooLong = computed(() => promptLength.value > PROMPT_MAX_LENGTH)

  const invalidParamKeys = computed(() =>
    paramDescriptors.value
      .filter(descriptor => !isParamValueValid(descriptor, params.value[descriptor.key]))
      .map(descriptor => descriptor.key),
  )
  const paramsValid = computed(() => invalidParamKeys.value.length === 0)
  const promptValid = computed(() => !promptEmpty.value && !promptTooLong.value)

  /** 只保留当前模型认识且合法的值，其余丢弃（契约 §8.2 第 3 条）。 */
  function ensureParams(model: CreationModelOption | null) {
    const defaults = descriptorDefaults(model)
    const merged: CreationParamValues = { ...defaults }
    if (model) {
      for (const descriptor of model.params) {
        if (descriptor.kind === 'image' || !isKnownParamKind(descriptor.kind)) continue
        const existing = params.value[descriptor.key]
        if (existing !== undefined && isParamValueValid(descriptor, existing)) {
          merged[descriptor.key] = existing
        }
      }
    }
    if (!sameParams(params.value, merged)) params.value = merged
  }

  function selectModel(nextModelId: string) {
    if (nextModelId === modelId.value) return
    const model = allModels.value.find(entry => entry.id === nextModelId) ?? null
    modelId.value = model?.id ?? ''
    if (model) mode.value = model.mode
    ensureParams(model)
  }

  function setMode(nextMode: CreationMode) {
    if (nextMode === mode.value) return
    mode.value = nextMode
    const first = allModels.value.find(model => model.mode === nextMode) ?? null
    modelId.value = first?.id ?? ''
    ensureParams(first)
  }

  function setParam(key: string, value: string | number) {
    params.value = { ...params.value, [key]: value }
  }

  function setPrompt(value: string) {
    prompt.value = value
  }

  function resetParams() {
    params.value = descriptorDefaults(selectedModel.value)
  }

  /** 复用参数：能回填的回填，不合法的丢弃（规范 §4.11）。 */
  function applyParams(values: CreationParamValues) {
    const next = descriptorDefaults(selectedModel.value)
    for (const descriptor of selectedModel.value?.params ?? []) {
      if (descriptor.kind === 'image' || !isKnownParamKind(descriptor.kind)) continue
      const incoming = values[descriptor.key]
      if (incoming !== undefined && isParamValueValid(descriptor, incoming)) {
        next[descriptor.key] = incoming
      }
    }
    params.value = next
  }

  // options 变化（切 Key、刷新能力）时对账当前模型；模型没了就清空，不拿旧模型去提交。
  watch(
    options,
    next => {
      const list = next?.models ?? []
      const current = list.find(model => model.id === modelId.value)
      if (current) {
        ensureParams(current)
        return
      }
      const fallback = list.find(model => model.mode === mode.value) ?? list[0] ?? null
      modelId.value = fallback?.id ?? ''
      if (fallback) mode.value = fallback.mode
      ensureParams(fallback)
    },
    { immediate: true },
  )

  return {
    mode,
    modelId,
    prompt,
    params,
    allModels,
    modelsForMode,
    selectedModel,
    protocol,
    paramDescriptors,
    imageDescriptors,
    promptLength,
    promptEmpty,
    promptTooLong,
    promptValid,
    invalidParamKeys,
    paramsValid,
    selectModel,
    setMode,
    setParam,
    setPrompt,
    resetParams,
    applyParams,
  }
}
