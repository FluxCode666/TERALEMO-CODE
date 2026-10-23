// 视频平台账号「上游能力」配置：某些上游（如 MiniMax-H3 中转）支持超出官方范围的
// 分辨率与时长。三项均可选，全部缺省表示按协议与模型的官方规则校验。
// 存储在账号 credentials：video_resolutions / video_min_duration_seconds /
// video_max_duration_seconds。

export const VIDEO_RESOLUTION_OPTIONS = ['480p', '720p', '768p', '1080p', '2k'] as const

export const VIDEO_CAPABILITY_DURATION_MIN = 1
export const VIDEO_CAPABILITY_DURATION_MAX = 60

/** 归一化分辨率选择：统一小写、去重、按固定档位顺序输出（提交与回填共用）。 */
export function normalizeVideoResolutions(values: unknown): string[] {
  if (!Array.isArray(values)) return []
  const selected = new Set(values.filter(value => typeof value === 'string').map(value => value.toLowerCase()))
  return VIDEO_RESOLUTION_OPTIONS.filter(option => selected.has(option))
}

/** 凭据里的时长值 → 输入框文本；仅接受正整数，脏数据视为未配置。 */
export function readVideoDuration(value: unknown): string {
  if (typeof value === 'number' && Number.isInteger(value) && value > 0) return String(value)
  return ''
}

export type VideoDurationError = 'minInvalid' | 'maxInvalid' | 'minGtMax'

function parseDuration(raw: string | number): number | null {
  const text = String(raw).trim()
  if (!text) return null
  const value = Number(text)
  return Number.isInteger(value) ? value : Number.NaN
}

function validDuration(value: number): boolean {
  return Number.isInteger(value) && value >= VIDEO_CAPABILITY_DURATION_MIN && value <= VIDEO_CAPABILITY_DURATION_MAX
}

/**
 * 校验时长上下限：留空合法（表示按官方规则）；填写则必须是 1–60 的整数；
 * 两者都填时 min ≤ max。返回 i18n 错误后缀，null 表示通过。
 */
export function validateVideoDurations(minRaw: string | number, maxRaw: string | number): VideoDurationError | null {
  const min = parseDuration(minRaw)
  const max = parseDuration(maxRaw)
  if (min !== null && !validDuration(min)) return 'minInvalid'
  if (max !== null && !validDuration(max)) return 'maxInvalid'
  if (min !== null && max !== null && min > max) return 'minGtMax'
  return null
}

/**
 * 把上游能力写入 credentials。有值时写入（分辨率小写规范化、时长为整数）；
 * 值为空时：create 不写入（默认官方规则），edit 删除对应键（恢复官方规则，
 * 同时保证未配置过的旧账号不会被隐式写入）。
 */
export function applyVideoCapabilities(
  credentials: Record<string, unknown>,
  resolutions: string[],
  minRaw: string | number,
  maxRaw: string | number,
  mode: 'create' | 'edit'
): void {
  const normalized = normalizeVideoResolutions(resolutions)
  if (normalized.length > 0) {
    credentials.video_resolutions = normalized
  } else if (mode === 'edit') {
    delete credentials.video_resolutions
  }
  const min = parseDuration(minRaw)
  if (min !== null && !Number.isNaN(min)) {
    credentials.video_min_duration_seconds = min
  } else if (mode === 'edit') {
    delete credentials.video_min_duration_seconds
  }
  const max = parseDuration(maxRaw)
  if (max !== null && !Number.isNaN(max)) {
    credentials.video_max_duration_seconds = max
  } else if (mode === 'edit') {
    delete credentials.video_max_duration_seconds
  }
}
