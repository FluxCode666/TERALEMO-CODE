import { mount, type VueWrapper } from '@vue/test-utils'
import { gsap } from 'gsap'
import { defineComponent, h, onMounted, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useMotion } from '@/composables/useMotion'

describe('useMotion', () => {
  let wrapper: VueWrapper | undefined
  let motion: ReturnType<typeof useMotion>
  let reduced = false
  const listeners = new Set<(event: MediaQueryListEvent) => void>()
  const contextSpy = vi.spyOn(gsap, 'context')
  const fromToSpy = vi.spyOn(gsap, 'fromTo')
  const toSpy = vi.spyOn(gsap, 'to')

  function render(beforeMount?: (api: ReturnType<typeof useMotion>) => void) {
    wrapper = mount(defineComponent({
      setup() {
        const root = ref<HTMLElement | null>(null)
        motion = useMotion(root)
        beforeMount?.(motion)
        return () => h('section', { ref: root }, [
          h('div', { class: 'card-item', style: { opacity: '0.8' } }, '卡片'),
          h('div', { class: 'card-item' }, '卡片'),
          h('span', { class: 'count' }, '原始数字'),
        ])
      },
    }))
    return wrapper
  }

  beforeEach(() => {
    vi.stubEnv('MODE', 'development')
    reduced = false
    listeners.clear()
    vi.clearAllMocks()
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
      get matches() { return reduced },
      media: query,
      onchange: null,
      addEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) => listeners.add(listener),
      removeEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) => listeners.delete(listener),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    } as MediaQueryList))
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.unstubAllEnvs()
  })

  it('减少动态效果时不创建 tween，数字直接显示格式化后的最终值', () => {
    reduced = true
    const view = render()
    motion.fadeIn()
    motion.stagger('.card-item')
    motion.hover('.card-item')
    motion.countUp('.count', 1200, { format: value => `¥${value}` })
    expect(contextSpy).not.toHaveBeenCalled()
    expect(fromToSpy).not.toHaveBeenCalled()
    expect(toSpy).not.toHaveBeenCalled()
    expect(view.get('.count').text()).toBe('¥1200')
    expect(view.get('.card-item').attributes('style')).toContain('opacity: 0.8')
  })

  it('Vitest 默认跳过动画，可安全使用 jsdom', () => {
    vi.stubEnv('MODE', 'test')
    const view = render()
    motion.fadeIn()
    motion.countUp('.count', 42)
    expect(contextSpy).not.toHaveBeenCalled()
    expect(view.get('.count').text()).toBe('42')
  })

  it('未挂载或卸载后调用不会操作 DOM', () => {
    render(api => api.fadeIn())
    expect(contextSpy).not.toHaveBeenCalled()
    wrapper!.unmount()
    motion.fadeIn()
    motion.hover('.card-item')
    motion.countUp('.count', 42)
    expect(contextSpy).not.toHaveBeenCalled()
  })

  it('选择器和元素目标限定在当前容器，入场结束清除临时样式', () => {
    const view = render()
    const outside = document.createElement('div')
    motion.fadeIn(outside)
    expect(contextSpy).not.toHaveBeenCalled()
    motion.stagger('.card-item', { stagger: 0.05 })
    expect(fromToSpy.mock.calls[0][0]).toHaveLength(2)
    fromToSpy.mock.results[0].value.progress(1)
    expect(view.get('.card-item').element.style.transform).toBe('')
    expect(view.get('.card-item').element.style.visibility).toBe('')
  })

  it('卸载会 revert 上下文、恢复样式和文本，并移除悬浮及媒体查询监听', () => {
    const view = render()
    const card = view.get<HTMLElement>('.card-item').element
    const count = view.get<HTMLElement>('.count').element
    motion.fadeIn('.card-item')
    motion.countUp('.count', 100)
    motion.hover('.card-item')
    card.dispatchEvent(new MouseEvent('pointerenter'))
    const hoverTween = toSpy.mock.results.at(-1)!.value
    hoverTween.progress(0.5)
    const contexts = contextSpy.mock.results.map(result => result.value as gsap.Context)
    const reverts = contexts.map(context => vi.spyOn(context, 'revert'))
    const callsBeforeUnmount = toSpy.mock.calls.length
    view.unmount()
    expect(reverts.every(spy => spy.mock.calls.length === 1)).toBe(true)
    expect(card.style.opacity).toBe('0.8')
    expect(card.style.transform).toBe('')
    expect(count.textContent).toBe('原始数字')
    expect(listeners.size).toBe(0)
    card.dispatchEvent(new MouseEvent('pointerenter'))
    expect(toSpy).toHaveBeenCalledTimes(callsBeforeUnmount)
  })

  it('运行中开启减少动态效果会停止动画并显示数字最终值', () => {
    const view = render()
    motion.fadeIn()
    motion.countUp('.count', 99)
    reduced = true
    listeners.forEach(listener => listener({ matches: true } as MediaQueryListEvent))
    expect(view.get('.count').text()).toBe('99')
    expect(view.element.style.opacity).toBe('')
    const created = contextSpy.mock.calls.length
    motion.stagger('.card-item')
    expect(contextSpy).toHaveBeenCalledTimes(created)
  })

  it('单个动效返回的清理函数幂等，并可提前清理悬浮事件', () => {
    const view = render()
    const stop = motion.hover('.card-item')
    stop()
    stop()
    view.get('.card-item').element.dispatchEvent(new MouseEvent('pointerenter'))
    expect(toSpy).not.toHaveBeenCalled()
  })

  it('在 mounted 钩子内调用可创建并完成数字滚动', () => {
    wrapper = mount(defineComponent({
      setup() {
        const root = ref<HTMLElement | null>(null)
        const api = useMotion(root)
        onMounted(() => api.countUp('.count', 123, { from: 10, format: value => value.toFixed(1) }))
        return () => h('div', { ref: root }, [h('span', { class: 'count' }, '123.0')])
      },
    }))
    expect(wrapper.get('.count').text()).toBe('10.0')
    toSpy.mock.results[0].value.progress(1)
    expect(wrapper.get('.count').text()).toBe('123.0')
  })
})
