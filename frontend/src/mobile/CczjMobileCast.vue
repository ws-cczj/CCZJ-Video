<script setup lang="ts">
import './player/presentation.css'
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { Button } from '../components/ui'
import CczjMobilePlayerDrawer from './CczjMobilePlayerDrawer.vue'
import CczjMobileCastRemote from './CczjMobileCastRemote.vue'
import { DiscoverCastDevices, DiscoverCastDevicesOnInterfaces, PlayCastVideo, ControlCastVideo, GetCastStatus, normalizeApiError } from '../api/app'
import { createPlayerPlatform } from '../player/platform'
import { createRequestScope } from '../api/requestScope'
import { formatPlaybackTime } from '../player/videoInfoFormat'
import type { CastPlaybackState } from '../player/views/controlState'
const platform=createPlayerPlatform()
const props = defineProps<{ open: boolean; url: string; title: string; position: number; host?:HTMLElement; fullscreen?:boolean }>()
const emit = defineEmits<{ open: []; close: []; connected: []; stopped: [position?: number]; status: [value: CastPlaybackState] }>()
const { t } = useI18n()
const devices = ref<{ id: string; name: string }[]>([])
const scanning = ref(false), busy = ref(false), error = ref('')
const announced=ref(false)
const stopFailed=ref(false)
let initialPosition: number | undefined
let lastPosition: number | undefined
const selected = ref<{ id: string; name: string } | null>(null)
const paused = ref(false)
const remote=ref<{state:string;transport_status:string;position:number;duration:number;has_position:boolean;media_state?:string}|null>(null)
const confirmationExpired=ref(false)
let attemptStarted=0
const statusError=ref(''),statusScope=createRequestScope(),controlScope=createRequestScope()
let statusTimer:ReturnType<typeof setTimeout>|undefined,statusRevision=0,sessionRevision=0
const remoteLabel=computed(()=>{
 if(remote.value?.transport_status==='ERROR_OCCURRED')return t('mobile.castReceiverError')
 if(remote.value?.media_state==='other')return t('mobile.castDifferentMedia')
 if(!announced.value&&(remote.value?.state==='PLAYING'||remote.value?.state==='PAUSED_PLAYBACK'))return t(confirmationExpired.value?'mobile.castVerifyPlayback':'mobile.castChecking')
 return t(remote.value?.state==='PLAYING'?'mobile.castPlaying':remote.value?.state==='PAUSED_PLAYBACK'?'mobile.castPaused':remote.value?.state==='STOPPED'||remote.value?.state==='NO_MEDIA_PRESENT'?'mobile.castNotPlaying':'mobile.castChecking')
})
const receiverActive=computed(()=>remote.value?.transport_status!=='ERROR_OCCURRED'&&(remote.value?.state==='PLAYING'||remote.value?.state==='PAUSED_PLAYBACK'))
const remoteView=computed(()=>({device:selected.value?.name||'',status:error.value||statusError.value||(busy.value?t('mobile.castConnecting'):remoteLabel.value),paused:paused.value,busy:busy.value,active:receiverActive.value,position:remote.value?.position||0,duration:remote.value?.duration||0,hasPosition:!!remote.value?.has_position}))
function confirmPlayback():void {
 if(!receiverActive.value||remote.value?.media_state==='other'||announced.value)return
 announced.value=true;emit('connected')
}
function stopStatus():void {statusRevision++;clearTimeout(statusTimer);statusTimer=undefined;statusScope.cancel()}
async function refreshStatus():Promise<void>{
 stopStatus()
 if(disposed||document.hidden||!selected.value)return
 const token=statusRevision,id=selected.value.id
 try{
  const value=await statusScope.run(()=>GetCastStatus(id))
  if(token!==statusRevision||disposed||selected.value?.id!==id)return
  if(value.media_state!=='matched')value.has_position=false
  remote.value=value;statusError.value=''
  confirmationExpired.value=performance.now()-attemptStarted>=30000
  if(value.has_position)lastPosition=value.position
  paused.value=value.state==='PAUSED_PLAYBACK'||value.state==='STOPPED'||value.state==='NO_MEDIA_PRESENT'
  // A renderer can still report the previous video's PLAYING state after SetURI.
  if(value.media_state==='other'&&announced.value){announced.value=false;emit('stopped',lastPosition)}
  if(value.media_state==='matched')confirmPlayback()
  if(announced.value){
    emit('status',{device:selected.value!.name,state:value.state,position:value.position,duration:value.duration,hasPosition:value.has_position})
    if(value.state==='STOPPED'||value.state==='NO_MEDIA_PRESENT'){announced.value=false;emit('stopped',value.has_position?value.position:undefined)}
    else if(initialPosition!==undefined&&value.state==='PLAYING'){
      const target=initialPosition;initialPosition=undefined
      if(target>0)void control('Seek',target)
    }
  }
 }catch(error){if(token===statusRevision&&!disposed&&normalizeApiError(error).code!=='CANCELLED'){remote.value=null;statusError.value=t('mobile.castUnconfirmed')}}
 finally{if(token===statusRevision&&!disposed&&!document.hidden&&selected.value)statusTimer=setTimeout(()=>{void refreshStatus()},3000)}
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
    let network:ReturnType<typeof platform.cast.interfaces>
    try{network=platform.cast.interfaces()}catch{error.value=t('mobile.castNetworkUnavailable');return}
    if(network && !network.interfaces.length){error.value=t('mobile.castNetworkRequired');return}
    discovery = network ? DiscoverCastDevicesOnInterfaces(network.interfaces) : DiscoverCastDevices()
    const result = await discovery
    if (!disposed&&token===scanGeneration) devices.value = Array.isArray(result)?result:[]
  } catch (e) { if (!disposed&&token===scanGeneration) error.value = normalizeApiError(e).message }
  finally { if(token===scanGeneration){platform.cast.multicast(false); scanning.value = false; discovery = undefined} }
}
async function connect(device: { id: string; name: string }): Promise<void> {
  if (busy.value) return
  const token=sessionRevision
  stopDiscovery()
  stopStatus()
  busy.value = true; error.value = '';remote.value=null;statusError.value='';stopFailed.value=false
  confirmationExpired.value=false;attemptStarted=performance.now()
  const resumePosition=announced.value ? lastPosition ?? props.position : props.position
  initialPosition=Number.isFinite(resumePosition)?Math.max(0,resumePosition):undefined
  lastPosition=undefined
  try { await controlScope.run(()=>PlayCastVideo(device.id, props.url, props.title)); if (!disposed&&token===sessionRevision) { selected.value = device; paused.value = false;void refreshStatus() } }
  catch (e) { if (!disposed&&token===sessionRevision&&normalizeApiError(e).code!=='CANCELLED') error.value = normalizeApiError(e).message }
  finally { if(token===sessionRevision)busy.value = false }
}
async function chooseDevice():Promise<void> {
 if(busy.value)return
 if(announced.value||remote.value?.media_state==='matched'){
   await control('Stop')
   if(selected.value)return
 }
 cancelControls();stopStatus();selected.value=null;remote.value=null;statusError.value='';error.value='';announced.value=false
 void scan()
}
function systemCast():void {
  stopDiscovery()
  cancelControls();stopStatus();selected.value=null;remote.value=null;announced.value=false
  try { if(platform.cast.openSystem()){emit('close');return} }
  catch { /* Keep the panel usable when an OEM settings activity is unavailable. */ }
  error.value=t('mobile.castSystemUnavailable')
}
async function control(action: string, target = 0): Promise<void> {
  if (!selected.value || busy.value) return
  const token=sessionRevision,id=selected.value.id
  stopStatus()
  busy.value = true; error.value = ''
  try {
    // Capture the receiver clock before Stop; many receivers reset it to zero.
    let position=lastPosition
    if(action==='Stop'){
      try{const latest=await controlScope.run(()=>GetCastStatus(id));if(latest.media_state==='matched'&&latest.has_position)position=latest.position}catch{/* Last confirmed receiver position remains usable. */}
      if(disposed||token!==sessionRevision)return
    }
    const seconds=action==='Seek'?Math.max(0,Math.min(target,remote.value?.duration||7*24*3600)):0
    await controlScope.run(()=>ControlCastVideo(id, action, seconds))
    if(disposed||token!==sessionRevision)return
    if (action === 'Stop') finishSession(position)
    else {if (action !== 'Seek')paused.value = action === 'Pause';void refreshStatus()}
  } catch (e) { if (!disposed&&token===sessionRevision&&normalizeApiError(e).code!=='CANCELLED') {error.value = normalizeApiError(e).message;if(action==='Stop')stopFailed.value=true;void refreshStatus()} }
  finally { if(token===sessionRevision)busy.value = false }
}
function finishSession(position?:number):void {
 stopStatus();selected.value=null;remote.value=null;announced.value=false;initialPosition=undefined;lastPosition=undefined;stopFailed.value=false;statusError.value='';error.value='';emit('stopped',position)
}
function disconnect():void {cancelControls();finishSession(lastPosition)}
function cancelControls():void{sessionRevision++;controlScope.cancel();busy.value=false}
// The drawer owns discovery, not the receiver session. Closing it must not
// cancel an accepted handoff or prevent its confirmation.
watch(() => props.open, value => { if (!value) stopDiscovery();else if (!selected.value&&!busy.value) void scan();else if(!busy.value) void refreshStatus() },{immediate:true})
watch(() => props.url, () => {cancelControls();finishSession();paused.value=false})
function visibility():void{if(document.hidden){stopStatus();stopDiscovery()}else if(selected.value&&!busy.value)void refreshStatus()}
onMounted(()=>document.addEventListener('visibilitychange',visibility))
onBeforeUnmount(() => { disposed = true;cancelControls();stopStatus(); stopDiscovery();document.removeEventListener('visibilitychange',visibility) })
</script>
<template>
 <Teleport v-if="announced && host && !open" :to="host">
  <CczjMobileCastRemote compact v-bind="remoteView" @play="control(paused ? 'Play' : 'Pause')" @seek="control('Seek',$event)" @stop="control('Stop')" @change="emit('open');chooseDevice()" @expand="emit('open')" />
 </Teleport>
 <CczjMobilePlayerDrawer class="cczj-mobile-playback-drawer" :solid="true" :expanded="announced" :close-label="announced ? t('mobile.castCollapse') : undefined" :initial-height="60" :host="host" :open="open" :fullscreen="!!fullscreen" :title="announced ? title : t('mobile.cast')" @close="emit('close')">
  <div v-if="open" class="cczj-mobile-cast">
    <template v-if="selected && announced">
      <CczjMobileCastRemote v-bind="remoteView" @play="control(paused ? 'Play' : 'Pause')" @seek="control('Seek',$event)" @stop="control('Stop')" @change="chooseDevice" />
      <div class="cczj-mobile-cast-actions"><Button variant="text" :disabled="busy" @click="connect(selected)">{{t('mobile.castRetry')}}</Button><Button v-if="stopFailed" variant="text" :disabled="busy" @click="disconnect">{{t('mobile.castDisconnect')}}</Button><p v-if="stopFailed">{{t('mobile.castDisconnectHint')}}</p></div>
    </template>
    <template v-else-if="selected">
      <p v-if="error" class="cczj-mobile-cast-error" role="alert">{{ error }}</p>
      <div class="cczj-mobile-cast-session"><Icon name="cast" :size="28"/><strong>{{ selected.name }}</strong><p role="status">{{ statusError || remoteLabel }}</p><p v-if="remote?.has_position">{{formatPlaybackTime(remote.position)}} / {{formatPlaybackTime(remote.duration)}}</p></div>
      <p v-if="confirmationExpired">{{t('mobile.castRetryHint')}}</p>
      <div class="cczj-mobile-cast-actions"><Button :disabled="busy" variant="text" @click="control('Stop')">{{ t('mobile.castStop') }}</Button><Button v-if="stopFailed" variant="text" :disabled="busy" @click="disconnect">{{t('mobile.castDisconnect')}}</Button><p v-if="stopFailed">{{t('mobile.castDisconnectHint')}}</p></div>
      <div class="cczj-mobile-cast-actions"><Button v-if="receiverActive && remote?.media_state!=='matched' && remote?.media_state!=='other'" :disabled="busy" @click="confirmPlayback">{{t('mobile.castConfirmPlayback')}}</Button><Button :disabled="busy" @click="connect(selected)">{{t('mobile.castRetry')}}</Button><Button variant="text" :disabled="busy" @click="chooseDevice">{{t('mobile.castChangeDevice')}}</Button><Button v-if="platform.cast.systemSupported()" variant="text" :disabled="busy" @click="systemCast">{{t('mobile.castSystem')}}</Button></div>
    </template>
    <template v-else>
      <p v-if="error" class="cczj-mobile-cast-error" role="alert">{{ error }}</p>
      <p>{{t('mobile.castHint')}}</p>
      <div class="cczj-mobile-cast-heading"><strong>{{t('mobile.castDevices')}}</strong><Button variant="text" size="sm" :disabled="scanning || busy" @click="scan">{{ scanning ? t('mobile.castScanning') : t('common.refresh') }}</Button></div>
      <div class="cczj-mobile-cast-list"><Button v-for="device in devices" :key="device.id" variant="primary" :disabled="busy" @click="connect(device)"><Icon name="monitor" :size="22" /><span>{{ device.name }}</span><Icon name="chevron-right" :size="18" /></Button></div>
      <div v-if="!scanning && !devices.length && !error" class="cczj-mobile-cast-empty"><Icon name="cast" :size="30"/><p>{{ t('mobile.castEmpty') }}</p></div>
      <p v-if="busy" role="status">{{ t('mobile.castConnecting') }}</p>
      <Button v-if="platform.cast.systemSupported()" variant="text" size="sm" :disabled="busy" @click="systemCast">{{t('mobile.castSystem')}}</Button>
      <p v-if="platform.cast.systemSupported()">{{t('mobile.castSystemHint')}}</p>
      <details class="cczj-mobile-cast-help"><summary>{{t('mobile.castHelp')}}</summary><p>{{t('mobile.castHelpText')}}</p></details>
    </template>
  </div>
 </CczjMobilePlayerDrawer>
</template>
