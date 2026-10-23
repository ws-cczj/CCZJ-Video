import { ref, watch } from 'vue'
import { readStorageBoolean, writeStorage } from '../platform/storage'
import { createPlayerSettings } from './settings'

/** Persistence and effective values are shared by settings and media events. */
export function usePlaybackPreferences(autoplay:()=>boolean, notify:()=>void) {
  const settings=createPlayerSettings()
  const autoPlayEnabled=ref(readStorageBoolean('cczj_auto_play',autoplay()))
  const autoNextEnabled=ref(readStorageBoolean('cczj_auto_next',true))
  const autoResume=ref(settings.read('auto_resume_jump','0')==='1')
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
  return {settings,autoPlayEnabled,autoNextEnabled,autoResume,toggleAutoPlay,toggleAutoNext,toggleAutoResume}
}
