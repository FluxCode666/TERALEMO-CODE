/**
 * 首页地形图的等高线几何。
 * 每座“山”由若干闭合等高线组成：level 1 是最内圈（紧贴山顶），level = levels 是最外圈。
 * 半径随 level 单调递增，形状扰动与圆心漂移都控制在相邻圈间距以内，保证等高线互不相交。
 */

export interface Point {
  x: number
  y: number
}

export interface HillSpec {
  cx: number
  cy: number
  levels: number
  innerRadius: number
  outerRadius: number
  /** 每向内一圈，圆心向山顶方向漂移的距离 */
  drift: Point
  /** [频率, 振幅（相对半径）, 相位] */
  harmonics: ReadonlyArray<readonly [number, number, number]>
  /** 每圈独立的小幅扰动（像素），让等高线不像简单缩放 */
  ripple: number
  seed: number
}

export interface ContourRing {
  level: number
  d: string
}

const SAMPLES = 96
const round = (value: number) => Math.round(value * 10) / 10

function hillCenter(spec: HillSpec, steps: number): Point {
  return { x: spec.cx + spec.drift.x * steps, y: spec.cy + spec.drift.y * steps }
}

export function ringPoint(spec: HillSpec, level: number, theta: number): Point {
  const t = spec.levels === 1 ? 1 : (level - 1) / (spec.levels - 1)
  const radius = spec.innerRadius + (spec.outerRadius - spec.innerRadius) * t
  const shape = spec.harmonics.reduce((sum, [k, amplitude, phase]) => sum + amplitude * Math.sin(k * theta + phase), 0)
  // 越靠近山顶越圆润
  const roughness = 0.45 + 0.55 * t
  const ripple = spec.ripple * Math.sin(4 * theta + level * 0.9 + spec.seed)
  const r = radius * (1 + roughness * shape) + ripple
  const center = hillCenter(spec, spec.levels - level)
  return { x: center.x + r * Math.cos(theta), y: center.y + r * Math.sin(theta) }
}

export function peakPoint(spec: HillSpec): Point {
  return hillCenter(spec, spec.levels)
}

/** Catmull-Rom 样条转三次贝塞尔；closed 时首尾相接。 */
export function smoothPath(points: readonly Point[], closed: boolean): string {
  const n = points.length
  if (n < 2) return ''
  const at = (i: number) => (closed ? points[(i + n) % n] : points[Math.min(Math.max(i, 0), n - 1)])
  const segments = closed ? n : n - 1
  let d = `M${round(points[0].x)} ${round(points[0].y)}`
  for (let i = 0; i < segments; i++) {
    const p0 = at(i - 1)
    const p1 = at(i)
    const p2 = at(i + 1)
    const p3 = at(i + 2)
    const c1x = p1.x + (p2.x - p0.x) / 6
    const c1y = p1.y + (p2.y - p0.y) / 6
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = p2.y - (p3.y - p1.y) / 6
    d += `C${round(c1x)} ${round(c1y)} ${round(c2x)} ${round(c2y)} ${round(p2.x)} ${round(p2.y)}`
  }
  return closed ? `${d}Z` : d
}

export function buildContours(spec: HillSpec): ContourRing[] {
  return Array.from({ length: spec.levels }, (_, index) => {
    const level = spec.levels - index // 由外向内输出，便于入场动画从外圈开始
    const points = Array.from({ length: SAMPLES }, (_, i) => ringPoint(spec, level, (i / SAMPLES) * Math.PI * 2))
    return { level, d: smoothPath(points, true) }
  })
}
