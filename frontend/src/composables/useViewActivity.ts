import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue'
import { nativeWindowEvents, onBackendEvent } from '../api/events'

// One listener serves every image/panel. KeepAlive deactivation is independent
// of document visibility and of the settings tab's active prop.
const foreground = ref(typeof document === 'undefined' || !document.hidden)
const nativeHidden = ref(false)
const nativeMinimized = ref(false)
export const windowForeground = computed(() => foreground.value && !nativeHidden.value && !nativeMinimized.value)
let detachWindowEvents: Array<() => void> = []
let subscribers = 0
function updateVisibility(): void { foreground.value = !document.hidden }

export function useViewActivity(enabled: () => boolean = () => true) {
  const activated = ref(true)
  onActivated(() => { activated.value = true })
  onDeactivated(() => { activated.value = false })
  onMounted(() => {
    if (subscribers++ === 0) {
      nativeHidden.value = false
      nativeMinimized.value = false
      document.addEventListener('visibilitychange', updateVisibility)
      const types = nativeWindowEvents()
      detachWindowEvents = [
        onBackendEvent(types.WindowHide, () => { nativeHidden.value = true }),
        onBackendEvent(types.WindowShow, () => { nativeHidden.value = false }),
        onBackendEvent(types.WindowMinimise, () => { nativeMinimized.value = true }),
        onBackendEvent(types.WindowUnMinimise, () => { nativeMinimized.value = false }),
        onBackendEvent(types.WindowRestore, () => { nativeMinimized.value = false; nativeHidden.value = false }),
      ]
    }
    updateVisibility()
  })
  onBeforeUnmount(() => {
    if (--subscribers === 0) {
      document.removeEventListener('visibilitychange', updateVisibility)
      detachWindowEvents.forEach(detach => detach())
      detachWindowEvents = []
    }
  })
  return computed(() => activated.value && windowForeground.value && enabled())
}
