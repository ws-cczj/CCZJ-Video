import { computed, onBeforeUnmount, watch, type Ref } from 'vue'
import { isMobileRuntime } from '../api/runtime'
import { readPlayerDevice, restoreDeviceBrightness, setDeviceBrightness } from '../api/mobilePlayer'
let nativeOwner=0

/** Window brightness belongs to a reading session, never to application settings. */
export function useReadingBrightness(preferences:Ref<{brightness:number;systemBrightness:boolean}>) {
  const native=isMobileRuntime() && !!readPlayerDevice()
  const owner=native?++nativeOwner:0
  watch(()=>[preferences.value.brightness,preferences.value.systemBrightness],()=>{
    if(!native || owner!==nativeOwner)return
    if(preferences.value.systemBrightness)restoreDeviceBrightness()
    else setDeviceBrightness(preferences.value.brightness)
  },{immediate:true})
  // Page transitions overlap two chapters. The departing reader must never
  // restore system brightness after the incoming reader has claimed it.
  onBeforeUnmount(()=>{if(native && owner===nativeOwner)restoreDeviceBrightness()})
  return computed(()=>!native && !preferences.value.systemBrightness ? 1-preferences.value.brightness : 0)
}
