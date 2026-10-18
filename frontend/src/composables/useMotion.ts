import { gsap } from 'gsap'
import { onMounted, onScopeDispose, toValue, type MaybeRefOrGetter } from 'vue'

type MotionElements = HTMLElement | readonly HTMLElement[] | NodeListOf<HTMLElement>
export type MotionTarget = MaybeRefOrGetter<MotionElements | string | null | undefined>
export type MotionCleanup = () => void

export interface MotionOptions {
  disabled?: MaybeRefOrGetter<boolean>
}

export interface EntranceOptions {
  duration?: number
  distance?: number
}

export interface StaggerOptions extends EntranceOptions {
  stagger?: number
}

export interface CountUpOptions {
  from?: number
  duration?: number
  format?: (value: number) => string
}

export interface HoverOptions {
  distance?: number
  duration?: number
}

const noop: MotionCleanup = () => {}

/**
 * 在 setup 中传入容器 ref；在 onMounted 或 nextTick 后调用各动效。
 * 选择器只查询容器内部。每次调用返回独立清理函数，卸载时统一 revert。
 * 测试、SSR、未挂载、缺少 matchMedia 或减少动态效果时跳过动画。
 * countUp 跳过时仍写入最终值；请使用专用的数字文本节点，避免覆盖子节点。
 */
export function useMotion(
  scope: MaybeRefOrGetter<HTMLElement | null | undefined>,
  options: MotionOptions = {},
) {
  let mounted = false
  let disposed = false
  let media: MediaQueryList | undefined
  const active = new Set<{ context: gsap.Context; finish?: () => void }>()

  function stopAll(finishCounts = false) {
    // 后创建的动效可能捕获了前一个动效的中间样式，按相反顺序还原。
    for (const entry of Array.from(active).reverse()) {
      entry.context.revert()
      if (finishCounts) entry.finish?.()
    }
    active.clear()
  }

  function onPreferenceChange(event: MediaQueryListEvent) {
    if (event.matches) stopAll(true)
  }

  onMounted(() => {
    mounted = true
    if (typeof window.matchMedia === 'function') {
      media = window.matchMedia('(prefers-reduced-motion: reduce)')
      media.addEventListener?.('change', onPreferenceChange)
    }
  })

  onScopeDispose(() => {
    disposed = true
    media?.removeEventListener?.('change', onPreferenceChange)
    stopAll()
  })

  function elements(target?: MotionTarget): HTMLElement[] {
    if (!mounted || disposed) return []
    const root = toValue(scope)
    if (!root) return []
    const value = target === undefined ? root : toValue(target)
    if (!value) return []
    if (typeof value === 'string') return Array.from(root.querySelectorAll<HTMLElement>(value))
    const nodes = value instanceof HTMLElement ? [value] : Array.from(value)
    return nodes.filter(node => node === root || root.contains(node))
  }

  function shouldSkip() {
    return import.meta.env.MODE === 'test'
      || typeof window === 'undefined'
      || !media
      || media.matches
      || Boolean(toValue(options.disabled))
  }

  function run(setup: (context: gsap.Context) => void | MotionCleanup, finish?: () => void): MotionCleanup {
    const root = toValue(scope)
    if (!root || shouldSkip()) return noop
    const entry = { context: gsap.context(setup, root), finish }
    active.add(entry)
    return () => {
      if (active.delete(entry)) entry.context.revert()
    }
  }

  /** Tab 内容切换完成 DOM 更新后调用。 */
  function fadeIn(target?: MotionTarget, config: EntranceOptions = {}): MotionCleanup {
    const nodes = elements(target)
    if (!nodes.length) return noop
    return run(() => {
      gsap.fromTo(nodes,
        { autoAlpha: 0, y: config.distance ?? 6 },
        { autoAlpha: 1, y: 0, duration: config.duration ?? 0.2, ease: 'power2.out', overwrite: 'auto', clearProps: 'opacity,visibility,transform' },
      )
    })
  }

  /** 列表或卡片依次入场；传入容器内的选择器或元素集合。 */
  function stagger(target: MotionTarget, config: StaggerOptions = {}): MotionCleanup {
    const nodes = elements(target)
    if (!nodes.length) return noop
    return run(() => {
      gsap.fromTo(nodes,
        { autoAlpha: 0, y: config.distance ?? 10 },
        { autoAlpha: 1, y: 0, duration: config.duration ?? 0.3, stagger: config.stagger ?? 0.04, ease: 'power2.out', overwrite: 'auto', clearProps: 'opacity,visibility,transform' },
      )
    })
  }

  /** 将专用文本节点从 from 滚动到 to；跳过动画时直接显示 to。 */
  function countUp(target: MotionTarget, to: number, config: CountUpOptions = {}): MotionCleanup {
    const nodes = elements(target)
    if (!nodes.length || !Number.isFinite(to)) return noop
    const format = config.format ?? ((value: number) => Math.round(value).toLocaleString())
    const write = (value: number) => nodes.forEach(node => { node.textContent = format(value) })
    const finish = () => write(to)
    if (shouldSkip()) {
      finish()
      return noop
    }
    return run(() => {
      const original = nodes.map(node => node.textContent)
      const counter = { value: config.from ?? 0 }
      write(counter.value)
      gsap.to(counter, {
        value: to,
        duration: config.duration ?? 0.6,
        ease: 'power2.out',
        onUpdate: () => write(counter.value),
        onComplete: finish,
      })
      return () => nodes.forEach((node, index) => { node.textContent = original[index] ?? '' })
    }, finish)
  }

  /** 卡片悬浮上移；事件回调创建的 tween 同样纳入 context 清理。 */
  function hover(target: MotionTarget, config: HoverOptions = {}): MotionCleanup {
    const nodes = elements(target)
    if (!nodes.length) return noop
    return run((context) => {
      // 事件在 context 创建后才触发，必须重新通过 context.add 记录。
      const removers = nodes.map(node => {
        const animate = (y: number) => {
          if (disposed || shouldSkip()) return
          context.add(() => {
            gsap.to(node, { y, duration: config.duration ?? 0.18, ease: 'power2.out', overwrite: 'auto' })
          })
        }
        const enter = (event: PointerEvent) => {
          if (event.pointerType !== 'touch') animate(-(config.distance ?? 3))
        }
        const leave = () => animate(0)
        node.addEventListener('pointerenter', enter)
        node.addEventListener('pointerleave', leave)
        return () => {
          node.removeEventListener('pointerenter', enter)
          node.removeEventListener('pointerleave', leave)
        }
      })
      return () => {
        removers.forEach(remove => remove())
      }
    })
  }

  return { fadeIn, stagger, countUp, hover, stop: () => stopAll() }
}
