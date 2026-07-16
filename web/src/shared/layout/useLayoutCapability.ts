import { computed, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

export type LayoutMode = 'compact-portrait' | 'compact-landscape' | 'regular' | 'wide'
export type LayoutDensity = 'compact' | 'comfortable' | 'spacious'

export interface LayoutCapability {
  mode: Ref<LayoutMode>
  containerWidth: Ref<number>
  containerHeight: Ref<number>
  orientation: Ref<'portrait' | 'landscape'>
  pointer: Ref<'coarse' | 'fine'>
  hover: Ref<'available' | 'unavailable'>
  reducedMotion: Ref<boolean>
  density: Ref<LayoutDensity>
  isCompact: Ref<boolean>
}

// useLayoutCapability intentionally describes the space and input available
// to a task surface rather than guessing a device class from User-Agent.
export function useLayoutCapability(target?: Ref<HTMLElement | null>): LayoutCapability {
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

  const orientation = computed<'portrait' | 'landscape'>(() =>
    containerWidth.value > containerHeight.value ? 'landscape' : 'portrait',
  )
  const mode = computed<LayoutMode>(() => {
    const width = containerWidth.value
    if (width <= 760 || (containerHeight.value <= 540 && width <= 1000)) {
      return orientation.value === 'landscape' ? 'compact-landscape' : 'compact-portrait'
    }
    if (width >= 1280) return 'wide'
    return 'regular'
  })
  const density = computed<LayoutDensity>(() => {
    if (mode.value.startsWith('compact')) return 'compact'
    return mode.value === 'wide' ? 'spacious' : 'comfortable'
  })
  const isCompact = computed(() => mode.value === 'compact-portrait' || mode.value === 'compact-landscape')

  const measure = () => {
    const element = targetElement.value
    containerWidth.value = Math.round(element?.clientWidth || window.innerWidth || 0)
    const viewportHeight = window.innerHeight || element?.clientHeight || 0
    containerHeight.value = Math.round(Math.min(element?.clientHeight || viewportHeight, viewportHeight))
  }
  const syncMedia = () => {
    pointer.value = pointerQuery?.matches ? 'coarse' : 'fine'
    hover.value = hoverQuery?.matches ? 'available' : 'unavailable'
    reducedMotion.value = Boolean(motionQuery?.matches)
  }
  const observeTarget = () => {
    observer?.disconnect()
    const element = targetElement.value
    if (element && typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(measure)
      observer.observe(element)
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
