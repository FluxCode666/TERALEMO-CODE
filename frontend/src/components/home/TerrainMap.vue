<template>
  <figure class="terrain" :aria-label="t('home.map.label')">
    <svg
      class="terrain-svg"
      :viewBox="`0 0 ${W} ${H}`"
      preserveAspectRatio="xMidYMid meet"
      aria-hidden="true"
      focusable="false"
    >
      <g class="terrain-faint">
        <path v-for="ring in faintRings" :key="ring.key" :d="ring.d" class="contour contour--faint" />
      </g>

      <g>
        <path
          v-for="(ring, index) in mainRings"
          :key="ring.level"
          :d="ring.d"
          pathLength="1000"
          class="contour contour--draw"
          :class="{ 'contour--index': ring.level % 4 === 0 }"
          :style="{ '--i': index }"
        />
      </g>

      <g>
        <path v-for="flow in flows" :key="flow.level" :d="flow.d" pathLength="1000" class="flow" :style="{ '--flow-delay': flow.delay }" />
      </g>

      <path :d="rollupPath" pathLength="1000" class="rollup" />

      <g class="elevation">
        <g v-for="label in elevationLabels" :key="label.level">
          <rect :x="label.x - 13" :y="label.y - 7.5" width="26" height="15" rx="2" class="elevation-bg" />
          <text :x="label.x" :y="label.y" class="elevation-text" text-anchor="middle" dominant-baseline="central">
            L{{ label.level }}
          </text>
        </g>
      </g>

      <g class="peak" :transform="`translate(${peak.x} ${peak.y})`">
        <circle r="15" class="peak-ring" />
        <circle r="3.5" class="peak-dot" />
        <path d="M-24 0H-9M9 0H24M0 -24V-9M0 9V24" class="peak-cross" />
      </g>
    </svg>

    <div class="markers">
      <div
        v-for="marker in markers"
        :key="marker.id"
        class="marker"
        :class="[`marker--${marker.side}`, `marker--${marker.tone}`, { 'marker--compact-hide': marker.hideOnCompact }]"
        :style="{ left: percent(marker.at.x, W), top: percent(marker.at.y, H), '--d': marker.delay }"
      >
        <span class="marker-dot" />
        <span class="marker-card">
          <span class="marker-title" :class="{ 'marker-title--mono': marker.mono }">{{ marker.title }}</span>
          <span class="marker-meta">{{ marker.meta }}</span>
        </span>
      </div>
    </div>

    <figcaption class="legend">
      <span class="legend-swatch" aria-hidden="true">
        <i /><i /><i />
      </span>
      <span>{{ t('home.map.legend') }}</span>
      <span class="legend-sep" aria-hidden="true">·</span>
      <span class="legend-rollup">{{ t('home.map.rollup') }}</span>
    </figcaption>
  </figure>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { buildContours, peakPoint, ringPoint, smoothPath, type HillSpec, type Point } from './terrain'

const { t } = useI18n()

const W = 760
const H = 680
const PI = Math.PI

// 主山：8 圈等高线 = 最多 8 级部门，山顶是企业本身
const MAIN: HillSpec = {
  cx: 432,
  cy: 360,
  levels: 8,
  innerRadius: 34,
  outerRadius: 286,
  drift: { x: -5, y: -4 },
  harmonics: [
    [2, 0.09, 0.6],
    [3, 0.06, 1.9],
    [5, 0.03, 0.3],
  ],
  ripple: 3.5,
  seed: 0.4,
}

const FAINT: HillSpec[] = [
  {
    cx: 64,
    cy: 650,
    levels: 5,
    innerRadius: 22,
    outerRadius: 150,
    drift: { x: 3, y: -3 },
    harmonics: [
      [2, 0.08, 2.2],
      [3, 0.05, 0.4],
      [4, 0.03, 1.1],
    ],
    ripple: 2.5,
    seed: 1.7,
  },
  {
    cx: 770,
    cy: -10,
    levels: 4,
    innerRadius: 28,
    outerRadius: 128,
    drift: { x: -3, y: 3 },
    harmonics: [
      [2, 0.07, 1.2],
      [3, 0.05, 2.8],
    ],
    ripple: 2,
    seed: 2.9,
  },
]

