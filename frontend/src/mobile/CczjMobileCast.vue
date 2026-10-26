<script setup lang="ts">
import './player/presentation.css'
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { Button } from '../components/ui'
import CczjMobilePlayerDrawer from './CczjMobilePlayerDrawer.vue'
import { DiscoverCastDevices, PlayCastVideo, ControlCastVideo, GetCastStatus, normalizeApiError } from '../api/app'
import { createPlayerPlatform } from '../player/platform'
import { createRequestScope } from '../api/requestScope'
import { formatPlaybackTime } from '../player/videoInfoFormat'
const platform=createPlayerPlatform()
const props = defineProps<{ open: boolean; url: string; title: string; position: number; host?:HTMLElement; fullscreen?:boolean }>()
const emit = defineEmits<{ close: []; connected: []; stopped: [] }>()
const { t } = useI18n()
const devices = ref<{ id: string; name: string }[]>([])
const scanning = ref(false), busy = ref(false), error = ref('')
let announced=false
const selected = ref<{ id: string; name: string } | null>(null)
const paused = ref(false)
const remote=ref<{state:string;transport_status:string;position:number;duration:number;has_position:boolean}|null>(null)
const statusError=ref(''),statusScope=createRequestScope(),controlScope=createRequestScope()
let statusTimer:ReturnType<typeof setTimeout>|undefined,statusRevision=0,sessionRevision=0
const remoteLabel=computed(()=>{
 if(remote.value?.transport_status==='ERROR_OCCURRED')return t('mobile.castReceiverError')
 if(remote.value?.state==='PLAYING'&&remote.value.has_position&&remote.value.position<=0)return t('mobile.castChecking')
 return t(remote.value?.state==='PLAYING'?'mobile.castPlaying':remote.value?.state==='PAUSED_PLAYBACK'?'mobile.castPaused':remote.value?.state==='STOPPED'||remote.value?.state==='NO_MEDIA_PRESENT'?'mobile.castNotPlaying':'mobile.castChecking')
})
function stopStatus():void {statusRevision++;clearTimeout(statusTimer);statusTimer=undefined;statusScope.cancel()}
async function refreshStatus():Promise<void>{
 stopStatus()
 if(disposed||!props.open||!selected.value)return
 const token=statusRevision,id=selected.value.id
 try{
  const value=await statusScope.run(()=>GetCastStatus(id))
  if(token!==statusRevision||disposed||selected.value?.id!==id)return
  remote.value=value;statusError.value=''
  paused.value=value.state==='PAUSED_PLAYBACK'
  if(value.state==='PLAYING'&&value.transport_status!=='ERROR_OCCURRED'&&(!value.has_position||value.position>0)&&!announced){announced=true;emit('connected')}
 }catch(error){if(token===statusRevision&&!disposed&&normalizeApiError(error).code!=='CANCELLED'){remote.value=null;statusError.value=t('mobile.castUnconfirmed')}}
 finally{if(token===statusRevision&&!disposed&&props.open&&selected.value)statusTimer=setTimeout(()=>{void refreshStatus()},3000)}
}
let disposed = false
let discovery: ReturnType<typeof DiscoverCastDevices> | undefined
let scanGeneration=0
function stopDiscovery():void {
  scanGeneration++
  const pending=discovery;discovery=undefined
  try{void Promise.resolve(pending?.cancel?.()).catch(()=>{})}catch{/* Already settled. */}
  scanning.value=false;platform.cast.multicast(false)
}
async function scan(): Promise<void> {
  if (scanning.value || disposed || !props.open) return
  const token=++scanGeneration
  scanning.value = true; error.value = ''
  try {
    platform.cast.multicast(true)
    discovery = DiscoverCastDevices()
    const result = await discovery
    if (!disposed&&token===scanGeneration) devices.value = Array.isArray(result)?result:[]
  } catch (e) { if (!disposed&&token===scanGeneration) error.value = normalizeApiError(e).message }
  finally { if(token===scanGeneration){platform.cast.multicast(false); scanning.value = false; discovery = undefined} }
}
async function connect(device: { id: string; name: string }): Promise<void> {
  if (busy.value) return
  const token=sessionRevision
  stopDiscovery()
  busy.value = true; error.value = '';remote.value=null;statusError.value='';announced=false
  try { await controlScope.run(()=>PlayCastVideo(device.id, props.url, props.title)); if (!disposed&&token===sessionRevision) { selected.value = device; paused.value = false;void refreshStatus() } }
  catch (e) { if (!disposed&&token===sessionRevision&&normalizeApiError(e).code!=='CANCELLED') error.value = normalizeApiError(e).message }
  finally { if(token===sessionRevision)busy.value = false }
}
function systemCast():void {
  stopDiscovery()
  try { if(platform.cast.openSystem()){emit('close');return} }
  catch { /* Keep the panel usable when an OEM settings activity is unavailable. */ }
  error.value=t('mobile.castSystemUnavailable')
}
async function control(action: string): Promise<void> {
  if (!selected.value || busy.value) return
  const token=sessionRevision,id=selected.value.id
  stopStatus()
  busy.value = true; error.value = ''
  try {
    await controlScope.run(()=>ControlCastVideo(id, action, action === 'Seek' ? props.position : 0))
    if(disposed||token!==sessionRevision)return
    if (action === 'Stop') {stopStatus();selected.value = null;remote.value=null;emit('stopped')}
    else {if (action !== 'Seek')paused.value = action === 'Pause';void refreshStatus()}
  } catch (e) { if (!disposed&&token===sessionRevision&&normalizeApiError(e).code!=='CANCELLED') {error.value = normalizeApiError(e).message;void refreshStatus()} }
  finally { if(token===sessionRevision)busy.value = false }
}
function cancelControls():void{sessionRevision++;controlScope.cancel();busy.value=false}
watch(() => props.open, value => { if (!value) {cancelControls();stopDiscovery();stopStatus()}else if (!selected.value) void scan();else void refreshStatus() },{immediate:true})
watch(() => props.url, () => {cancelControls();stopStatus(); selected.value = null; paused.value = false;remote.value=null })
onBeforeUnmount(() => { disposed = true;cancelControls();stopStatus(); stopDiscovery() })
</script>
<template>
 <CczjMobilePlayerDrawer class="cczj-mobile-playback-drawer" :solid="true" :initial-height="46" :host="host" :open="open" :fullscreen="!!fullscreen" :title="t('mobile.cast')" @close="emit('close')">
  <div v-if="open" class="cczj-mobile-cast">
    <p v-if="error" class="cczj-mobile-cast-error" role="alert">{{ error }}</p>
    <template v-if="selected">
      <div class="cczj-mobile-cast-session"><Icon name="cast" :size="28"/><strong>{{ selected.name }}</strong><p role="status">{{ statusError || remoteLabel }}</p><p v-if="remote?.has_position">{{formatPlaybackTime(remote.position)}} / {{formatPlaybackTime(remote.duration)}}</p></div>
      <div class="cczj-mobile-cast-actions"><Button variant="primary" :disabled="busy" @click="control(paused ? 'Play' : 'Pause')">{{ t(paused ? 'player.play' : 'player.pause') }}</Button><Button :disabled="busy || position <= 0" @click="control('Seek')">{{ t('mobile.castSeek') }}</Button><Button variant="text" :disabled="busy" @click="control('Stop')">{{ t('mobile.castStop') }}</Button></div>
    </template>
    <template v-else>
      <p>{{t('mobile.castHint')}}</p>
      <div class="cczj-mobile-cast-heading"><strong>{{t('mobile.castDevices')}}</strong><Button variant="text" size="sm" :disabled="scanning || busy" @click="scan">{{ scanning ? t('mobile.castScanning') : t('common.refresh') }}</Button></div>
      <div class="cczj-mobile-cast-list"><Button v-for="device in devices" :key="device.id" variant="primary" :disabled="busy" @click="connect(device)"><Icon name="monitor" :size="22" /><span>{{ device.name }}</span><Icon name="chevron-right" :size="18" /></Button></div>
      <div v-if="!scanning && !devices.length" class="cczj-mobile-cast-empty"><Icon name="cast" :size="30"/><p>{{ t('mobile.castEmpty') }}</p></div>
      <p v-if="busy" role="status">{{ t('mobile.castConnecting') }}</p>
      <Button v-if="platform.cast.systemSupported()" variant="text" size="sm" :disabled="busy" @click="systemCast">{{t('mobile.castSystem')}}</Button>
      <details class="cczj-mobile-cast-help"><summary>{{t('mobile.castHelp')}}</summary><p>{{t('mobile.castHelpText')}}</p></details>
    </template>
  </div>
 </CczjMobilePlayerDrawer>
</template>
