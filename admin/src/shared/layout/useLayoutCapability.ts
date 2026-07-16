import { computed, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

export type LayoutMode = 'compact-portrait' | 'compact-landscape' | 'regular' | 'wide'

// Admin keeps the same layout contract as Web so operational screens respond
// to their actual container and input capabilities instead of device labels.
export function useLayoutCapability(target?: Ref<HTMLElement | null>) {
  const targetElement = target || ref<HTMLElement | null>(null)
  const containerWidth = ref(0)
  const containerHeight = ref(0)
  const pointer = ref<'coarse' | 'fine'>('fine')
  const hover = ref<'available' | 'unavailable'>('available')
  const reducedMotion = ref(false)
  let observer: ResizeObserver | undefined
  let pointerQuery: MediaQueryList | undefined
  let hoverQuery: MediaQueryList | undefined
  let motionQuery: MediaQueryList | undefined

  const orientation = computed<'portrait' | 'landscape'>(() => containerWidth.value > containerHeight.value ? 'landscape' : 'portrait')
  const mode = computed<LayoutMode>(() => {
    if (containerWidth.value <= 800 || (containerHeight.value <= 540 && containerWidth.value <= 1000)) {
      return orientation.value === 'landscape' ? 'compact-landscape' : 'compact-portrait'
    }
    return containerWidth.value >= 1280 ? 'wide' : 'regular'
  })
  const density = computed<'compact' | 'comfortable' | 'spacious'>(() => mode.value.startsWith('compact') ? 'compact' : mode.value === 'wide' ? 'spacious' : 'comfortable')
  const isCompact = computed(() => mode.value.startsWith('compact'))
  const measure = () => {
    containerWidth.value = Math.round(targetElement.value?.clientWidth || window.innerWidth || 0)
    const viewportHeight = window.innerHeight || targetElement.value?.clientHeight || 0
    containerHeight.value = Math.round(Math.min(targetElement.value?.clientHeight || viewportHeight, viewportHeight))
  }
  const syncMedia = () => {
    pointer.value = pointerQuery?.matches ? 'coarse' : 'fine'
    hover.value = hoverQuery?.matches ? 'available' : 'unavailable'
    reducedMotion.value = Boolean(motionQuery?.matches)
  }
  const observeTarget = () => {
    observer?.disconnect()
    if (targetElement.value && typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(measure)
      observer.observe(targetElement.value)
    }
    measure()
  }
  onMounted(() => {
    pointerQuery = window.matchMedia('(pointer: coarse)')
    hoverQuery = window.matchMedia('(hover: hover)')
    motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
    pointerQuery.addEventListener('change', syncMedia)
    hoverQuery.addEventListener('change', syncMedia)
    motionQuery.addEventListener('change', syncMedia)
    window.addEventListener('resize', measure)
    syncMedia()
    observeTarget()
  })
  onBeforeUnmount(() => {
    observer?.disconnect()
    pointerQuery?.removeEventListener('change', syncMedia)
    hoverQuery?.removeEventListener('change', syncMedia)
    motionQuery?.removeEventListener('change', syncMedia)
    window.removeEventListener('resize', measure)
  })
  watch(targetElement, observeTarget)
  return { mode, containerWidth, containerHeight, orientation, pointer, hover, reducedMotion, density, isCompact }
}