const mainRings = buildContours(MAIN)
const faintRings = FAINT.flatMap((spec, hill) => buildContours(spec).map((ring) => ({ key: `${hill}-${ring.level}`, d: ring.d })))
const peak = peakPoint(MAIN)

const flows = [3, 6].map((level, index) => ({
  level,
  d: mainRings.find((ring) => ring.level === level)?.d ?? '',
  delay: `${index * -4.5}s`,
}))

const elevationLabels = [1, 2, 3, 4, 5, 6, 7, 8].map((level) => ({ level, ...ringPoint(MAIN, level, PI * 1.12) }))

const at = {
  request: ringPoint(MAIN, 8, PI * 0.71),
  backend: ringPoint(MAIN, 5, PI * 0.1),
  rd: ringPoint(MAIN, 3, -PI * 0.3),
}

// 用量从成员请求经部门逐级汇总到企业
const rollupPath = smoothPath(
  [at.request, ringPoint(MAIN, 7, PI * 0.38), at.backend, ringPoint(MAIN, 4, -PI * 0.12), at.rd, peak],
  false,
)

interface Marker {
  id: string
  at: Point
  title: string
  meta: string
  side: 'left' | 'right'
  tone: 'signal' | 'marker' | 'ink'
  mono?: boolean
  hideOnCompact?: boolean
  delay: string
}

const markers = computed<Marker[]>(() => [
  { id: 'peak', at: peak, title: t('home.map.enterprise'), meta: 'L0', side: 'right', tone: 'marker', hideOnCompact: true, delay: '1.5s' },
  { id: 'rd', at: at.rd, title: t('home.map.rd'), meta: t('home.map.rdMeta'), side: 'right', tone: 'signal', delay: '1.7s' },
  {
    id: 'backend',
    at: at.backend,
    title: t('home.map.backend'),
    meta: t('home.map.backendMeta'),
    side: 'left',
    tone: 'marker',
    hideOnCompact: true,
    delay: '1.9s',
  },
  {
    id: 'request',
    at: at.request,
    title: 'POST /v1/messages',
    meta: t('home.map.requestMeta'),
    side: 'right',
    tone: 'ink',
    mono: true,
    hideOnCompact: true,
    delay: '2.1s',
  },
])

function percent(value: number, total: number): string {
  return `${((value / total) * 100).toFixed(3)}%`
}
</script>

<style scoped>
.terrain {
  position: relative;
  aspect-ratio: 760 / 680;
  width: 100%;
  margin: 0;
}

.terrain-svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  overflow: visible;
}

.contour {
  fill: none;
  stroke: var(--tl-contour);
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}

.contour--index {
  stroke: var(--tl-contour-strong);
  stroke-width: 1.6;
}

.contour--faint {
  stroke: var(--tl-contour);
  opacity: 0.55;
  stroke-dasharray: 2 5;
}

.contour--draw {
  stroke-dasharray: 1000;
  stroke-dashoffset: 1000;
  animation: tl-draw 2.4s cubic-bezier(0.65, 0.05, 0.25, 1) forwards;
  animation-delay: calc(var(--i) * 110ms + 150ms);
}

.flow {
  fill: none;
  stroke: var(--tl-signal);
  stroke-width: 2.2;
  stroke-linecap: round;
  vector-effect: non-scaling-stroke;
  stroke-dasharray: 46 954;
  opacity: 0;
  animation:
    tl-fade-in 0.6s ease 2.4s forwards,
    tl-flow 9s linear infinite;
  animation-delay: 2.4s, var(--flow-delay);
}

.rollup {
  fill: none;
  stroke: var(--tl-signal);
  stroke-width: 1.25;
  stroke-dasharray: 4 6;
  vector-effect: non-scaling-stroke;
  opacity: 0;
  animation: tl-fade-in 0.8s ease 2.2s forwards;
}

.elevation {
  opacity: 0;
  animation: tl-fade-in 0.8s ease 1.4s forwards;
}

.elevation-bg {
  fill: var(--tl-paper);
}

.elevation-text {
  fill: var(--tl-muted);
  font-family: var(--tl-font-mono);
  font-size: 10px;
  letter-spacing: 0.04em;
}

.peak {
  opacity: 0;
  animation: tl-fade-in 0.6s ease 1.3s forwards;
}

