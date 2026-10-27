import { ref, watch, onMounted } from 'vue'
import { readStorageBoolean, writeStorage } from '../platform/storage'
import { createPlayerSettings } from './settings'
import { GetSetting, SetSetting } from '../api/app'
import { createSettingsQueue } from '../api/settingsQueue'
import { useErrorStore } from '../stores/error'
import { tr } from '../locales'

export const HOLD_SPEEDS = [2, 2.5, 3, 4] as const
export const PICTURE_FITS = ['auto', 'contain', 'cover', 'fill', 'width', 'height'] as const
export type PictureFit = typeof PICTURE_FITS[number]
const holdSpeed = ref(2), pictureFit = ref<PictureFit>('auto')
let holdEdited = false, fitEdited = false, loaded = false, loading: Promise<void> | undefined
// Shared across overlapping player instances so older writes cannot win later.
const preferenceWrites = createSettingsQueue((key,value)=>SetSetting(key,value),(_pending,error)=>{
  if(error)useErrorStore().fromError(tr('player.preferenceSaveFailed'),error,'Player.preferences')
})
function loadDisplayPreferences(): Promise<void> {
  if(loaded)return Promise.resolve()
  if(loading)return loading
  loading=Promise.all([GetSetting('player_hold_speed'),GetSetting('player_picture_fit')]).then(([speed,fit])=>{
    const value=Number(speed)
    if(!holdEdited && HOLD_SPEEDS.some(option=>option===value))holdSpeed.value=value
    if(!fitEdited && PICTURE_FITS.some(option=>option===fit))pictureFit.value=fit as PictureFit
    loaded=true
  }).catch(error=>{useErrorStore().fromError(tr('player.preferenceLoadFailed'),error,'Player.preferences')}).finally(()=>{loading=undefined})
  return loading
}

/** Persistence and effective values are shared by settings and media events. */
export function usePlaybackPreferences(autoplay:()=>boolean, notify:()=>void) {
  const settings=createPlayerSettings()
  const autoPlayEnabled=ref(readStorageBoolean('cczj_auto_play',autoplay()))
  const autoNextEnabled=ref(readStorageBoolean('cczj_auto_next',true))
  const autoResume=ref(settings.read('auto_resume_jump','0')==='1')
  onMounted(()=>{void loadDisplayPreferences()})
  function setHoldSpeed(value:number):void {
    if(!HOLD_SPEEDS.some(option=>option===value))return
    holdEdited=true;holdSpeed.value=value
    void preferenceWrites.set('player_hold_speed',String(value))
  }
  function setPictureFit(value:string):void {
    if(!PICTURE_FITS.some(option=>option===value))return
    fitEdited=true;pictureFit.value=value as PictureFit
    void preferenceWrites.set('player_picture_fit',value)
  }
  watch(autoplay,value=>{autoPlayEnabled.value=value})
  function toggleAutoPlay():void {
    const value=!autoPlayEnabled.value
    if(writeStorage('cczj_auto_play',value)){autoPlayEnabled.value=value;notify()}
  }
  function toggleAutoNext():void {
    const value=!autoNextEnabled.value
    if(writeStorage('cczj_auto_next',value))autoNextEnabled.value=value
  }
  function toggleAutoResume():void {
    const value=!autoResume.value
    if(settings.write('auto_resume_jump',value?'1':'0'))autoResume.value=value
  }
  return {settings,autoPlayEnabled,autoNextEnabled,autoResume,toggleAutoPlay,toggleAutoNext,toggleAutoResume,holdSpeed,pictureFit,setHoldSpeed,setPictureFit}
}
