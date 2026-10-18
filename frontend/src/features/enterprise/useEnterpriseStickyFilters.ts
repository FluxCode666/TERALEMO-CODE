import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

/** Keep the sentinel outside the sticky element so collapsing it cannot move the threshold. */
export function useEnterpriseStickyFilters() {
  const sentinel = ref<HTMLElement>()
  const headerHeight = ref(0)
  const isSticky = ref(false)
  const expanded = ref(false)
  const showFilters = computed(() => !isSticky.value || expanded.value)
  let intersection: IntersectionObserver | undefined
  let resize: ResizeObserver | undefined

  watch(isSticky, () => { expanded.value = false })

  onMounted(() => {
    const header = document.querySelector<HTMLElement>('[data-app-header]')
    function observe() {
      headerHeight.value = header?.getBoundingClientRect().height ?? 0
      intersection?.disconnect()
      if (!sentinel.value || typeof IntersectionObserver === 'undefined') return
      intersection = new IntersectionObserver(([entry]) => {
        if (entry) isSticky.value = !entry.isIntersecting && entry.boundingClientRect.top < headerHeight.value
      }, { rootMargin: `-${headerHeight.value}px 0px 0px 0px`, threshold: 0 })
      intersection.observe(sentinel.value)
    }
    observe()
    if (header && typeof ResizeObserver !== 'undefined') {
      resize = new ResizeObserver(observe)
      resize.observe(header)
    }
  })

  onBeforeUnmount(() => { intersection?.disconnect(); resize?.disconnect() })
  return { sentinel, headerHeight, isSticky, expanded, showFilters }
}