.peak-ring {
  fill: none;
  stroke: var(--tl-marker);
  stroke-width: 1.25;
  vector-effect: non-scaling-stroke;
}

.peak-dot {
  fill: var(--tl-marker);
}

.peak-cross {
  stroke: var(--tl-marker);
  stroke-width: 1.25;
  vector-effect: non-scaling-stroke;
}

/* ---------- HTML 标注 ---------- */
.markers {
  position: absolute;
  inset: 0;
  pointer-events: none;
}

.marker {
  position: absolute;
  display: flex;
  align-items: center;
  gap: 10px;
  transform: translate(-6px, -50%);
  opacity: 0;
  animation: tl-marker-in 0.6s cubic-bezier(0.2, 0.8, 0.2, 1) forwards;
  animation-delay: var(--d);
}

.marker--left {
  flex-direction: row-reverse;
  transform: translate(calc(-100% + 6px), -50%);
}

.marker-dot {
  position: relative;
  flex: none;
  width: 12px;
  height: 12px;
  border-radius: 999px;
  background: var(--tl-paper);
  border: 2px solid var(--tone);
}

.marker-dot::after {
  content: '';
  position: absolute;
  inset: -7px;
  border-radius: 999px;
  border: 1px solid var(--tone);
  opacity: 0;
  animation: tl-ping 2.8s ease-out infinite;
  animation-delay: calc(var(--d) + 0.6s);
}

.marker-card {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 10px 7px;
  border: 1px solid var(--tl-rule-strong);
  border-left: 2px solid var(--tone);
  background: color-mix(in srgb, var(--tl-paper) 88%, transparent);
  backdrop-filter: blur(6px);
  white-space: nowrap;
}

.marker--left .marker-card {
  border-left: 1px solid var(--tl-rule-strong);
  border-right: 2px solid var(--tone);
  text-align: right;
}

.marker-title {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.2;
  color: var(--tl-ink);
}

.marker-title--mono {
  font-family: var(--tl-font-mono);
  font-size: 12px;
  font-weight: 500;
}

.marker-meta {
  font-family: var(--tl-font-mono);
  font-size: 11px;
  line-height: 1.2;
  color: var(--tone);
}

.marker--signal {
  --tone: var(--tl-signal);
}

.marker--marker {
  --tone: var(--tl-marker);
}

.marker--ink {
  --tone: var(--tl-ink-soft);
}

.legend {
  position: absolute;
  left: 0;
  bottom: -4px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 10px;
  font-family: var(--tl-font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--tl-muted);
  opacity: 0;
  animation: tl-fade-in 0.8s ease 2.4s forwards;
}

.legend-swatch {
  display: inline-flex;
  gap: 3px;
}

.legend-swatch i {
  display: block;
  width: 14px;
  height: 0;
  border-top: 1px solid var(--tl-contour-strong);
}

.legend-swatch i:nth-child(2) {
  border-top-width: 2px;
}

.legend-rollup {
  color: var(--tl-signal);
}

@media (max-width: 639px) {
  .marker--compact-hide {
    display: none;
  }

  .marker-card {
    padding: 4px 8px 5px;
  }

  .legend-sep,
  .legend-rollup {
    display: none;
  }
}

@keyframes tl-draw {
  to {
    stroke-dashoffset: 0;
  }
}

@keyframes tl-flow {
  from {
    stroke-dashoffset: 1000;
  }
  to {
    stroke-dashoffset: 0;
  }
}

@keyframes tl-fade-in {
  to {
    opacity: 1;
  }
}

@keyframes tl-marker-in {
  from {
    opacity: 0;
    margin-top: 6px;
  }
  to {
    opacity: 1;
    margin-top: 0;
  }
}

@keyframes tl-ping {
  0% {
    opacity: 0.7;
    transform: scale(0.6);
  }
  70%,
  100% {
    opacity: 0;
    transform: scale(1.5);
  }
}

@media (prefers-reduced-motion: reduce) {
  .contour--draw,
  .flow,
  .rollup,
  .elevation,
  .peak,
  .marker,
  .legend {
    animation: none;
    opacity: 1;
    stroke-dashoffset: 0;
  }

  .flow {
    stroke-dasharray: none;
    opacity: 0;
  }

  .marker-dot::after {
    animation: none;
  }
}
</style>
